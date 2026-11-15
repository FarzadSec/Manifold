#!/usr/bin/env bash
# End-to-end check of L2TP inbounds: a real panel, the xl2tpd and pppd it
# runs, the system's strongSwan, and a real L2TP/IPsec client in a network
# namespace of its own. The client sits behind a NAT router, like nearly every
# device that will ever connect.
#
#   sudo deploy/test/l2tp-e2e.sh /path/to/manifold [/path/to/bin-folder]
#
# Needs root, a kernel with PPP and IPsec, xl2tpd, pppd, strongSwan running as
# a service with swanctl, nft, iproute2, curl and python3. It uses UDP ports
# 500, 4500 and 1701 besides what vpn-e2e-lib.sh uses.
#
# Against the installed service (see PANEL_URL in vpn-e2e-lib.sh) the same
# steps check the unit's sandbox. EXPECT_KERNEL_L2TP=1 then also requires the
# sessions to be the kernel's, which the sandbox has to allow.
set -euo pipefail

BIN="$(readlink -f "${1:?usage: l2tp-e2e.sh /path/to/manifold [/path/to/bin-folder]}")"
XRAY_DIR="${2:-}"
ENGINE_LOG_WORD=l2tp
# shellcheck source=deploy/test/vpn-e2e-lib.sh
source "$(dirname "$0")/vpn-e2e-lib.sh"

PSK="e2e-Shared-key-1"
CLIENT_VICI="$WORK/client.vici"
CLIENT_IKE_PID=""
ROUTER_NS=""
DEVICE_NS=mfe2edevice

engine_cleanup() {
    pkill -f "xl2tpd -D -c $WORK/" 2>/dev/null || true
    if [ -n "$CLIENT_IKE_PID" ]; then
        kill "$CLIENT_IKE_PID" 2>/dev/null || true
    fi
    if [ -n "$ROUTER_NS" ]; then
        ip netns del "$ROUTER_NS" 2>/dev/null || true
    fi
}

# behind_nat: turn the client namespace into a home router and make the client
# a device behind it. IPsec through NAT is wrapped in UDP, a path of its own.
behind_nat() {
    ROUTER_NS="$CLIENT_NS"
    ip netns add "$DEVICE_NS"
    ip link add mfe2ed0 type veth peer name mfe2ed1
    ip link set mfe2ed0 netns "$ROUTER_NS"
    ip link set mfe2ed1 netns "$DEVICE_NS"
    ip netns exec "$ROUTER_NS" ip addr add 10.77.0.1/24 dev mfe2ed0
    ip netns exec "$ROUTER_NS" ip link set mfe2ed0 up
    ip netns exec "$ROUTER_NS" sysctl -qw net.ipv4.ip_forward=1
    ip netns exec "$ROUTER_NS" nft -f - <<'NFT'
table ip mfe2enat {
    chain postrouting {
        type nat hook postrouting priority 100;
        oifname "mfe2ec1" masquerade
    }
}
NFT
    ip netns exec "$DEVICE_NS" ip addr add 10.77.0.2/24 dev mfe2ed1
    ip netns exec "$DEVICE_NS" ip link set mfe2ed1 up
    ip netns exec "$DEVICE_NS" ip link set lo up
    ip netns exec "$DEVICE_NS" ip route add default via 10.77.0.1
    # From here on the harness's client is the device.
    CLIENT_NS="$DEVICE_NS"
}

in_client() { ip netns exec "$CLIENT_NS" "$@"; }

# prints PATTERN COMMAND...: the command's output has a line matching PATTERN.
# The output is read to its end first, so the command never writes to a closed pipe.
prints() {
    local pattern="$1" output
    shift
    output="$("$@" 2>/dev/null)" || return 1
    grep -q -- "$pattern" <<<"$output"
}

# Where AppArmor confines swanctl it reaches only strongSwan's own socket and
# files; the client's daemon has both elsewhere.
client_swanctl() {
    if command -v aa-exec >/dev/null 2>&1 && aa-enabled --quiet 2>/dev/null; then
        in_client aa-exec -p unconfined -- swanctl "$@" --uri "unix://$CLIENT_VICI"
    else
        in_client swanctl "$@" --uri "unix://$CLIENT_VICI"
    fi
}

# account_field NAME FIELD: one part of the account the panel hands out for NAME.
account_field() {
    python3 - "$(client_link "$1" l2tp://)" "$2" <<'PY'
import sys, urllib.parse
u = urllib.parse.urlsplit(sys.argv[1])
q = urllib.parse.parse_qs(u.query)
print({
    "host": u.hostname,
    "user": urllib.parse.unquote(u.username),
    "password": urllib.parse.unquote(u.password),
    "psk": q["psk"][0],
    "ipsec": q["ipsec"][0],
}[sys.argv[2]])
PY
}

# start_client_ipsec: a second IKE daemon, in the client's namespace, that
# protects UDP port 1701 to the server with the inbound's key.
start_client_ipsec() {
    local section
    for section in charon charon-systemd; do
        cat <<CONF
$section {
    load_modular = yes
    plugins {
        include /etc/strongswan.d/charon/*.conf
        vici {
            socket = unix://$CLIENT_VICI
        }
    }
}
CONF
    done >"$WORK/client-strongswan.conf"
    cat >"$WORK/client-swanctl.conf" <<CONF
connections {
    mfe2e {
        version = 1
        remote_addrs = $HOST_ADDR
        proposals = aes256-sha256-modp2048
        local {
            auth = psk
        }
        remote {
            auth = psk
        }
        children {
            mfe2e {
                mode = transport
                local_ts = dynamic[udp]
                remote_ts = dynamic[udp/1701]
                esp_proposals = aes256-sha256
            }
        }
    }
}
secrets {
    ike-mfe2e {
        secret = "$(account_field alice psk)"
    }
}
CONF
    STRONGSWAN_CONF="$WORK/client-strongswan.conf" ip netns exec "$CLIENT_NS" charon-systemd >"$WORK/client-ike.log" 2>&1 &
    CLIENT_IKE_PID=$!
    wait_until 20 "the client's IKE daemon to start" test -S "$CLIENT_VICI"
    client_swanctl --load-all --noprompt --file "$WORK/client-swanctl.conf" >"$WORK/client-load.log" 2>&1 \
        || fail "the client's IPsec configuration was refused: $(tail -n 3 "$WORK/client-load.log")"
    client_swanctl --initiate --child mfe2e --timeout 30 >"$WORK/client-initiate.log" 2>&1 \
        || fail "no IPsec connection to the server: $(tail -n 6 "$WORK/client-initiate.log")"
}

# l2tp_connect NAME USER LOCAL_PORT: dial the server as the L2TP client NAME.
# Each client needs a source port of its own; the link comes up as mfe2e<NAME>.
# Without "noipdefault" this pppd proposes the device's own address, as Linux
# clients and routers do; the server has to answer with the one it assigned.
l2tp_connect() {
    local name="$1" user="$2" port="$3"
    cat >"$WORK/$name-ppp" <<OPT
noauth
refuse-pap
refuse-chap
refuse-mschap
refuse-eap
noccp
nodefaultroute
ipcp-accept-local
ipcp-accept-remote
mtu 1280
mru 1280
ifname mfe2e$name
name $user
password $(account_field "$user" password)
debug
logfile $WORK/$name-ppp.log
OPT
    cat >"$WORK/$name-xl2tpd.conf" <<CONF
[global]
port = $port
access control = no

[lac server]
lns = $HOST_ADDR
pppoptfile = $WORK/$name-ppp
length bit = yes
CONF
    : >"$WORK/$name-secrets"
    : >"$WORK/$name-ppp.log"
    in_client xl2tpd -D -c "$WORK/$name-xl2tpd.conf" -s "$WORK/$name-secrets" \
        -p "$WORK/$name-xl2tpd.pid" -C "$WORK/$name-xl2tpd.control" >"$WORK/$name-xl2tpd.log" 2>&1 &
    wait_until 10 "the L2TP client $name to start" test -p "$WORK/$name-xl2tpd.control"
    echo "c server" >"$WORK/$name-xl2tpd.control"
}

l2tp_disconnect() { pkill -f "xl2tpd -D -c $WORK/$1-xl2tpd.conf" 2>/dev/null || true; }

link_up() { prints "inet 10\.195\.0\." in_client ip -4 -o addr show dev "mfe2e$1"; }

link_down() { ! in_client ip link show dev "mfe2e$1" >/dev/null 2>&1; }

# through_tunnel NAME: send the "internet" through NAME's link, like a device's VPN setting does.
through_tunnel() { in_client ip route replace 198.18.0.0/24 dev "mfe2e$1"; }

client_diagnosis() {
    echo "--- $1: pppd"
    tail -n 25 "$WORK/$1-ppp.log" 2>/dev/null || true
    echo "--- $1: xl2tpd"
    tail -n 15 "$WORK/$1-xl2tpd.log" 2>/dev/null || true
    echo "--- server: pppd"
    tail -n 25 "$WORK"/bin/l2tp/*/ppp.log 2>/dev/null || true
}

# wait_for_link SECONDS NAME: like wait_until, with both ends' logs when the link does not come up.
wait_for_link() {
    local i
    for ((i = 0; i < $1; i++)); do
        if link_up "$2"; then
            return 0
        fi
        sleep 1
    done
    client_diagnosis "$2" >&2
    fail "timed out after ${1}s waiting for $2's L2TP link"
}

log "kernel and programs"
[ -c /dev/ppp ] || modprobe ppp_generic 2>/dev/null || true
[ -c /dev/ppp ] || fail "this kernel has no PPP"
for program in xl2tpd pppd swanctl charon-systemd; do
    command -v "$program" >/dev/null 2>&1 || fail "$program is not installed"
done
systemctl is-active --quiet strongswan || systemctl start strongswan || fail "the strongswan service does not start"
# The distribution's own xl2tpd service would hold the port the panel's server needs.
systemctl stop xl2tpd >/dev/null 2>&1 || true

log "network"
network
behind_nat
log "panel"
start_panel
engine_seen="$(field "$(api GET /panel/api/inbounds/engines)" "[e['installed'] for e in r['obj'] if e['protocol'] == 'l2tp']")"
[ "$engine_seen" = "[True]" ] || fail "the panel does not see the L2TP programs: $engine_seen"

log "inbound and accounts"
inbound_id="$(add_inbound l2tp 1701 "{\"server\": {\"psk\": \"$PSK\", \"subnet\": \"10.195.0.0/24\"}, \"clients\": []}")"
add_client alice 0 "$inbound_id" 1
# 60 kB of quota: the pings below use it up.
add_client bob 60000 "$inbound_id" 1
server_listening() { prints ":1701 " ss -lun; }
server_gone() { ! server_listening; }
wait_until 30 "xl2tpd to listen" server_listening
[ "$(account_field alice host) $(account_field alice psk) $(account_field alice ipsec)" = "$HOST_ADDR $PSK 1" ] \
    || fail "alice's account does not name this server and its key: $(client_link alice l2tp://)"
prints "^manifold-l2tp:" swanctl --list-conns || fail "strongSwan was not given the inbound's connection"

log "a session that is not encrypted is not let in"
l2tp_connect plain alice 1711
sleep 12
if ! link_down plain; then
    client_diagnosis plain >&2
    fail "the server took an L2TP session that did not come through IPsec"
fi
l2tp_disconnect plain

log "alice connects through IPsec, reaches the internet through the tunnel, and is billed"
start_client_ipsec
l2tp_connect alice alice 1701
wait_for_link 40 alice
prints "encap type espinudp" in_client ip xfrm state || fail "alice's session is up without IPsec through NAT: $(in_client ip xfrm state | head -n 4)"
if [ "${EXPECT_KERNEL_L2TP:-0}" = 1 ]; then
    prints "^Tunnel " ip l2tp show tunnel || fail "alice's session is relayed by xl2tpd instead of being the kernel's"
fi
through_tunnel alice
wait_until 20 "alice to reach the internet through the tunnel" reaches_internet
use_traffic 20 || fail "no answer through the tunnel and NAT"
wait_until 40 "alice's traffic to be stored" traffic_at_least alice 20000

log "a second device on alice's account is refused"
l2tp_connect alice2 alice 1712
refused() { grep -qi "authentication failed" "$WORK/alice2-ppp.log"; }
wait_until 40 "the second device to be refused" refused
link_down alice2 || fail "the second device has a link"
l2tp_disconnect alice2

log "disabling alice cuts her session"
must_succeed "$(api POST /panel/api/clients/bulkDisable '{"emails":["alice"]}')" "disabling alice"
wait_until 40 "alice's link to go down" link_down alice
l2tp_disconnect alice

log "bob runs out of quota and is cut off"
l2tp_connect bob bob 1713
wait_for_link 40 bob
through_tunnel bob
wait_until 20 "bob to reach the internet through the tunnel" reaches_internet
use_traffic 40 || fail "bob: no answer through the tunnel"
wait_until 60 "bob's account to be switched off" account_switched_off bob
wait_until 40 "bob's link to go down" link_down bob
l2tp_disconnect bob

log "deleting the inbound stops its server"
must_succeed "$(api POST "/panel/api/inbounds/del/$inbound_id")" "deleting the inbound"
wait_until 20 "xl2tpd to stop listening" server_gone
for table in "manifold_l2tp${inbound_id}" "manifold_l2tp${inbound_id}g"; do
    if nft list table ip "$table" >/dev/null 2>&1; then
        fail "the table $table outlived the inbound"
    fi
done
if prints "^manifold-l2tp:" swanctl --list-conns; then
    fail "strongSwan still holds the inbound's connection"
fi

echo "PASS"

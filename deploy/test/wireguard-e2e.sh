#!/usr/bin/env bash
# End-to-end check of WireGuard inbounds: a real panel, the kernel interface
# it brings up, and a real peer in a network namespace of its own.
#
#   sudo deploy/test/wireguard-e2e.sh /path/to/manifold [/path/to/bin-folder]
#
# It also takes over an interface that wg-quick already runs, with the peer
# names and usage a WGDashboard database holds for it.
#
# Needs root, wireguard-tools, nft, iproute2, curl and python3. It uses ports
# 51897/udp and 51898/udp and the interface mfe2eold besides what
# vpn-e2e-lib.sh uses.
set -euo pipefail

BIN="$(readlink -f "${1:?usage: wireguard-e2e.sh /path/to/manifold [/path/to/bin-folder]}")"
XRAY_DIR="${2:-}"
ENGINE_LOG_WORD=wireguard
# shellcheck source=deploy/test/vpn-e2e-lib.sh
source "$(dirname "$0")/vpn-e2e-lib.sh"

WG_PORT=51897
OLD_PORT=51898
OLD_IFACE=mfe2eold
OLD_CONF="/etc/wireguard/${OLD_IFACE}.conf"
CONNECTED=""

engine_cleanup() {
    if [ -n "$CONNECTED" ]; then
        ip netns exec "$CLIENT_NS" wg-quick down "$WORK/$CONNECTED.conf" >/dev/null 2>&1 || true
    fi
    if ip link show "$OLD_IFACE" >/dev/null 2>&1; then
        wg-quick down "$OLD_CONF" >/dev/null 2>&1 || ip link del "$OLD_IFACE" 2>/dev/null || true
    fi
    rm -f "$OLD_CONF" "${OLD_CONF}.before-manifold"
}

# connect NAME: bring the account's peer up in the client namespace, from the
# link the panel hands out. Only the internet namespace is routed into the tunnel.
connect() {
    local link
    link="$(client_link "$1" wireguard://)"
    # The file name is the interface name, which holds at most 15 characters.
    python3 - "$link" >"$WORK/mfe2e$1.conf" <<'PY'
import sys, urllib.parse
u = urllib.parse.urlsplit(sys.argv[1])
q = dict(urllib.parse.parse_qsl(u.query))
print("[Interface]")
print("PrivateKey = " + urllib.parse.unquote(u.username))
print("Address = " + q["address"])
print("[Peer]")
print("PublicKey = " + q["publickey"])
print("Endpoint = %s:%d" % (u.hostname, u.port))
print("AllowedIPs = 198.18.0.0/24")
print("PersistentKeepalive = 1")
PY
    grep -q "^Endpoint = ${HOST_ADDR}:${WG_PORT}\$" "$WORK/mfe2e$1.conf" || fail "config of $1 has the wrong endpoint: $(grep '^Endpoint' "$WORK/mfe2e$1.conf")"
    ip netns exec "$CLIENT_NS" wg-quick up "$WORK/mfe2e$1.conf" >"$WORK/$1.log" 2>&1 || fail "peer $1 did not come up: $(tail -n 3 "$WORK/$1.log")"
    CONNECTED="mfe2e$1"
}

disconnect() {
    ip netns exec "$CLIENT_NS" wg-quick down "$WORK/$CONNECTED.conf" >/dev/null 2>&1 || true
    CONNECTED=""
}

cut_off() { ! reaches_internet; }

interface_up() { ip link show "mfwg${inbound_id}"; }

interface_gone() { ! interface_up; }

log "network"
network
log "panel"
start_panel

engine_seen="$(field "$(api GET /panel/api/inbounds/engines)" "[e['installed'] for e in r['obj'] if e['protocol'] == 'wireguard']")"
[ "$engine_seen" = "[True]" ] || fail "the panel does not see the WireGuard tools: $engine_seen"

log "inbound and accounts"
inbound_id="$(add_inbound wireguard "$WG_PORT" "{\"secretKey\": \"$(wg genkey)\", \"subnetIp\": \"10.198.0.0\", \"subnetCidr\": 24, \"clients\": []}")"
add_client alice 0 "$inbound_id"
# 60 kB of quota: the pings below use it up.
add_client bob 60000 "$inbound_id"
wait_until 30 "the WireGuard interface to come up" interface_up
grep -q "^# Managed by Manifold: inbound ${inbound_id}\." "/etc/wireguard/mfwg${inbound_id}.conf" || fail "the interface has no config in /etc/wireguard"

log "alice connects, reaches the internet through the tunnel, and is billed"
connect alice
wait_until 20 "alice to reach the internet through the tunnel" reaches_internet
use_traffic 20 || fail "no answer through the tunnel and NAT"
wait_until 40 "alice's traffic to be stored" traffic_at_least alice 20000

log "disabling alice removes her peer, enabling her puts it back"
must_succeed "$(api POST /panel/api/clients/bulkDisable '{"emails":["alice"]}')" "disabling alice"
wait_until 20 "alice to be cut off" cut_off
must_succeed "$(api POST /panel/api/clients/bulkEnable '{"emails":["alice"]}')" "enabling alice"
wait_until 20 "alice to reach the internet again" reaches_internet
disconnect

log "bob runs out of quota and is cut off"
connect bob
wait_until 20 "bob to reach the internet through the tunnel" reaches_internet
use_traffic 40 || fail "bob: no answer through the tunnel"
wait_until 60 "bob's account to be switched off" account_switched_off bob
wait_until 20 "bob to be cut off" cut_off
disconnect

log "an interface wg-quick already runs is taken over with its peers"
old_server_key="$(wg genkey)"
dave_key="$(wg genkey)"
dave_public="$(printf '%s' "$dave_key" | wg pubkey)"
erin_public="$(wg genkey | wg pubkey)"
# The server sits on the last address of its subnet and has no NAT of its own.
cat >"$OLD_CONF" <<CONF
[Interface]
PrivateKey = ${old_server_key}
Address = 10.197.0.254/24
ListenPort = ${OLD_PORT}
PostUp = touch ${WORK}/hook-ran

#Name# = from the config
[Peer]
PublicKey = ${dave_public}
AllowedIPs = 10.197.0.2/32
CONF
chmod 600 "$OLD_CONF"
wg-quick up "$OLD_CONF" >"$WORK/old-up.log" 2>&1 || fail "the old interface did not come up: $(tail -n 3 "$WORK/old-up.log")"
cat >"$WORK/mfe2edave.conf" <<CONF
[Interface]
PrivateKey = ${dave_key}
Address = 10.197.0.2/32
[Peer]
PublicKey = $(printf '%s' "$old_server_key" | wg pubkey)
Endpoint = ${HOST_ADDR}:${OLD_PORT}
AllowedIPs = 198.18.0.0/24, 10.197.0.254/32
PersistentKeepalive = 1
CONF
ip netns exec "$CLIENT_NS" wg-quick up "$WORK/mfe2edave.conf" >"$WORK/dave.log" 2>&1 || fail "dave did not come up: $(tail -n 3 "$WORK/dave.log")"
CONNECTED=mfe2edave
reaches_old_server() { ip netns exec "$CLIENT_NS" ping -c 2 -i 0.2 -W 2 10.197.0.254; }
wait_until 20 "dave to reach the old server" reaches_old_server
if reaches_internet >/dev/null 2>&1; then
    fail "the old interface already had NAT, so the takeover would prove nothing"
fi

# What WGDashboard 4.3.5 keeps: a table per interface, usage in GiB, restricted peers apart.
python3 - "$WORK/wgd.db" "$OLD_IFACE" "$dave_public" "$dave_key" "$erin_public" <<'PY'
import sqlite3, sys
path, iface, dave_public, dave_key, erin_public = sys.argv[1:]
columns = """(id VARCHAR(255) NOT NULL PRIMARY KEY, private_key VARCHAR(255), DNS TEXT, endpoint_allowed_ip TEXT,
    name TEXT, total_receive FLOAT, total_sent FLOAT, total_data FLOAT, endpoint VARCHAR(255), status VARCHAR(255),
    latest_handshake VARCHAR(255), allowed_ip VARCHAR(255), cumu_receive FLOAT, cumu_sent FLOAT, cumu_data FLOAT,
    mtu INTEGER, keepalive INTEGER, notes TEXT, remote_endpoint VARCHAR(255), preshared_key VARCHAR(255))"""
db = sqlite3.connect(path)
db.execute(f'CREATE TABLE "{iface}" {columns}')
db.execute(f'CREATE TABLE "{iface}_restrict_access" {columns}')
db.execute(f'INSERT INTO "{iface}" (id, private_key, name, DNS, total_receive, total_sent, cumu_receive, cumu_sent, allowed_ip, keepalive) '
           "VALUES (?, ?, 'Dave Phone', '1.1.1.1', 0.5, 1.0, 0.5, 1.0, '10.197.0.2/32', 21)", (dave_public, dave_key))
db.execute(f'INSERT INTO "{iface}_restrict_access" (id, private_key, name, cumu_receive, cumu_sent, allowed_ip) '
           "VALUES (?, '', 'erin', 0, 0, '10.197.0.3/32')", (erin_public,))
db.commit()
PY

sources="$(api GET "/panel/api/inbounds/wireguard/importable?dashboardDb=${WORK}/wgd.db")"
found="$(field "$sources" "[(i['name'], i['port'], i['peers'], i['restricted'], i['up'], i['problem']) for i in r['obj']['interfaces'] if i['name'] == '${OLD_IFACE}']")"
[ "$found" = "[('${OLD_IFACE}', ${OLD_PORT}, 1, 1, True, '')]" ] || fail "the old interface is not offered for import as it is: $found"

rm -f "$WORK/hook-ran"
imported="$(api POST /panel/api/inbounds/wireguard/import "{\"interface\": \"${OLD_IFACE}\", \"dashboardDb\": \"${WORK}/wgd.db\"}")"
must_succeed "$imported" "importing the old interface"
summary="$(field "$imported" "(r['obj']['clients'], r['obj']['disabled'], r['obj']['withoutKey'])")"
[ "$summary" = "(2, 1, 1)" ] || fail "import result: $imported"
old_inbound_id="$(field "$imported" 'r["obj"]["inboundId"]')"

old_is_managed() { grep -q "^# Managed by Manifold: inbound ${old_inbound_id}\." "$OLD_CONF"; }
wait_until 20 "the panel to run the interface from its own config" old_is_managed
[ -f "${OLD_CONF}.before-manifold" ] || fail "the old config was not kept"
[ -f "$WORK/hook-ran" ] || fail "the interface came back without running its PostUp hook"
grep -q "^Address = 10.197.0.254/24\$" "$OLD_CONF" || fail "the server lost its address: $(grep '^Address' "$OLD_CONF")"

# dave's config is untouched: same key, same server address, and now NAT from the panel.
wait_until 20 "dave to reach the server again" reaches_old_server
wait_until 20 "dave to reach the internet through the panel's NAT" reaches_internet
# 1 GiB up and 2 GiB down came with him from WGDashboard.
traffic="$(api GET /panel/api/clients/traffic/Dave-Phone)"
[ "$(field "$traffic" "r['obj']['up'] >= 1 << 30 and r['obj']['down'] >= 2 << 30 and r['obj']['enable']")" = "True" ] || fail "dave's usage was not imported: $traffic"
client_link Dave-Phone wireguard:// | grep -q "@${HOST_ADDR}:${OLD_PORT}?" || fail "the panel cannot hand dave his config: $(client_link Dave-Phone wireguard://)"
[ "$(field "$(api GET /panel/api/clients/traffic/erin)" "r['obj']['enable']")" = "False" ] || fail "the restricted peer was not imported disabled"
if wg show "$OLD_IFACE" peers | grep -q "$erin_public"; then
    fail "the restricted peer is on the interface"
fi
disconnect
must_succeed "$(api POST "/panel/api/inbounds/del/$old_inbound_id")" "deleting the imported inbound"
old_gone() { ! ip link show "$OLD_IFACE"; }
wait_until 20 "the imported interface to go down" old_gone

log "deleting the inbound brings its interface down"
must_succeed "$(api POST "/panel/api/inbounds/del/$inbound_id")" "deleting the inbound"
wait_until 20 "the interface to go down" interface_gone
if nft list table ip "manifold_wg${inbound_id}" >/dev/null 2>&1; then
    fail "the NAT table outlived the inbound"
fi
if [ -e "/etc/wireguard/mfwg${inbound_id}.conf" ]; then
    fail "the config outlived the inbound"
fi

echo "PASS"

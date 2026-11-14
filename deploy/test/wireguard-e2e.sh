#!/usr/bin/env bash
# End-to-end check of WireGuard inbounds: a real panel, the kernel interface
# it brings up, and a real peer in a network namespace of its own.
#
#   sudo deploy/test/wireguard-e2e.sh /path/to/manifold [/path/to/bin-folder]
#
# Needs root, wireguard-tools, nft, iproute2, curl and python3. It uses port
# 51897/udp besides what vpn-e2e-lib.sh uses.
set -euo pipefail

BIN="$(readlink -f "${1:?usage: wireguard-e2e.sh /path/to/manifold [/path/to/bin-folder]}")"
XRAY_DIR="${2:-}"
ENGINE_LOG_WORD=wireguard
# shellcheck source=deploy/test/vpn-e2e-lib.sh
source "$(dirname "$0")/vpn-e2e-lib.sh"

WG_PORT=51897
CONNECTED=""

engine_cleanup() {
    if [ -n "$CONNECTED" ]; then
        ip netns exec "$CLIENT_NS" wg-quick down "$WORK/$CONNECTED.conf" >/dev/null 2>&1 || true
    fi
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

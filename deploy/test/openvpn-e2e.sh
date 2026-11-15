#!/usr/bin/env bash
# End-to-end check of OpenVPN inbounds: a real panel, the openvpn server it
# starts, and real clients in a network namespace of their own.
#
#   sudo deploy/test/openvpn-e2e.sh /path/to/manifold [/path/to/bin-folder]
#
# The optional bin folder holds the Xray binary and data files the panel runs;
# without it the panel runs without Xray. Needs root, openvpn, nft, iproute2,
# curl and python3. It uses port 21194/udp besides what vpn-e2e-lib.sh uses.
set -euo pipefail

BIN="$(readlink -f "${1:?usage: openvpn-e2e.sh /path/to/manifold [/path/to/bin-folder]}")"
XRAY_DIR="${2:-}"
ENGINE_LOG_WORD=openvpn
# shellcheck source=deploy/test/vpn-e2e-lib.sh
source "$(dirname "$0")/vpn-e2e-lib.sh"

VPN_PORT=21194

engine_cleanup() { pkill -f "openvpn --config $WORK/" 2>/dev/null || true; }

# connect NAME: start an OpenVPN client for the account in the client namespace.
connect() {
    local link
    link="$(client_link "$1" openvpn://)"
    python3 - "$link" >"$WORK/$1.ovpn" <<'PY'
import base64, sys
payload = sys.argv[1][len("openvpn://"):]
sys.stdout.write(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)).decode())
PY
    grep -q "^remote ${HOST_ADDR} ${VPN_PORT}\$" "$WORK/$1.ovpn" || fail "profile of $1 has the wrong remote: $(grep '^remote' "$WORK/$1.ovpn")"
    if client_before_2_6; then
        login_from_file "$WORK/$1.ovpn" "$WORK/$1.auth"
    fi
    ip netns exec "$CLIENT_NS" openvpn --config "$WORK/$1.ovpn" >"$WORK/$1.log" 2>&1 &
}

# OpenVPN before 2.6, as on Ubuntu 22.04, does not read a login embedded in the
# profile. Such a client is given it in a file, where its user would type it in.
client_before_2_6() {
    local version major minor
    version="$(openvpn --version | sed -n '1s/^OpenVPN \([0-9]*\)\.\([0-9]*\).*/\1 \2/p')"
    read -r major minor <<<"$version"
    [ "$major" -lt 2 ] || { [ "$major" -eq 2 ] && [ "$minor" -lt 6 ]; }
}

login_from_file() {
    python3 - "$1" "$2" <<'PY'
import re, sys
profile = open(sys.argv[1]).read()
block = re.search(r"<auth-user-pass>\n(.*?)\n</auth-user-pass>\n", profile, re.S)
open(sys.argv[2], "w").write(block.group(1) + "\n")
open(sys.argv[1], "w").write(profile.replace(block.group(0), "auth-user-pass %s\n" % sys.argv[2]))
PY
}

# wait_connected NAME: like wait_until, with the client's own words when it does not connect.
wait_connected() {
    local i
    for ((i = 0; i < 40; i++)); do
        if grep -q "Initialization Sequence Completed" "$WORK/$1.log"; then
            return 0
        fi
        sleep 1
    done
    echo "--- $1: openvpn client" >&2
    tail -n 25 "$WORK/$1.log" >&2 || true
    fail "timed out after 40s waiting for $1 to connect"
}

client_gone() { ! pgrep -f "openvpn --config $WORK/$1.ovpn" >/dev/null; }

server_listening() { ss -lun | grep -q ":${VPN_PORT} "; }

server_gone() { ! server_listening; }

log "network"
network
log "panel"
start_panel

log "inbound and accounts"
inbound_id="$(add_inbound openvpn "$VPN_PORT" '{"server": {"proto": "udp", "subnet": "10.199.0.0/24", "dns": ["1.1.1.1"]}, "clients": []}')"
add_client alice 0 "$inbound_id" 1
# 60 kB of quota: the pings below use it up.
add_client bob 60000 "$inbound_id" 1
wait_until 30 "the OpenVPN server to listen" server_listening

log "alice connects, reaches the internet through the tunnel, and is billed"
connect alice
wait_connected alice
use_traffic 20 || fail "no answer through the tunnel and NAT"
wait_until 40 "alice's traffic to be stored" traffic_at_least alice 20000

log "a second device on alice's account is refused"
cp "$WORK/alice.ovpn" "$WORK/alice2.ovpn"
# openvpn exits with status 0 after an authentication failure, so only its log tells.
ip netns exec "$CLIENT_NS" timeout 30 openvpn --config "$WORK/alice2.ovpn" >"$WORK/alice2.log" 2>&1 || true
grep -q "AUTH_FAILED.*maximum number of devices" "$WORK/alice2.log" || fail "second device: $(tail -n 3 "$WORK/alice2.log")"

log "disabling alice cuts her session"
must_succeed "$(api POST /panel/api/clients/bulkDisable '{"emails":["alice"]}')" "disabling alice"
wait_until 40 "alice's client to be refused" client_gone alice
grep -q "AUTH_FAILED" "$WORK/alice.log" || fail "alice was not refused: $(tail -n 3 "$WORK/alice.log")"

log "bob runs out of quota and is cut off"
connect bob
wait_connected bob
use_traffic 40 || fail "bob: no answer through the tunnel"
wait_until 60 "bob's account to be switched off" account_switched_off bob
wait_until 40 "bob's client to be refused" client_gone bob
grep -q "AUTH_FAILED" "$WORK/bob.log" || fail "bob was not refused: $(tail -n 3 "$WORK/bob.log")"

log "deleting the inbound stops its server"
must_succeed "$(api POST "/panel/api/inbounds/del/$inbound_id")" "deleting the inbound"
wait_until 20 "the server to stop listening" server_gone
if nft list table ip "manifold_ovpn${inbound_id}" >/dev/null 2>&1; then
    fail "the NAT table outlived the inbound"
fi
if ip link show "mfovpn${inbound_id}" >/dev/null 2>&1; then
    fail "the tunnel device outlived the inbound"
fi

echo "PASS"

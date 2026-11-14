#!/usr/bin/env bash
# End-to-end check of OpenVPN inbounds: a real panel, the openvpn server it
# starts, and real clients in a network namespace of their own.
#
#   sudo deploy/test/openvpn-e2e.sh /path/to/manifold [/path/to/bin-folder]
#
# The optional bin folder holds the Xray binary and data files the panel runs;
# without it the panel runs without Xray. Needs root, openvpn, nft, iproute2,
# curl and python3. It uses ports 20541 (panel) and 21194/udp (OpenVPN), and
# the names prefixed "mfe2e".
set -euo pipefail

BIN="${1:?usage: openvpn-e2e.sh /path/to/manifold [/path/to/bin-folder]}"
BIN="$(readlink -f "$BIN")"
XRAY_DIR="${2:-}"
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }

PANEL_PORT=20541
VPN_PORT=21194
HOST_ADDR=192.168.251.1
CLIENT_NS=mfe2eclient
INET_NS=mfe2einet
INET_ADDR=198.18.0.2
BASE="http://${HOST_ADDR}:${PANEL_PORT}/e2e"

# A short path: the management socket's address is limited to 107 bytes.
WORK="$(mktemp -d /tmp/mfe2e.XXXXXX)"
JAR="$WORK/cookies"
PANEL_PID=""
TOKEN=""

log() { printf '== %s\n' "$*"; }

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    if [ -f "$WORK/panel.log" ]; then
        echo "--- last panel log lines about OpenVPN" >&2
        grep -i "openvpn" "$WORK/panel.log" | grep -v "peer info" | tail -n 60 >&2 || true
        echo "--- last panel warnings and errors" >&2
        grep -E "WARNING|ERROR" "$WORK/panel.log" | tail -n 15 >&2 || true
    fi
    exit 1
}

cleanup() {
    pkill -f "openvpn --config $WORK/" 2>/dev/null || true
    if [ -n "$PANEL_PID" ]; then
        kill "$PANEL_PID" 2>/dev/null || true
        wait "$PANEL_PID" 2>/dev/null || true
    fi
    ip netns del "$CLIENT_NS" 2>/dev/null || true
    ip netns del "$INET_NS" 2>/dev/null || true
    ip link del mfe2ec0 2>/dev/null || true
    ip link del mfe2ei0 2>/dev/null || true
    rm -rf "$WORK"
}
trap cleanup EXIT

# api METHOD PATH [JSON]: call the panel API and print the response body.
api() {
    local method="$1" path="$2" body="${3:-}"
    if [ -n "$body" ]; then
        curl -fsS -b "$JAR" -c "$JAR" -X "$method" -H "X-CSRF-Token: $TOKEN" \
            -H 'Content-Type: application/json' -d "$body" "$BASE$path"
    else
        curl -fsS -b "$JAR" -c "$JAR" -X "$method" -H "X-CSRF-Token: $TOKEN" "$BASE$path"
    fi
}

# field JSON EXPR: evaluate a Python expression against the parsed response as r.
field() {
    python3 -c 'import json,sys; r=json.loads(sys.argv[1]); print(eval(sys.argv[2]))' "$1" "$2"
}

must_succeed() {
    [ "$(field "$1" 'r["success"]')" = "True" ] || fail "$2: $1"
}

# wait_until SECONDS DESCRIPTION COMMAND...: poll until the command succeeds.
wait_until() {
    local seconds="$1" what="$2" i
    shift 2
    for ((i = 0; i < seconds; i++)); do
        if "$@" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    fail "timed out after ${seconds}s waiting for: $what"
}

network() {
    ip netns add "$CLIENT_NS"
    ip link add mfe2ec0 type veth peer name mfe2ec1
    ip link set mfe2ec1 netns "$CLIENT_NS"
    ip addr add "${HOST_ADDR}/24" dev mfe2ec0
    ip link set mfe2ec0 up
    ip netns exec "$CLIENT_NS" ip addr add 192.168.251.2/24 dev mfe2ec1
    ip netns exec "$CLIENT_NS" ip link set mfe2ec1 up
    ip netns exec "$CLIENT_NS" ip link set lo up
    ip netns exec "$CLIENT_NS" ip route add default via "$HOST_ADDR"

    # The "internet" has no route back to the tunnel subnet, so an answer from
    # it proves the traffic was both tunnelled and masqueraded.
    ip netns add "$INET_NS"
    ip link add mfe2ei0 type veth peer name mfe2ei1
    ip link set mfe2ei1 netns "$INET_NS"
    ip addr add 198.18.0.1/24 dev mfe2ei0
    ip link set mfe2ei0 up
    ip netns exec "$INET_NS" ip addr add "${INET_ADDR}/24" dev mfe2ei1
    ip netns exec "$INET_NS" ip link set mfe2ei1 up
}

start_panel() {
    export MANIFOLD_DB_FOLDER="$WORK/db" MANIFOLD_LOG_FOLDER="$WORK/log" MANIFOLD_BIN_FOLDER="$WORK/bin"
    export MANIFOLD_LOG_LEVEL=debug
    mkdir -p "$WORK/db" "$WORK/log" "$WORK/bin"
    if [ -n "$XRAY_DIR" ]; then
        cp -r "$XRAY_DIR"/. "$WORK/bin/"
    fi
    "$BIN" setting -port "$PANEL_PORT" -webBasePath /e2e/ -username e2e -password e2e-pass-123 >/dev/null
    "$BIN" >"$WORK/panel.log" 2>&1 &
    PANEL_PID=$!
    wait_until 40 "the panel to answer" curl -fsS -o /dev/null "$BASE/csrf-token"
    TOKEN="$(field "$(curl -fsS -c "$JAR" "$BASE/csrf-token")" 'r["obj"]')"
    must_succeed "$(curl -fsS -b "$JAR" -c "$JAR" -H "X-CSRF-Token: $TOKEN" \
        -d 'username=e2e&password=e2e-pass-123' "$BASE/login")" "login"
    # Logging in starts a new session, and the token belongs to the session.
    TOKEN="$(field "$(curl -fsS -b "$JAR" -c "$JAR" "$BASE/csrf-token")" 'r["obj"]')"
}

# profile EMAIL: print the client's .ovpn as the panel hands it out.
profile() {
    local links
    links="$(api GET "/panel/api/clients/links/$1")"
    python3 - "$links" <<'PY'
import base64, json, sys
links = json.loads(sys.argv[1])["obj"]
found = [l for l in links if l.startswith("openvpn://")]
if len(found) != 1:
    sys.exit("expected exactly one openvpn:// link, got: %r" % links)
payload = found[0][len("openvpn://"):]
sys.stdout.write(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)).decode())
PY
}

# connect NAME: start an OpenVPN client for the account in the client namespace.
connect() {
    profile "$1" >"$WORK/$1.ovpn"
    grep -q "^remote ${HOST_ADDR} ${VPN_PORT}\$" "$WORK/$1.ovpn" || fail "profile of $1 has the wrong remote: $(grep '^remote' "$WORK/$1.ovpn")"
    ip netns exec "$CLIENT_NS" openvpn --config "$WORK/$1.ovpn" >"$WORK/$1.log" 2>&1 &
}

client_gone() { ! pgrep -f "openvpn --config $WORK/$1.ovpn" >/dev/null; }

traffic_at_least() {
    local traffic
    traffic="$(api GET "/panel/api/clients/traffic/$1")"
    [ "$(field "$traffic" "r['obj']['up'] >= $2 and r['obj']['down'] >= $2")" = "True" ]
}

server_listening() { ss -lun | grep -q ":${VPN_PORT} "; }

server_gone() { ! server_listening; }

account_switched_off() {
    [ "$(field "$(api GET "/panel/api/clients/traffic/$1")" "r['obj']['enable']")" = "False" ]
}

log "network"
network
log "panel"
start_panel

log "inbound and accounts"
inbound="$(api POST /panel/api/inbounds/add "$(python3 - <<PY
import json
print(json.dumps({
    "remark": "e2e", "enable": True, "port": $VPN_PORT, "protocol": "openvpn",
    "settings": json.dumps({"server": {"proto": "udp", "subnet": "10.199.0.0/24", "dns": ["1.1.1.1"]}, "clients": []}),
}))
PY
)")"
must_succeed "$inbound" "adding the inbound"
inbound_id="$(field "$inbound" 'r["obj"]["id"]')"

add_client() {
    must_succeed "$(api POST /panel/api/clients/add "$(python3 - "$1" "$2" "$inbound_id" <<'PY'
import json, sys
email, total, inbound = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
print(json.dumps({"client": {"email": email, "enable": True, "totalGB": total, "limitIp": 1}, "inboundIds": [inbound]}))
PY
)")" "adding client $1"
}
add_client alice 0
# 60 kB of quota: the pings below use it up.
add_client bob 60000

wait_until 30 "the OpenVPN server to listen" server_listening

log "alice connects, reaches the internet through the tunnel, and is billed"
connect alice
wait_until 40 "alice to connect" grep -q "Initialization Sequence Completed" "$WORK/alice.log"
ip netns exec "$CLIENT_NS" ping -c 20 -i 0.05 -s 1000 -W 3 "$INET_ADDR" >/dev/null || fail "no answer through the tunnel and NAT"
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
wait_until 40 "bob to connect" grep -q "Initialization Sequence Completed" "$WORK/bob.log"
ip netns exec "$CLIENT_NS" ping -c 40 -i 0.05 -s 1000 -W 3 "$INET_ADDR" >/dev/null || fail "bob: no answer through the tunnel"
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

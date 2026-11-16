#!/usr/bin/env bash
# Shared by the VPN end-to-end scripts: a real panel on this host, a client
# network namespace and an "internet" namespace, and helpers for the panel API.
# Source it after setting BIN and, optionally, XRAY_DIR.
#
# It uses port 20541 for the panel and the names prefixed "mfe2e".
#
# With PANEL_URL, PANEL_USER and PANEL_PASSWORD set it drives a panel that
# already runs instead of starting one: the installed service, under its unit.
# Pass them with "sudo env NAME=value ..."; not every sudo has -E.

[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }

PANEL_PORT=20541
HOST_ADDR=192.168.251.1
CLIENT_NS=mfe2eclient
INET_NS=mfe2einet
INET_ADDR=198.18.0.2
BASE="http://${HOST_ADDR}:${PANEL_PORT}/e2e"

# A short path: an engine's unix socket address is limited to 107 bytes.
WORK="$(mktemp -d /tmp/mfe2e.XXXXXX)"
JAR="$WORK/cookies"
PANEL_PID=""
TOKEN=""
# The accounts add_client made: a panel the script did not start is left without them.
ACCOUNTS=()
# What the panel log calls the engine under test; set by the script for failure output.
ENGINE_LOG_WORD="${ENGINE_LOG_WORD:-}"

log() { printf '== %s\n' "$*"; }

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    if [ -n "${PANEL_URL:-}" ]; then
        echo "--- last lines of the service's log" >&2
        journalctl -u manifold -n 80 --no-pager >&2 || true
    elif [ -f "$WORK/panel.log" ]; then
        if [ -n "$ENGINE_LOG_WORD" ]; then
            echo "--- last panel log lines about ${ENGINE_LOG_WORD}" >&2
            grep -i "$ENGINE_LOG_WORD" "$WORK/panel.log" | grep -v "peer info" | tail -n 60 >&2 || true
        fi
        echo "--- last panel warnings and errors" >&2
        grep -E "WARNING|ERROR" "$WORK/panel.log" | tail -n 15 >&2 || true
    fi
    exit 1
}

# engine_cleanup is replaced by a script that has more to stop than the panel.
engine_cleanup() { :; }

cleanup() {
    engine_cleanup
    if [ -n "${SPEED_PID:-}" ]; then
        kill "$SPEED_PID" 2>/dev/null || true
    fi
    if [ -n "${PANEL_URL:-}" ] && [ "${#ACCOUNTS[@]}" -gt 0 ]; then
        api POST /panel/api/clients/bulkDel \
            "$(python3 -c 'import json, sys; print(json.dumps({"emails": sys.argv[1:]}))' "${ACCOUNTS[@]}")" >/dev/null 2>&1 || true
    fi
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

    # The "internet" has no route back to a tunnel subnet, so an answer from
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
    local user=e2e password=e2e-pass-123
    if [ -n "${PANEL_URL:-}" ]; then
        BASE="${PANEL_URL%/}" user="$PANEL_USER" password="$PANEL_PASSWORD"
    else
        export MANIFOLD_DB_FOLDER="$WORK/db" MANIFOLD_LOG_FOLDER="$WORK/log" MANIFOLD_BIN_FOLDER="$WORK/bin"
        export MANIFOLD_LOG_LEVEL=debug
        mkdir -p "$WORK/db" "$WORK/log" "$WORK/bin"
        if [ -n "${XRAY_DIR:-}" ]; then
            cp -r "$XRAY_DIR"/. "$WORK/bin/"
        fi
        "$BIN" setting -port "$PANEL_PORT" -webBasePath /e2e/ -username "$user" -password "$password" >/dev/null
        "$BIN" >"$WORK/panel.log" 2>&1 &
        PANEL_PID=$!
    fi
    wait_until 40 "the panel to answer" curl -fsS -o /dev/null "$BASE/csrf-token"
    TOKEN="$(field "$(curl -fsS -c "$JAR" "$BASE/csrf-token")" 'r["obj"]')"
    must_succeed "$(curl -fsS -b "$JAR" -c "$JAR" -H "X-CSRF-Token: $TOKEN" \
        --data-urlencode "username=$user" --data-urlencode "password=$password" "$BASE/login")" "login"
    # Logging in starts a new session, and the token belongs to the session.
    TOKEN="$(field "$(curl -fsS -b "$JAR" -c "$JAR" "$BASE/csrf-token")" 'r["obj"]')"
}

# add_inbound PROTOCOL PORT SETTINGS_JSON: create an inbound and print its id.
add_inbound() {
    local response
    response="$(api POST /panel/api/inbounds/add "$(python3 - "$1" "$2" "$3" <<'PY'
import json, sys
print(json.dumps({"remark": "e2e", "enable": True, "port": int(sys.argv[2]), "protocol": sys.argv[1], "settings": sys.argv[3]}))
PY
)")"
    must_succeed "$response" "adding the $1 inbound"
    field "$response" 'r["obj"]["id"]'
}

# add_client EMAIL QUOTA_BYTES INBOUND_ID [DEVICE_LIMIT]
add_client() {
    ACCOUNTS+=("$1")
    must_succeed "$(api POST /panel/api/clients/add "$(python3 - "$1" "$2" "$3" "${4:-0}" <<'PY'
import json, sys
email, total, inbound, limit = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4])
print(json.dumps({"client": {"email": email, "enable": True, "totalGB": total, "limitIp": limit}, "inboundIds": [inbound]}))
PY
)")" "adding client $1"
}

# client_link EMAIL SCHEME: print the client's one link of that scheme, asked
# for by subscription ID as the client's windows in the panel ask. A link the
# panel only gives out by email is one no admin ever sees.
client_link() {
    local sub
    sub="$(field "$(api GET "/panel/api/clients/get/$1")" 'r["obj"]["client"]["subId"]')"
    python3 - "$(api GET "/panel/api/clients/subLinks/$sub")" "$2" <<'PY'
import json, sys
links = json.loads(sys.argv[1])["obj"]
found = [l for l in links if l.startswith(sys.argv[2])]
if len(found) != 1:
    sys.exit("expected exactly one %s link, got: %r" % (sys.argv[2], links))
print(found[0])
PY
}

traffic_at_least() {
    local traffic
    traffic="$(api GET "/panel/api/clients/traffic/$1")"
    [ "$(field "$traffic" "r['obj']['up'] >= $2 and r['obj']['down'] >= $2")" = "True" ]
}

account_switched_off() {
    [ "$(field "$(api GET "/panel/api/clients/traffic/$1")" "r['obj']['enable']")" = "False" ]
}

reaches_internet() {
    ip netns exec "$CLIENT_NS" ping -c 2 -i 0.2 -W 2 "$INET_ADDR"
}

# use_traffic COUNT: send COUNT kilobyte-sized pings through the tunnel.
use_traffic() {
    ip netns exec "$CLIENT_NS" ping -c "$1" -i 0.05 -s 1000 -W 3 "$INET_ADDR" >/dev/null
}

SPEED_PORT=8099
SPEED_PID=""

# set_speed_limit EMAIL MBPS: change the account's speed limit and nothing else.
set_speed_limit() {
    must_succeed "$(api POST "/panel/api/clients/update/$1" "{\"speedLimit\": $2}")" "setting the speed limit of $1"
}

# start_speed_source: a server in the internet namespace that sends without end to whoever connects.
start_speed_source() {
    ip netns exec "$INET_NS" python3 - "$INET_ADDR" "$SPEED_PORT" >/dev/null 2>&1 <<'PY' &
import socket, sys, threading
srv = socket.create_server((sys.argv[1], int(sys.argv[2])))
chunk = b"\0" * 65536
def serve(c):
    try:
        c.recv(4096)
        c.sendall(b"HTTP/1.0 200 OK\r\nContent-Type: application/octet-stream\r\n\r\n")
        while True:
            c.sendall(chunk)
    except OSError:
        pass
    finally:
        c.close()
while True:
    conn, _ = srv.accept()
    threading.Thread(target=serve, args=(conn,), daemon=True).start()
PY
    SPEED_PID=$!
}

# download_mbps: the rate of a three-second download through the tunnel, in whole Mbps.
download_mbps() {
    local bytes_per_second
    bytes_per_second="$(ip netns exec "$CLIENT_NS" curl -s -o /dev/null --max-time 3 -w '%{speed_download}' \
        "http://${INET_ADDR}:${SPEED_PORT}/" || true)"
    python3 -c 'import sys; print(int(float(sys.argv[1] or 0) * 8 / 1e6))' "${bytes_per_second:-0}"
}

# The first second of a limited download also spends the bucket's initial credit.
rate_at_most() { [ "$(download_mbps)" -le "$1" ]; }

rate_above() { [ "$(download_mbps)" -gt "$1" ]; }

# check_speed_limit EMAIL: hold a connected account to 5 Mbps, then lift the limit.
check_speed_limit() {
    [ -n "$SPEED_PID" ] || start_speed_source
    wait_until 10 "the speed source to answer" rate_above 0
    set_speed_limit "$1" 5
    wait_until 30 "$1 to be held to 5 Mbps" rate_at_most 8
    set_speed_limit "$1" 0
    wait_until 30 "$1's limit to be lifted" rate_above 20
}

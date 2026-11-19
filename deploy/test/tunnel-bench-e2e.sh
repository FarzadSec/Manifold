#!/usr/bin/env bash
# End-to-end check of the tunnel test: two real panels, each the other's peer, build
# every kind of tunnel between them and measure it, as an admin runs it from the pages.
#
#   sudo deploy/test/tunnel-bench-e2e.sh /path/to/manifold /path/to/bin-folder
#
# The origin is the panel the library starts on this host, or the installed service
# with PANEL_URL; the relay is a second panel, the relay edition, in the client
# namespace behind a firewall that turns away what it did not ask for, as ufw does.
# The bin folder must hold backpack-linux-<arch>. Needs root, iproute2, iptables,
# curl and python3; it uses port 20542 and ports 41000 to 41079 besides the library's.
set -euo pipefail

BIN="$(readlink -f "${1:?usage: tunnel-bench-e2e.sh /path/to/manifold /path/to/bin-folder}")"
BIN_DIR="$(readlink -f "${2:?usage: tunnel-bench-e2e.sh /path/to/manifold /path/to/bin-folder}")"
XRAY_DIR="$BIN_DIR"
ENGINE_LOG_WORD=tunnel
# shellcheck source=deploy/test/vpn-e2e-lib.sh
source "$(dirname "$0")/vpn-e2e-lib.sh"

RELAY_ADDR=192.168.251.2
RELAY_PORT=20542
RELAY_BASE="http://${RELAY_ADDR}:${RELAY_PORT}/relay"
RELAY_JAR="$WORK/relay-cookies"
RELAY_TOKEN=""
RELAY_PID=""
ARCH="$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')"
# Every shape of tunnel the test builds; pck and sni pick the interface of the default
# route, which in these namespaces is not the one between the two panels.
KEYS='"reverse:tcpmux","reverse:ws","reverse:udp","l3:udp:server","l3:udp:client","l3:quic:client"'
# A kernel without GRE, IPIP and SIT, as in some containers, can say so with EXPECT_KERNEL_LINKS=0.
if [ "${EXPECT_KERNEL_LINKS:-1}" = 1 ]; then
    KEYS='"gre","ipip","sit","gretap",'"$KEYS"
fi
KEYS="[$KEYS]"

engine_cleanup() {
    if [ -n "$RELAY_PID" ]; then
        kill "$RELAY_PID" 2>/dev/null || true
        wait "$RELAY_PID" 2>/dev/null || true
    fi
    if [ -f "$WORK/relay.log" ] && [ "${KEEP_LOG:-}" = 1 ]; then
        cp "$WORK/relay.log" /tmp/tunnel-bench-relay.log
    fi
}

# relay_api METHOD PATH [JSON]: call the relay panel's API.
relay_api() {
    local method="$1" path="$2" body="${3:-}"
    if [ -n "$body" ]; then
        curl -fsS -b "$RELAY_JAR" -c "$RELAY_JAR" -X "$method" -H "X-CSRF-Token: $RELAY_TOKEN" \
            -H 'Content-Type: application/json' -d "$body" "$RELAY_BASE$path"
    else
        curl -fsS -b "$RELAY_JAR" -c "$RELAY_JAR" -X "$method" -H "X-CSRF-Token: $RELAY_TOKEN" "$RELAY_BASE$path"
    fi
}

relay_fail() {
    echo "--- last relay panel warnings and errors" >&2
    grep -E "WARNING|ERROR" "$WORK/relay.log" | tail -n 25 >&2 || true
    fail "$@"
}

start_relay() {
    local user=relay password=relay-pass-123 dir="$WORK/relay"
    mkdir -p "$dir/db" "$dir/log" "$dir/bin"
    cp "$BIN_DIR/backpack-linux-$ARCH" "$dir/bin/"
    local env=(env MANIFOLD_DB_FOLDER="$dir/db" MANIFOLD_LOG_FOLDER="$dir/log" MANIFOLD_BIN_FOLDER="$dir/bin"
        MANIFOLD_EDITION=tunnel MANIFOLD_LOG_LEVEL=debug)
    "${env[@]}" "$BIN" setting -port "$RELAY_PORT" -webBasePath /relay/ -username "$user" -password "$password" >/dev/null
    # The relay answers its panel port and what it asked for; the test must open the rest.
    for tool in iptables ip6tables; do
        ip netns exec "$CLIENT_NS" "$tool" -A INPUT -i lo -j ACCEPT
        ip netns exec "$CLIENT_NS" "$tool" -A INPUT -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
        ip netns exec "$CLIENT_NS" "$tool" -A INPUT -p tcp --dport "$RELAY_PORT" -j ACCEPT
        ip netns exec "$CLIENT_NS" "$tool" -P INPUT DROP
    done
    ip netns exec "$CLIENT_NS" "${env[@]}" "$BIN" >"$WORK/relay.log" 2>&1 &
    RELAY_PID=$!
    wait_until 40 "the relay panel to answer" curl -fsS -o /dev/null "$RELAY_BASE/csrf-token"
    RELAY_TOKEN="$(field "$(curl -fsS -c "$RELAY_JAR" "$RELAY_BASE/csrf-token")" 'r["obj"]')"
    must_succeed "$(curl -fsS -b "$RELAY_JAR" -c "$RELAY_JAR" -H "X-CSRF-Token: $RELAY_TOKEN" \
        --data-urlencode "username=$user" --data-urlencode "password=$password" "$RELAY_BASE/login")" "relay login"
    RELAY_TOKEN="$(field "$(curl -fsS -b "$RELAY_JAR" -c "$RELAY_JAR" "$RELAY_BASE/csrf-token")" 'r["obj"]')"
}

test_finished() {
    local state
    state="$(api GET /panel/api/tunnels/bench/state)"
    [ "$(field "$state" 'r["obj"].get("active") is None and r["obj"].get("last") is not None')" = "True" ]
}

no_test_links() {
    ! ip -o link show | grep -q ': mfx[0-9]' && ! ip netns exec "$CLIENT_NS" ip -o link show | grep -q ': mfx[0-9]'
}

log "network"
network
log "panels"
start_panel
start_relay

log "the relay makes a test link"
link="$(relay_api POST /panel/api/tunnels/bench/link \
    "{\"role\":\"server\",\"url\":\"${RELAY_BASE}/\",\"addr4\":\"${RELAY_ADDR}\",\"minutes\":30}")"
must_succeed "$link" "making the test link"
code="$(field "$link" 'r["obj"]["code"]')"
port="$(field "$link" 'r["obj"]["port"]')"
[ "$(field "$(relay_api GET /panel/api/tunnels/bench/state)" 'r["obj"]["link"]["code"]')" = "$code" ] ||
    relay_fail "the relay does not show its link waiting"

log "the origin runs it"
started="$(api POST /panel/api/tunnels/bench/start "$(python3 - "$code" "$KEYS" <<'PY'
import json, sys
print(json.dumps({"code": sys.argv[1], "profile": "quick", "keys": json.loads(sys.argv[2]), "addr4": "192.168.251.1"}))
PY
)")"
must_succeed "$started" "starting the test"
wait_until 30 "the relay to take part" \
    sh -c "curl -fsS -b '$RELAY_JAR' '$RELAY_BASE/panel/api/tunnels/bench/state' | grep -q '\"state\":\"running\"'"
wait_until 900 "the test to finish" test_finished

report="$(api GET /panel/api/tunnels/bench/state)"
python3 - "$report" <<'PY' || relay_fail "the origin's report is not what the test should give"
import json, sys
r = json.loads(sys.argv[1])["obj"]["last"]
results = {x["key"]: x for x in r["results"]}
for key, x in sorted(results.items()):
    print("  %-16s %-7s tcp %-5s %7.1f/%7.1f  udp %-5s %7.1f/%7.1f  score %5.1f  %s" % (
        key, x["state"], x["tcp"], x["downMbps"], x["upMbps"], x["udp"], x["udpDownMbps"], x["udpUpMbps"],
        x["score"], x.get("error") or x.get("warning") or ""))
problems = []
if r["state"] != "done" or not r["initiator"] or r["role"] != "client":
    problems.append("state %s, initiator %s, role %s, error %r" % (r["state"], r["initiator"], r["role"], r.get("error")))
for key, x in results.items():
    want_tcp = key != "reverse:udp"
    if x["state"] != "ok" or x["tcp"] != want_tcp or not x["udp"]:
        problems.append("%s: %s tcp=%s udp=%s (%s)" % (key, x["state"], x["tcp"], x["udp"], x.get("error") or x.get("warning")))
best = r.get("best")
if not best or results.get(best["key"], {}).get("stage") != "full":
    problems.append("no recommendation measured in full: %r" % (best,))
# The panel's end dials the relay, unless the relay dials a layer-3 tunnel itself.
if best:
    want = "" if best["kind"] == "l3" and best.get("dialer") == "server" else "192.168.251.2"
    if (best.get("remoteAddr") or "") != want:
        problems.append("the recommendation dials %r, want %r" % (best.get("remoteAddr"), want))
if not r.get("bestUdp") or r["bytes"] <= 0:
    problems.append("no UDP pick or no data moved")
print("  best %s (mtu %s), tcp %s, udp %s, %d MiB moved" % (
    best and best["key"], best and best.get("mtu"), (r.get("bestTcp") or {}).get("key"),
    (r.get("bestUdp") or {}).get("key"), r["bytes"] >> 20))
if problems:
    sys.exit("\n".join(problems))
PY

log "the relay holds the same outcome, and nothing of the test is left on either server"
wait_until 30 "the relay to finish its side" \
    sh -c "curl -fsS -b '$RELAY_JAR' '$RELAY_BASE/panel/api/tunnels/bench/history' | grep -q '\"state\":\"done\"'"
relay_history="$(relay_api GET /panel/api/tunnels/bench/history)"
[ "$(field "$relay_history" 'len(r["obj"]) == 1 and not r["obj"][0]["initiator"] and r["obj"][0]["side"] == "server"')" = "True" ] ||
    relay_fail "the relay's history: $relay_history"
origin_best="$(field "$report" 'r["obj"]["last"]["best"]["key"]')"
[ "$(field "$relay_history" 'r["obj"][0]["best"]')" = "$origin_best" ] || relay_fail "the relay's pick differs: $relay_history"
wait_until 20 "the test interfaces to go" no_test_links
if ip netns exec "$CLIENT_NS" iptables -S INPUT | grep -q 'manifold:bench' || iptables -S INPUT | grep -q 'manifold:bench'; then
    relay_fail "a firewall still lets the test in: $(ip netns exec "$CLIENT_NS" iptables -S INPUT | grep manifold)"
fi
if pgrep -af "backpack-linux-$ARCH" | grep -q tunnel-test; then
    relay_fail "a BackPack process of the test still runs: $(pgrep -af "backpack-linux-$ARCH" | grep tunnel-test)"
fi
inv_token="$(python3 - "$code" <<'PY'
import base64, json, sys
body = sys.argv[1].split("://", 1)[1]
print(json.loads(base64.urlsafe_b64decode(body + "=" * (-len(body) % 4)))["token"])
PY
)"
status="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Authorization: Bearer $inv_token" \
    -H 'Content-Type: application/json' -d '{}' "$RELAY_BASE/panel/api/tunnels/bench/peer/report")"
[ "$status" = 401 ] || relay_fail "the spent link's credential still reaches the relay: $status"

log "tunnel test e2e passed (ports ${port} to $((port + 79)))"

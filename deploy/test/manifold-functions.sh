#!/usr/bin/env bash
# Tests the functions of manifold.sh that do not need an installed panel. Run
# from the repository root.
set -euo pipefail

# shellcheck source=manifold.sh
source "$(dirname "$0")/../../manifold.sh"

failures=0
check() {
    local name="$1" got="$2" want="$3"
    if [ "$got" = "$want" ]; then
        echo "ok   ${name}"
    else
        echo "FAIL ${name}: got '${got}', want '${want}'"
        failures=$((failures + 1))
    fi
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# An uninstall takes the panel's files out of the folders it shares with other
# programs, and nothing else from them: not an admin's own interface, and not
# the original of an interface the panel took over.
mkdir -p "$work/wireguard" "$work/swanctl"
printf '# Managed by Manifold: inbound 3. Changes made here are overwritten.\n[Interface]\nPrivateKey = x\n' >"$work/wireguard/mfwg3.conf"
printf '[Interface]\n# Managed by Manifold: inbound 9. A line an admin copied.\nPrivateKey = y\n' >"$work/wireguard/wg0.conf"
printf '[Interface]\nPrivateKey = z\n' >"$work/wireguard/wg1.conf.before-manifold"
echo 'connections {}' >"$work/swanctl/manifold-l2tp.conf"
SWANCTL_CALLS=""
swanctl() { SWANCTL_CALLS="${SWANCTL_CALLS}[$*]"; }
remove_engine_files "$work/wireguard" "$work/swanctl/manifold-l2tp.conf"
check "only the panel's own files are removed" "$(cd "$work" && find . -type f | sort | tr '\n' ' ')" \
    "./wireguard/wg0.conf ./wireguard/wg1.conf.before-manifold "
check "strongSwan is told to drop the connection" "$SWANCTL_CALLS" "[--load-all --noprompt]"
SWANCTL_CALLS=""
remove_engine_files "$work/wireguard" "$work/swanctl/manifold-l2tp.conf"
check "strongSwan is left alone when there was nothing of the panel's" "$SWANCTL_CALLS" ""
unset -f swanctl

# Tunnel links outlive the panel's restarts, so an uninstall takes them down,
# with their tables and routing, and leaves an admin's own links alone.
mkdir -p "$work/net/mft4" "$work/net/mft9" "$work/net/gre1"
echo "manifold-tunnel:0123abcd" >"$work/net/mft4/ifalias"
echo "made by hand" >"$work/net/mft9/ifalias"
echo "manifold-tunnel:feed" >"$work/net/gre1/ifalias"
IP_CALLS="" NFT_CALLS=""
ip() {
    case "$*" in
        "rule show") printf '0:\tfrom all lookup local\n10004:\tfrom all fwmark 0x4d460004 lookup 20004\n32766:\tfrom all lookup main\n' ;;
        *) IP_CALLS="${IP_CALLS}[$*]" ;;
    esac
}
nft() {
    case "$*" in
        "list tables") printf 'table ip manifold_tf4\ntable inet manifold_tr4\ntable ip manifold_ovpn1\n' ;;
        *) NFT_CALLS="${NFT_CALLS}[$*]" ;;
    esac
}
remove_tunnels "$work/net"
check "only the panel's tunnel links and rules are removed" "$IP_CALLS" "[link del dev mft4][rule del pref 10004]"
check "only the tunnel tables are removed" "$NFT_CALLS" "[delete table ip manifold_tf4][delete table inet manifold_tr4]"
unset -f ip nft

# A fake panel binary: it records each call and answers the settings queries.
mkdir -p "$work/bin"
cat >"$work/bin/manifold" <<'FAKE'
#!/usr/bin/env bash
echo "[$*]" >>"$FAKE_LOG"
case "$*" in
    "setting -show") printf 'port: 2053\nsubPort: 2096\nwebBasePath: /p/\n' ;;
    "setting -getCert") printf 'cert: %s\nkey: %s\n' "${FAKE_CERT:-}" "${FAKE_CERT:+key.pem}" ;;
    "setting -getListen") echo "listenIP: " ;;
    "setting -reset") exit "${FAKE_RESET_EXIT:-0}" ;;
esac
FAKE
chmod +x "$work/bin/manifold"
BIN="$work/bin/manifold"
export FAKE_LOG="$work/calls.log"
need_root() { :; }
print_menu() { :; }
show_status() { :; }
RESTARTS=0
restart_if_running() { RESTARTS=$((RESTARTS + 1)); }
calls() { tr -d '\n' <"$FAKE_LOG"; : >"$FAKE_LOG"; }
: >"$FAKE_LOG"

# Menu numbers reach the action they name, and an answer that is not a port is refused.
menu_out=$(printf '7\n70000\n\n7\n2053\n\n0\n' | show_menu 2>&1)
check "change port refuses 70000 and sets 2053" "$(calls)" "[setting -port 2053]"
check "a refused answer returns to the menu" "$(grep -c 'did not finish' <<<"$menu_out")" "1"
printf '4\ny\nadmin\nsecret\ny\n\n0\n' | show_menu >/dev/null 2>&1
check "reset login passes the login and the 2FA reset" "$(calls)" "[setting -username admin -password secret -resetTwoFactor]"

# A menu action stops at its first failing command: a reset that failed must not
# restart the panel as if it had worked. Run under || or if, -e would be off.
RESTARTS=0
FAKE_RESET_EXIT=1 bash -c 'source "$1"; BIN="$2"; need_root() { :; }; print_menu() { :; }; show_status() { :; }
    restart_if_running() { echo RESTARTED; }; show_menu' _ "$(dirname "$0")/../../manifold.sh" "$BIN" \
    <<<$'6\ny\n\n0\n' >"$work/reset.out" 2>&1 || true
check "a failed reset does not restart the panel" "$(grep -c RESTARTED "$work/reset.out")" "0"
check "a failed reset is reported" "$(grep -c 'did not finish' "$work/reset.out")" "1"
: >"$FAKE_LOG"

check "no command and no terminal prints the usage, not a menu" "$(main </dev/null | head -n 1)" "Usage: manifold [command]"

# The certificate for the server's own address goes to the panel's issue command.
curl() { echo "203.0.113.9"; }
printf 'y\n' | cert_for_ip >/dev/null
check "an IP certificate is issued for the detected address" "$(head -n 1 "$FAKE_LOG")" "[cert issue -domain 203.0.113.9]"
: >"$FAKE_LOG"
unset -f curl

check "IPv4 octets stop at 255" "$(valid_ipv4 256.1.1.1 && echo yes || echo no)" "no"
check "an IPv4 address" "$(valid_ipv4 203.0.113.9 && echo yes || echo no)" "yes"
check "an IPv6 address is bracketed in a URL" "$(url_host 2001:db8::9)" "[2001:db8::9]"

# The address printed for the panel: https and the certificate's own name once it serves one.
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 1 -subj "/" \
    -addext "subjectAltName=IP:203.0.113.9,DNS:panel.example.com" \
    -keyout "$work/key.pem" -out "$work/cert.pem" >/dev/null 2>&1
check "an IP certificate's panel address" "$(FAKE_CERT="$work/cert.pem" access_url)" "https://203.0.113.9:2053/p/"
# shellcheck disable=SC2329 # public_ip calls it
curl() { echo "198.51.100.4"; }
check "without a certificate the panel address is plain HTTP on the public IP" "$(access_url)" "http://198.51.100.4:2053/p/"
unset -f curl
: >"$FAKE_LOG"

check "ports and ranges are read" "$(port_list 80,443,2000-2100 | tr '\n' ' ')" "80 443 2000-2100 "
check "a backwards range is refused" "$(port_list 2100-2000 || echo refused)" "refused"
check "a port past 65535 is refused" "$(port_list 70000 || echo refused)" "refused"
check "an empty item is refused" "$(port_list 80,,443 || echo refused)" "refused"
UFW_CALLS=""
ufw() { UFW_CALLS="${UFW_CALLS}[$*]"; }
ufw_ports allow 80,2000-2100
check "ufw opens a port and a range for TCP and UDP" "$UFW_CALLS" "[allow 80/tcp][allow 80/udp][allow 2000:2100/tcp][allow 2000:2100/udp]"
unset -f ufw

# Geo files keep the dataset's suffix here though each project names them plainly.
GEO_DIR="$work/geo"
mkdir -p "$GEO_DIR"
curl() {
    local out="" url="${*: -1}"
    while [ $# -gt 0 ]; do
        case "$1" in -o) out="$2"; shift 2 ;; *) shift ;; esac
    done
    echo "$url" >"$out"
    printf 200
}
GEO_UPDATED=0
update_geofiles IR >/dev/null
check "the IR dataset is saved under its own names" "$(cd "$GEO_DIR" && printf '%s ' *)" "geoip_IR.dat geosite_IR.dat "
check "the IR files come from chocolate4u" "$(cat "$GEO_DIR/geosite_IR.dat")" \
    "https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geosite.dat"
curl() { printf 304; }
echo old >"$GEO_DIR/geoip_IR.dat"
update_geofiles IR >/dev/null
check "a file the server says is current is kept" "$(cat "$GEO_DIR/geoip_IR.dat")" "old"
unset -f curl

# BBR is switched back to what was there before it.
SYSCTL_DIR="$work/sysctl"
mkdir -p "$SYSCTL_DIR"
SYSCTL_CALLS=""
sysctl() {
    case "$*" in
        "-n net.core.default_qdisc") echo pfifo_fast ;;
        "-n net.ipv4.tcp_congestion_control") echo cubic ;;
        *) SYSCTL_CALLS="${SYSCTL_CALLS}[$*]" ;;
    esac
}
bbr enable >/dev/null
check "BBR remembers what it replaced" "$(head -n 1 "$SYSCTL_DIR/99-bbr-manifold.conf")" "#pfifo_fast:cubic"
SYSCTL_CALLS=""
bbr disable >/dev/null
check "turning BBR off restores it" "$SYSCTL_CALLS" "[-w net.core.default_qdisc=pfifo_fast][-w net.ipv4.tcp_congestion_control=cubic]"
check "turning BBR off removes its file" "$(ls "$SYSCTL_DIR")" ""
unset -f sysctl

printf '[manifold-ipl]\nbantime=45m\n' >"$work/jail.conf"
check "the ban length of a written jail is kept" "$(jail_bantime "$work/jail.conf")" "45"
check "a jail not written yet bans for 30 minutes" "$(jail_bantime "$work/none.conf")" "30"

[ "$failures" -eq 0 ] || exit 1
echo "all management command function tests passed"

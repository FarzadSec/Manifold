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

[ "$failures" -eq 0 ] || exit 1
echo "all management command function tests passed"

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

[ "$failures" -eq 0 ] || exit 1
echo "all management command function tests passed"

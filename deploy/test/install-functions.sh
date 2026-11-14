#!/usr/bin/env bash
# Tests the pure functions of install.sh. Run from the repository root.
set -euo pipefail

# shellcheck source=install.sh
source "$(dirname "$0")/../../install.sh"

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

# The shape GitHub returns for a release: the release has its own "url" and
# "name", and every asset lists "url" before "name".
release_json='{
  "url": "https://api.github.com/repos/FarzadSec/Manifold/releases/1001",
  "name": "v0.1.0",
  "assets": [
    {
      "url": "https://api.github.com/repos/FarzadSec/Manifold/releases/assets/501",
      "id": 501,
      "name": "manifold-linux-amd64.tar.gz",
      "uploader": {
        "login": "github-actions[bot]",
        "url": "https://api.github.com/users/github-actions%5Bbot%5D"
      },
      "browser_download_url": "https://github.com/FarzadSec/Manifold/releases/download/v0.1.0/manifold-linux-amd64.tar.gz"
    },
    {
      "url": "https://api.github.com/repos/FarzadSec/Manifold/releases/assets/502",
      "id": 502,
      "name": "manifold-linux-amd64.tar.gz.sha256"
    },
    {
      "url": "https://api.github.com/repos/FarzadSec/Manifold/releases/assets/503",
      "id": 503,
      "name": "manifold-linux-arm64.tar.gz"
    }
  ]
}'

check "asset url of the amd64 package" \
    "$(asset_api_url "$release_json" manifold-linux-amd64.tar.gz)" \
    "https://api.github.com/repos/FarzadSec/Manifold/releases/assets/501"
check "asset url of the checksum, not of the package it is named after" \
    "$(asset_api_url "$release_json" manifold-linux-amd64.tar.gz.sha256)" \
    "https://api.github.com/repos/FarzadSec/Manifold/releases/assets/502"
check "asset url of the last asset" \
    "$(asset_api_url "$release_json" manifold-linux-arm64.tar.gz)" \
    "https://api.github.com/repos/FarzadSec/Manifold/releases/assets/503"
check "the release name is not mistaken for an asset" \
    "$(asset_api_url "$release_json" v0.1.0 || echo none)" "none"
check "a missing asset is reported, not guessed" \
    "$(asset_api_url "$release_json" manifold-linux-riscv64.tar.gz || echo none)" "none"

VERSION=""
check "the latest release is downloaded from its fixed address" \
    "$(release_file_url manifold-linux-amd64.tar.gz)" \
    "https://github.com/FarzadSec/Manifold/releases/latest/download/manifold-linux-amd64.tar.gz"
VERSION="v0.2.0"
check "a pinned version is downloaded from that release" \
    "$(release_file_url manifold-linux-arm64.tar.gz.sha256)" \
    "https://github.com/FarzadSec/Manifold/releases/download/v0.2.0/manifold-linux-arm64.tar.gz.sha256"
VERSION=""

check "x86_64 maps to amd64" "$(package_arch x86_64)" "amd64"
check "aarch64 maps to arm64" "$(package_arch aarch64)" "arm64"
check "an unsupported machine is refused" "$(package_arch mips || echo refused)" "refused"

parse_args --port 8443 --path /panel/ --username alice --password secret --no-start --package /tmp/p.tar.gz --version v0.2.0
check "parsed port" "$PANEL_PORT" "8443"
check "parsed path" "$PANEL_PATH" "/panel/"
check "parsed login" "${PANEL_USERNAME}:${PANEL_PASSWORD}" "alice:secret"
check "parsed no-start" "$START_SERVICE" "0"
check "parsed package and version" "${PACKAGE} ${VERSION}" "/tmp/p.tar.gz v0.2.0"

check "random string has the requested length" "$(random_string 18 | wc -c | tr -d ' ')" "18"

parse_args --no-deps
check "parsed no-deps" "$INSTALL_DEPS" "0"

settings_output='current panel settings as follows:
Warning: Panel is not secure with SSL
hasDefaultCredential: false
port: 23456
webBasePath: /abc/'
check "port read from the settings output" "$(port_from_settings <<<"$settings_output")" "23456"
check "no port when the settings could not be read" "$(port_from_settings <<<"open database failed")" ""

# service_listens_on must only accept the service's own process: a panel that
# cannot bind its port leaves whoever holds the port answering on it.
systemctl() { echo "$FAKE_MAIN_PID"; }
ss() { echo "$FAKE_SS"; }
listens() { if service_listens_on 23456; then echo yes; else echo no; fi; }

FAKE_MAIN_PID=4242
FAKE_SS='LISTEN 0 4096 *:23456 *:* users:(("manifold",pid=4242,fd=9))'
check "the service holds its port" "$(listens)" "yes"
FAKE_SS='LISTEN 0 511 0.0.0.0:23456 0.0.0.0:* users:(("nginx",pid=42421,fd=6),("nginx",pid=900,fd=6))'
check "another program holds the port" "$(listens)" "no"
FAKE_SS=''
check "nothing listens on the port" "$(listens)" "no"
FAKE_MAIN_PID=0
FAKE_SS='LISTEN 0 4096 *:23456 *:* users:(("manifold",pid=0,fd=9))'
check "the service has no running process" "$(listens)" "no"
unset -f systemctl ss

command_exists() { case " $FAKE_COMMANDS " in *" $1 "*) return 0 ;; *) return 1 ;; esac; }
FAKE_COMMANDS="openvpn nft"
check "missing packages are named" "$(missing_dependencies)" "iptables"
FAKE_COMMANDS="iptables"
check "the nft command comes from the nftables package" "$(missing_dependencies)" "openvpn nftables"
FAKE_COMMANDS="openvpn nft iptables"
check "nothing to install when every command exists" "$(missing_dependencies)" ""

# Another panel on the server usually holds the default subscription port.
FAKE_COMMANDS="ss"
ss() { [ "$3" = "sport = :2096" ] && echo 'LISTEN 0 4096 *:2096 *:*'; return 0; }
in_use() { if port_in_use "$1"; then echo yes; else echo no; fi; }
check "a port another program listens on" "$(in_use 2096)" "yes"
check "a free port" "$(in_use 2097)" "no"
PANEL_PORT=2097
check "a free port is neither taken nor the panel's own" "$(shuf() { printf '2096\n2097\n31337\n'; }; free_port)" "31337"
unset -f ss

[ "$failures" -eq 0 ] || exit 1
echo "all installer function tests passed"

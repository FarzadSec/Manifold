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

[ "$failures" -eq 0 ] || exit 1
echo "all installer function tests passed"

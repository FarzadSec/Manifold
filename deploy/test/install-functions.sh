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
FAKE_COMMANDS="openvpn nft wg-quick xl2tpd pppd swanctl"
check "missing packages are named" "$(missing_dependencies)" "iptables"
FAKE_COMMANDS="iptables pppd"
check "a command is named by the package that brings it" "$(missing_dependencies)" "openvpn nftables wireguard-tools xl2tpd strongswan"
FAKE_COMMANDS="openvpn nft iptables wg-quick xl2tpd pppd swanctl"
check "nothing to install when every command exists" "$(missing_dependencies)" ""
# A second IKE daemon would take ports 500 and 4500 from the one an admin already runs.
FAKE_COMMANDS="openvpn nft iptables wg-quick xl2tpd pppd ipsec"
check "an IPsec service of another kind is not joined by a second one" "$(missing_dependencies)" ""

# A relay of the tunnel edition needs no VPN engine, only links and netfilter.
ENV_FILE=$(mktemp)
EDITION=tunnel
set_edition
check "the tunnel edition is recorded for the service" "$(cat "$ENV_FILE")" "MANIFOLD_EDITION=tunnel"
FAKE_COMMANDS="iptables"
check "a relay installs only what tunnels need" "$(missing_dependencies)" "nftables iproute2"
EDITION=""
set_edition
check "an update without --edition keeps the edition" "$(installed_edition)" "tunnel"
EDITION=full
set_edition
check "the full edition clears the record" "$(installed_edition)" "full"
rm -f "$ENV_FILE"
EDITION=""

# Debian and Ubuntu ship strongSwan in pieces; everywhere else it is one package.
check "apt gets the strongSwan daemon and swanctl by their own names" \
    "$(packages_for apt-get openvpn strongswan ppp)" "openvpn strongswan-swanctl charon-systemd libstrongswan-standard-plugins ppp"
check "other package managers get the names as they are" "$(packages_for dnf openvpn strongswan ppp)" "openvpn strongswan ppp"

# The services of freshly installed L2TP packages: xl2tpd's own would take the
# panel's port, strongSwan's has to run. An admin's existing setup is not touched.
# Stopping is a call of its own: for a service that is an init script, as xl2tpd
# is up to Ubuntu 24.04, "disable --now" disables and leaves it running.
SYSTEMCTL_CALLS=""
systemctl() { SYSTEMCTL_CALLS="${SYSTEMCTL_CALLS}[$*]"; }
prepare_l2tp_services "openvpn xl2tpd ppp strongswan"
check "a new xl2tpd is kept off the port and a new strongSwan is started" "$SYSTEMCTL_CALLS" \
    "[stop xl2tpd][disable xl2tpd][enable strongswan][start strongswan]"
SYSTEMCTL_CALLS=""
prepare_l2tp_services "openvpn wireguard-tools"
check "services that were already there are left as they are" "$SYSTEMCTL_CALLS" ""

# One package a distribution does not have must not keep the others out, and
# only what really was installed gets its service set up.
FAKE_COMMANDS="apt-get openvpn nft iptables wg-quick pppd"
APT_CALLS=""
apt-get() {
    [ "$1" = install ] || return 0
    shift 3
    APT_CALLS="${APT_CALLS}[$*]"
    case " $* " in *" xl2tpd "*) return 1 ;; esac
}
info() { :; }
INSTALL_DEPS=1
install_dependencies 2>/dev/null
check "each engine's packages are installed on their own" "$APT_CALLS" "[xl2tpd][strongswan-swanctl charon-systemd libstrongswan-standard-plugins]"
check "only an installed package has its service set up" "$SYSTEMCTL_CALLS" "[enable strongswan][start strongswan]"
unset -f systemctl apt-get

# Another panel on the server usually holds the default subscription port.
FAKE_COMMANDS="ss"
ss() { [ "$3" = "sport = :2096" ] && echo 'LISTEN 0 4096 *:2096 *:*'; return 0; }
in_use() { if port_in_use "$1"; then echo yes; else echo no; fi; }
check "a port another program listens on" "$(in_use 2096)" "yes"
check "a free port" "$(in_use 2097)" "no"
PANEL_PORT=2097
check "a free port is neither taken nor the panel's own" "$(shuf() { printf '2096\n2097\n31337\n'; }; free_port)" "31337"
unset -f ss

# A fake panel binary for the first-install questions: it records each call.
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
INSTALL_DIR="$work/opt"
DATA_DIR="$work/data"
mkdir -p "$INSTALL_DIR" "$DATA_DIR"
cat >"$INSTALL_DIR/manifold" <<'FAKE'
#!/usr/bin/env bash
echo "[$*]" >>"$FAKE_LOG"
case "$1 $2" in "cert issue") exit "${FAKE_ISSUE_EXIT:-0}" ;; esac
FAKE
chmod +x "$INSTALL_DIR/manifold"
export FAKE_LOG="$work/calls.log"
calls() { tr -d '\n' <"$FAKE_LOG"; : >"$FAKE_LOG"; }
: >"$FAKE_LOG"
PANEL_PORT=2053 PANEL_PATH=/p/

# The default answer secures the panel with a certificate for the server's own address.
SERVER_IP=203.0.113.9 PANEL_SCHEME=http PANEL_HOST="" LISTEN_LOCAL=0
setup_ssl <<<"" >/dev/null
check "the default is a certificate for the server's IP" "$(calls)" "[cert issue -domain 203.0.113.9]"
check "the summary gives the HTTPS address" "$(access_line)" "https://203.0.113.9:2053/p/"

PANEL_SCHEME=http PANEL_HOST=""
setup_ssl <<<$'1\nnot a domain\npanel.example.com' >/dev/null
check "a domain certificate asks again until it gets a domain" "$(calls)" "[cert issue -domain panel.example.com]"
check "the summary gives the domain" "$(access_line)" "https://panel.example.com:2053/p/"

# A certificate that could not be had leaves the install going, on plain HTTP.
PANEL_SCHEME=http PANEL_HOST=""
FAKE_ISSUE_EXIT=1 setup_ssl <<<"2" >/dev/null 2>&1
check "a failed certificate leaves the panel on HTTP" "$(access_line)" "http://203.0.113.9:2053/p/"
: >"$FAKE_LOG"

SERVER_IP=2001:db8::9
check "an IPv6 address is bracketed" "$(access_line)" "http://[2001:db8::9]:2053/p/"
SERVER_IP=203.0.113.9

setup_ssl <<<$'4\ny' >/dev/null
check "plain HTTP behind an SSH tunnel listens on loopback" "$(calls)" "[setting -listenIP 127.0.0.1]"
check "the summary gives the tunnel" "$(access_line)" \
    "http://localhost:2222/p/  (after: ssh -L 2222:127.0.0.1:2053 root@203.0.113.9)"
LISTEN_LOCAL=0

# The port question: refused answers are asked again; without a terminal nothing is asked.
port_in_use() { [ "$1" = 8080 ]; }
PANEL_PORT=""
choose_port <<<$'y\nabc\n70000\n8080\n8443' >/dev/null
check "the chosen port, after a bad one and a taken one" "$PANEL_PORT" "8443"
PANEL_PORT=""
INTERACTIVE=0
free_port() { echo 31337; }
first_setup <<<$'y\n8443' >/dev/null
check "a first install without a terminal asks nothing" "$PANEL_PORT" "31337"
unset -f port_in_use free_port

# Which product a run installs: the flag, what an update finds, or the answer to a question.
ENV_FILE="$work/default"
: >"$ENV_FILE"
pick() { EDITION="$1" INTERACTIVE="$2"; resolve_edition <<<"$3" >/dev/null; echo "$EDITION"; }
check "--edition relay installs the relay" "$(pick relay 0 '')" "tunnel"
check "a first install from a terminal asks, and 2 is the relay" "$(pick '' 1 2)" "tunnel"
check "the first answer is the panel" "$(pick '' 1 '')" "full"
check "a first install without a terminal is the panel" "$(pick '' 0 2)" "full"
echo "MANIFOLD_EDITION=tunnel" >"$ENV_FILE"
touch "$DATA_DIR/manifold.db"
check "an update keeps the relay without asking" "$(pick '' 1 1)" "tunnel"
rm -f "$DATA_DIR/manifold.db"
: >"$ENV_FILE"

EDITION=full
check "the panel's package" "$(package_name amd64)" "manifold-linux-amd64.tar.gz"
EDITION=tunnel
check "the relay's package" "$(package_name arm64)" "manifold-relay-linux-arm64.tar.gz"

# The relay package has no Xray; the panel cannot run on it, so it is refused before anything is replaced.
mkdir -p "$work/pkg/bin"
touch "$work/pkg/bin/backpack-linux-amd64"
EDITION=full
check "the panel refuses the relay's package" "$( (check_package "$work/pkg") 2>&1 || true)" \
    "error: this is the Manifold Relay package; install it with --edition relay, or use the Manifold package"
EDITION=tunnel
check "the relay takes its package" "$( (check_package "$work/pkg") 2>&1 && echo ok)" "ok"

# A panel turned into a relay drops what only the panel runs, and keeps the rest.
mkdir -p "$work/bin"
touch "$work/bin/xray-linux-amd64" "$work/bin/mtg-linux-amd64" "$work/bin/geoip_IR.dat" "$work/bin/backpack-linux-amd64"
mkdir -p "$work/bin/tunnels"
prune_panel_engines "$work/bin"
check "a relay keeps BackPack and its tunnels" "$(cd "$work/bin" && printf '%s ' *)" "backpack-linux-amd64 tunnels "
EDITION=""

[ "$failures" -eq 0 ] || exit 1
echo "all installer function tests passed"

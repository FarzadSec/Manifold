#!/usr/bin/env bash
# Installs Manifold or Manifold Relay from a release package, or updates an
# existing install. Run as root. See `install.sh --help`.
set -euo pipefail

REPO="${MANIFOLD_REPO:-FarzadSec/Manifold}"
INSTALL_DIR=/usr/local/manifold
DATA_DIR=/etc/manifold
LOG_DIR=/var/log/manifold
ENV_FILE=/etc/default/manifold
UNIT_FILE=/etc/systemd/system/manifold.service
COMMAND=/usr/bin/manifold

PACKAGE=""
VERSION=""
PANEL_PORT=""
PANEL_PATH=""
PANEL_USERNAME=""
PANEL_PASSWORD=""
START_SERVICE=1
INSTALL_DEPS=1
DEFAULT_SUB_PORT=2096
SUB_PORT=""
FIRST_INSTALL=0
WORK_DIR=""
EDITION=""
INTERACTIVE=0
SERVER_IP=""
PANEL_SCHEME=http
PANEL_HOST=""
LISTEN_LOCAL=0
API_TOKEN=""

IP4_SOURCES=(https://api4.ipify.org https://ipv4.icanhazip.com https://v4.api.ipinfo.io/ip
    https://ipv4.myexternalip.com/raw https://4.ident.me https://check-host.net/ip)
IP6_SOURCES=(https://api6.ipify.org https://ipv6.icanhazip.com https://6.ident.me)

usage() {
    cat <<'USAGE'
Usage: install.sh [options]

Installs Manifold, or updates it in place when it is already installed.
The database and settings in /etc/manifold are kept on update.

Two products come from this installer: Manifold, the full panel, and Manifold
Relay, a tunnel server only (no Xray, inbounds or subscriptions) that users
connect to and that forwards their ports to a Manifold panel. A first install
from a terminal asks which; --edition chooses without asking.

Run from a terminal, a first install asks for the panel port (unless --port
is given) and how to secure the panel: a Let's Encrypt certificate for a
domain or for the server's IP address, certificate files of your own, or
plain HTTP. Without a terminal it asks nothing and the panel starts on HTTP;
get a certificate later with: manifold cert issue -domain NAME

  --package FILE     Install from a package file instead of downloading one
  --version vX.Y.Z   Install this release instead of the latest
  --port N           Panel port for a first install (default: random)
  --path /p/         Panel URL path for a first install (default: random)
  --username NAME    Panel login for a first install (default: random)
  --password PASS    Panel password for a first install (default: random)
  --no-start         Install the files but do not start the service
  --no-deps          Do not install the system packages the VPN engines need
  --edition NAME     full (Manifold, the default) or relay (Manifold Relay;
                     "tunnel" is the same). Kept on update.
  -h, --help         Show this help

Environment:
  MANIFOLD_REPO      owner/name of the repository (default: FarzadSec/Manifold)
  MANIFOLD_TOKEN     GitHub token, only for a private copy of the repository
USAGE
}

die() {
    echo "error: $*" >&2
    exit 1
}

# The EXIT trap runs after main has returned, so the directory cannot be a local of main.
cleanup() {
    [ -z "$WORK_DIR" ] || rm -rf "$WORK_DIR"
}

info() {
    echo "==> $*"
}

warn() {
    echo "warning: $*" >&2
}

valid_port() {
    [[ "$1" =~ ^[0-9]+$ ]] && [ "$1" -ge 1 ] && [ "$1" -le 65535 ]
}

valid_ipv4() {
    local IFS=. part
    [[ "$1" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]] || return 1
    for part in $1; do
        [ "$part" -le 255 ] || return 1
    done
}

valid_ipv6() {
    [[ "$1" == *:* && "$1" =~ ^[0-9A-Fa-f:.]+$ ]]
}

valid_domain() {
    [[ "$1" =~ ^([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$ ]]
}

# public_ip: this server's address as the internet sees it, IPv4 before IPv6.
public_ip() {
    local url ip
    for url in "${IP4_SOURCES[@]}"; do
        ip=$(curl -fsS --max-time 3 "$url" 2>/dev/null | tr -d '[:space:]"') || continue
        if valid_ipv4 "$ip"; then
            echo "$ip"
            return 0
        fi
    done
    for url in "${IP6_SOURCES[@]}"; do
        ip=$(curl -fsS --max-time 3 "$url" 2>/dev/null | tr -d '[:space:]"') || continue
        if valid_ipv6 "$ip"; then
            echo "$ip"
            return 0
        fi
    done
    return 1
}

# url_host HOST: the host as it goes in a URL, with an IPv6 address bracketed.
url_host() {
    case "$1" in *:*) echo "[$1]" ;; *) echo "$1" ;; esac
}

# cert_name FILE: the first name a certificate is for, DNS or IP.
cert_name() {
    command -v openssl >/dev/null 2>&1 || return 1
    openssl x509 -in "$1" -noout -ext subjectAltName 2>/dev/null |
        grep -oE '(DNS|IP Address):[^,[:space:]]+' | head -n 1 | cut -d: -f2- | sed 's/^\*\.//'
}

# Maps `uname -m` output to the architecture label used in package names.
package_arch() {
    case "$1" in
        x86_64 | amd64) echo amd64 ;;
        aarch64 | arm64) echo arm64 ;;
        *) return 1 ;;
    esac
}

# Prints the API download URL of the asset named $2 in the release JSON $1.
# GitHub lists each asset's "url" before its "name", one field per line.
asset_api_url() {
    awk -v want="$2" '
        /"url": "https:\/\/api\.github\.com\/repos\/[^"]*\/releases\/assets\/[0-9]+"/ {
            match($0, /https:[^"]+/)
            url = substr($0, RSTART, RLENGTH)
        }
        /"name": "/ {
            match($0, /"name": "[^"]+"/)
            name = substr($0, RSTART + 9, RLENGTH - 10)
            if (name == want && url != "") {
                print url
                found = 1
                exit
            }
        }
        END { if (!found) exit 1 }
    ' <<<"$1"
}

random_string() {
    LC_ALL=C tr -dc 'a-zA-Z0-9' </dev/urandom | head -c "$1" || true
}

# port_in_use PORT: some program already listens on the TCP port.
port_in_use() {
    command_exists ss || return 1
    [ -n "$(ss -H -ltn "sport = :$1" 2>/dev/null)" ]
}

# free_port: a random port nothing listens on, other than the panel's own.
free_port() {
    local candidate
    for candidate in $(shuf -i 20000-60000 -n 20); do
        [ "$candidate" = "$PANEL_PORT" ] && continue
        port_in_use "$candidate" && continue
        echo "$candidate"
        return 0
    done
    return 1
}

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --package) PACKAGE="${2:?--package needs a file}"; shift 2 ;;
            --version) VERSION="${2:?--version needs a tag}"; shift 2 ;;
            --port) PANEL_PORT="${2:?--port needs a number}"; shift 2 ;;
            --path) PANEL_PATH="${2:?--path needs a value}"; shift 2 ;;
            --username) PANEL_USERNAME="${2:?--username needs a value}"; shift 2 ;;
            --password) PANEL_PASSWORD="${2:?--password needs a value}"; shift 2 ;;
            --no-start) START_SERVICE=0; shift ;;
            --no-deps) INSTALL_DEPS=0; shift ;;
            --edition)
                EDITION="${2:?--edition needs full or relay}"
                case "$EDITION" in full | relay | tunnel) ;; *) die "--edition must be full or relay" ;; esac
                shift 2
                ;;
            -h | --help) usage; exit 0 ;;
            *) die "unknown option: $1 (see --help)" ;;
        esac
    done
}

github_curl() {
    if [ -n "${MANIFOLD_TOKEN:-}" ]; then
        curl -fsSL -H "Authorization: Bearer ${MANIFOLD_TOKEN}" "$@"
    else
        curl -fsSL "$@"
    fi
}

# Prints the public download URL of the release file named $1.
release_file_url() {
    if [ -n "$VERSION" ]; then
        echo "https://github.com/${REPO}/releases/download/${VERSION}/$1"
    else
        echo "https://github.com/${REPO}/releases/latest/download/$1"
    fi
}

# A private copy can only be read through the API, which needs the token.
download_with_token() {
    local asset="$1" dir="$2" release_url release url sum_url
    if [ -n "$VERSION" ]; then
        release_url="https://api.github.com/repos/${REPO}/releases/tags/${VERSION}"
    else
        release_url="https://api.github.com/repos/${REPO}/releases/latest"
    fi
    release=$(github_curl -H "Accept: application/vnd.github+json" "$release_url") ||
        die "cannot read ${release_url} with MANIFOLD_TOKEN. Unset it for a public repository."
    url=$(asset_api_url "$release" "$asset") || die "the release has no package named ${asset}"
    sum_url=$(asset_api_url "$release" "${asset}.sha256") || die "the release has no checksum for ${asset}"
    github_curl -H "Accept: application/octet-stream" -o "${dir}/${asset}" "$url"
    github_curl -H "Accept: application/octet-stream" -o "${dir}/${asset}.sha256" "$sum_url"
}

# package_name ARCH: the release file of the edition this run installs.
package_name() {
    if [ "$EDITION" = tunnel ]; then
        echo "manifold-relay-linux-$1.tar.gz"
    else
        echo "manifold-linux-$1.tar.gz"
    fi
}

# Downloads the package for $1 (arch) into $2 (directory) and verifies its checksum.
download_package() {
    local arch="$1" dir="$2" asset
    asset=$(package_name "$arch")
    info "Downloading ${asset}"
    if [ -n "${MANIFOLD_TOKEN:-}" ]; then
        download_with_token "$asset" "$dir"
    else
        curl -fsSL -o "${dir}/${asset}" "$(release_file_url "$asset")" ||
            die "cannot download $(release_file_url "$asset")"
        curl -fsSL -o "${dir}/${asset}.sha256" "$(release_file_url "${asset}.sha256")" ||
            die "cannot download the checksum of ${asset}"
    fi
    (cd "$dir" && sha256sum -c "${asset}.sha256" >/dev/null) || die "checksum mismatch for ${asset}"
    PACKAGE="${dir}/${asset}"
}

# check_package DIR: the unpacked package can run the edition this run installs.
check_package() {
    if [ "$EDITION" != tunnel ] && ! compgen -G "$1/bin/xray-linux-*" >/dev/null; then
        die "this is the Manifold Relay package; install it with --edition relay, or use the Manifold package"
    fi
}

# prune_panel_engines BIN_DIR: a panel turned into a relay drops what only the panel runs.
prune_panel_engines() {
    rm -f "$1"/xray-linux-* "$1"/mtg-linux-* "$1"/*.dat
}

install_files() {
    local work="$1" src
    tar -xzf "$PACKAGE" -C "$work"
    src="${work}/manifold"
    [ -x "${src}/manifold" ] || die "the package does not contain the manifold binary"
    check_package "$src"

    if systemctl is-active --quiet manifold 2>/dev/null; then
        info "Stopping the running service"
        systemctl stop manifold
    fi

    mkdir -p "${INSTALL_DIR}/bin" "$DATA_DIR" "$LOG_DIR"
    # The unit can only open for writing a directory that exists when it starts,
    # and strongSwan may be installed after the panel.
    [ -d /etc/wireguard ] || install -d -m 700 /etc/wireguard
    [ -d /etc/swanctl/conf.d ] || install -d -m 755 /etc/swanctl/conf.d
    chmod 700 "$DATA_DIR"
    install -m 755 "${src}/manifold" "${INSTALL_DIR}/manifold"
    cp -f "${src}"/bin/* "${INSTALL_DIR}/bin/"
    [ "$EDITION" != tunnel ] || prune_panel_engines "${INSTALL_DIR}/bin"
    chmod 755 "${INSTALL_DIR}"/bin/xray-linux-* "${INSTALL_DIR}"/bin/mtg-linux-* "${INSTALL_DIR}"/bin/backpack-linux-* 2>/dev/null || true
    install -m 755 "${src}/manifold.sh" "${INSTALL_DIR}/manifold.sh"
    install -m 755 "${src}/install.sh" "${INSTALL_DIR}/install.sh"
    install -m 755 "${src}/manifold.sh" "$COMMAND"
    install -m 644 "${src}/manifold.service" "$UNIT_FILE"
    [ -f "$ENV_FILE" ] || install -m 600 /dev/null "$ENV_FILE"
    set_edition
    systemctl daemon-reload
}

# installed_edition prints the edition the env file records: tunnel, or full.
installed_edition() {
    if [ -f "$ENV_FILE" ] && grep -q '^MANIFOLD_EDITION=tunnel$' "$ENV_FILE"; then
        echo tunnel
    else
        echo full
    fi
}

# choose_edition asks which product a first install is.
choose_edition() {
    local reply=""
    echo "What should this server run?"
    echo "  1. Manifold: the full panel (users, inbounds, every protocol, tunnels)"
    echo "  2. Manifold Relay: a tunnel server only, in front of a Manifold panel"
    read -rp "Choose [1]: " reply || true
    case "$reply" in
        2) EDITION=tunnel ;;
        *) EDITION=full ;;
    esac
}

# resolve_edition sets EDITION to what this run installs: --edition, the edition an
# update finds, or on a first install from a terminal the answer to choose_edition.
resolve_edition() {
    [ "$EDITION" != relay ] || EDITION=tunnel
    [ -z "$EDITION" ] || return 0
    if [ -f "${DATA_DIR}/manifold.db" ] || [ "$INTERACTIVE" -eq 0 ]; then
        EDITION=$(installed_edition)
    else
        choose_edition
    fi
}

# set_edition records the edition in the env file the service reads.
set_edition() {
    [ -n "$EDITION" ] || return 0
    sed -i '/^MANIFOLD_EDITION=/d' "$ENV_FILE"
    if [ "$EDITION" = tunnel ]; then
        echo "MANIFOLD_EDITION=tunnel" >>"$ENV_FILE"
    fi
}

# choose_port asks for the panel port; a random free one is taken when the answer is no.
choose_port() {
    local reply=""
    read -rp "Choose the panel port yourself? A random free port is used otherwise [y/N]: " reply || true
    case "$reply" in y | Y | yes) ;; *) return 0 ;; esac
    while true; do
        read -rp "Panel port [1-65535]: " reply || return 0
        if ! valid_port "$reply"; then
            echo "Not a port: ${reply}"
        elif port_in_use "$reply"; then
            echo "Another program listens on port ${reply}."
        else
            PANEL_PORT="$reply"
            return 0
        fi
    done
}

# Chooses the login, port and path of a new panel. An existing database is left alone.
first_setup() {
    FIRST_INSTALL=0
    [ -f "${DATA_DIR}/manifold.db" ] && return 0
    FIRST_INSTALL=1
    if [ -z "$PANEL_PORT" ] && [ "$INTERACTIVE" -eq 1 ]; then
        choose_port
    fi
    [ -n "$PANEL_PORT" ] || PANEL_PORT=$(free_port) || die "found no free port for the panel"
    # Another panel on the server usually holds the default subscription port,
    # and the subscription server could not start next to it.
    local sub_args=()
    # A relay runs no subscription server.
    if [ "$EDITION" != tunnel ] && port_in_use "$DEFAULT_SUB_PORT"; then
        SUB_PORT=$(free_port) || die "found no free port for the subscription server"
        sub_args=(-subPort "$SUB_PORT")
    fi
    [ -n "$PANEL_PATH" ] || PANEL_PATH="/$(random_string 18)/"
    [ -n "$PANEL_USERNAME" ] || PANEL_USERNAME=$(random_string 10)
    [ -n "$PANEL_PASSWORD" ] || PANEL_PASSWORD=$(random_string 16)
    info "Creating the panel database"
    local out
    out=$("${INSTALL_DIR}/manifold" setting -port "$PANEL_PORT" -webBasePath "$PANEL_PATH" \
        -username "$PANEL_USERNAME" -password "$PANEL_PASSWORD" "${sub_args[@]}" 2>&1) || die "first setup failed: ${out}"
    case "$out" in
        *Failed*) die "first setup failed: ${out}" ;;
    esac
}

# issue_panel_cert NAME: a Let's Encrypt certificate the panel serves and renews.
issue_panel_cert() {
    info "Getting a Let's Encrypt certificate for $1"
    if "${INSTALL_DIR}/manifold" cert issue -domain "$1"; then
        PANEL_SCHEME=https PANEL_HOST="$1"
        return 0
    fi
    warn "no certificate, so the panel answers over plain HTTP for now. HTTP-01 needs"
    warn "port 80 free and open to the internet; try again later with: manifold cert issue -domain $1"
}

# setup_ssl asks how a new panel is reached: over HTTPS with a certificate, or plain HTTP.
setup_ssl() {
    local choice="" domain="" cert="" key="" reply=""
    echo
    echo "How should the panel be reached?"
    echo "  1. HTTPS with a Let's Encrypt certificate for a domain that points at this server"
    echo "  2. HTTPS with a Let's Encrypt certificate for this server's IP address${SERVER_IP:+ (${SERVER_IP})}"
    echo "     (six-day certificates the panel renews on its own)"
    echo "  3. HTTPS with certificate files you already have"
    echo "  4. Plain HTTP (not recommended)"
    read -rp "Choose [2]: " choice || true
    case "${choice:-2}" in
        1)
            while ! valid_domain "$domain"; do
                read -rp "Domain: " domain || return 0
            done
            issue_panel_cert "$domain"
            ;;
        2)
            while ! valid_ipv4 "$SERVER_IP" && ! valid_ipv6 "$SERVER_IP"; do
                read -rp "This server's public IP address: " SERVER_IP || return 0
            done
            issue_panel_cert "$SERVER_IP"
            ;;
        3)
            read -rp "Certificate (full chain) file: " cert || true
            read -rp "Private key file: " key || true
            if [ -r "$cert" ] && [ -r "$key" ] && "${INSTALL_DIR}/manifold" cert -webCert "$cert" -webCertKey "$key" >/dev/null; then
                PANEL_SCHEME=https PANEL_HOST=$(cert_name "$cert" || true)
            else
                warn "the files were not taken; the panel answers over plain HTTP for now"
            fi
            ;;
        *)
            read -rp "Listen on 127.0.0.1 only, and reach the panel through an SSH tunnel? [y/N]: " reply || true
            case "$reply" in
                y | Y | yes)
                    "${INSTALL_DIR}/manifold" setting -listenIP 127.0.0.1 >/dev/null
                    LISTEN_LOCAL=1
                    ;;
            esac
            ;;
    esac
}

# mint_api_token: the token the summary shows. A panel's drives it from a bot or script;
# a relay's is tunnel-scoped, for the Manifold panel that manages its tunnels.
mint_api_token() {
    local args=(setting -getApiToken)
    [ "$EDITION" != tunnel ] || args+=(-tokenName main-panel -tokenScope tunnel)
    API_TOKEN=$("${INSTALL_DIR}/manifold" "${args[@]}" 2>/dev/null | sed -n 's/^apiToken: //p' || true)
}

command_exists() { command -v "$1" >/dev/null 2>&1; }

# missing_dependencies: the packages to install for the VPN engines, by the
# commands they provide.
missing_dependencies() {
    local missing=()
    # A relay runs tunnels only, which need the kernel's links and netfilter.
    if [ "$(installed_edition)" = tunnel ]; then
        command_exists nft || missing+=(nftables)
        command_exists iptables || missing+=(iptables)
        command_exists ip || missing+=(iproute2)
        echo "${missing[*]}"
        return 0
    fi
    command_exists openvpn || missing+=(openvpn)
    command_exists nft || missing+=(nftables)
    command_exists iptables || missing+=(iptables)
    command_exists wg-quick || missing+=(wireguard-tools)
    command_exists xl2tpd || missing+=(xl2tpd)
    command_exists pppd || missing+=(ppp)
    # "ipsec" is an IKE daemon set up another way; a second one would take its ports.
    command_exists swanctl || command_exists ipsec || missing+=(strongswan)
    echo "${missing[*]}"
}

# packages_for MANAGER PACKAGE...: the names that package manager knows the packages by.
packages_for() {
    local manager="$1" package
    local -a names=()
    shift
    for package in "$@"; do
        # Debian and Ubuntu split strongSwan: the daemon, swanctl and the ciphers old clients offer.
        if [ "$manager" = apt-get ] && [ "$package" = strongswan ]; then
            names+=(strongswan-swanctl charon-systemd libstrongswan-standard-plugins)
        else
            names+=("$package")
        fi
    done
    echo "${names[*]}"
}

# prepare_l2tp_services INSTALLED: set up the services of the L2TP packages this
# run installed. What was on the server before belongs to its admin.
prepare_l2tp_services() {
    local action
    case " $1 " in
        # The distribution's own xl2tpd service would hold port 1701, which the panel's server needs.
        # Two calls: "disable --now" leaves a service that is an init script running.
        *" xl2tpd "*) for action in stop disable; do systemctl "$action" xl2tpd >/dev/null 2>&1 || true; done ;;
    esac
    case " $1 " in
        # The panel only hands strongSwan a connection; the daemon itself has to be running.
        *" strongswan "*) for action in enable start; do systemctl "$action" strongswan >/dev/null 2>&1 || true; done ;;
    esac
}

# package_manager prints the package manager of this distribution.
package_manager() {
    local manager
    for manager in apt-get dnf yum zypper pacman; do
        if command_exists "$manager"; then
            echo "$manager"
            return 0
        fi
    done
    return 1
}

# install_package MANAGER NAME...: install the packages of one dependency.
install_package() {
    local manager="$1"
    shift
    case "$manager" in
        apt-get) apt-get install -y -qq "$@" ;;
        dnf | yum) "$manager" install -y -q "$@" ;;
        zypper) zypper --non-interactive --quiet install "$@" ;;
        pacman) pacman -Sy --noconfirm --needed "$@" ;;
    esac >/dev/null 2>&1
}

# install_dependencies never fails the install: the Xray protocols run without
# these packages, and only the engines that need one say so in the panel.
install_dependencies() {
    [ "$INSTALL_DEPS" -eq 1 ] || return 0
    local missing manager package
    local -a names installed=()
    missing=$(missing_dependencies)
    [ -n "$missing" ] || return 0
    info "Installing system packages: ${missing}"
    if manager=$(package_manager); then
        case "$manager" in
            apt-get)
                export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a
                apt-get update -qq >/dev/null 2>&1 || true
                ;;
            # On RHEL and its rebuilds openvpn and xl2tpd come from EPEL.
            dnf | yum) "$manager" install -y -q epel-release >/dev/null 2>&1 || true ;;
        esac
        # One at a time: a package this distribution lacks must not keep the others out.
        for package in $missing; do
            read -r -a names <<<"$(packages_for "$manager" "$package")"
            if install_package "$manager" "${names[@]}"; then
                installed+=("$package")
            fi
        done
    fi
    missing=$(missing_dependencies)
    if [ -n "$missing" ]; then
        echo "warning: could not install: ${missing}" >&2
        echo "         OpenVPN, WireGuard and L2TP inbounds will not start until these packages are installed." >&2
    fi
    prepare_l2tp_services "${installed[*]}"
}

# port_from_settings reads the panel port out of "manifold setting -show".
port_from_settings() {
    awk -F': *' '$1 == "port" { print $2; exit }'
}

# service_listens_on PORT: the service's own process holds the port.
service_listens_on() {
    local pid
    pid=$(systemctl show manifold --property MainPID --value 2>/dev/null)
    [ -n "$pid" ] && [ "$pid" != 0 ] || return 1
    ss -H -ltnp "sport = :$1" 2>/dev/null | grep -q "pid=${pid},"
}

start_service() {
    [ "$START_SERVICE" -eq 1 ] || return 0
    info "Starting the service"
    systemctl enable --quiet manifold
    systemctl restart manifold
    local port
    port=$("${INSTALL_DIR}/manifold" setting -show 2>/dev/null | port_from_settings)
    # A panel that cannot serve still counts as "active" for its first second,
    # so the proof is its own process holding the panel port.
    if [ -n "$port" ] && command_exists ss; then
        for _ in $(seq 1 30); do
            service_listens_on "$port" && return 0
            sleep 1
        done
        journalctl -u manifold -n 30 --no-pager >&2 || true
        die "the panel is not listening on port ${port}; its last log lines are above"
    fi
    for _ in 1 2 3 4 5 6 7 8 9 10; do
        systemctl is-active --quiet manifold && return 0
        sleep 1
    done
    die "the service did not start; see: journalctl -u manifold -n 50"
}

# access_line: the address the panel answers on, as the summary prints it.
access_line() {
    local host="${PANEL_HOST:-${SERVER_IP:-SERVER-IP}}"
    if [ "$LISTEN_LOCAL" -eq 1 ]; then
        echo "http://localhost:2222${PANEL_PATH}  (after: ssh -L 2222:127.0.0.1:${PANEL_PORT} root@$(url_host "${SERVER_IP:-SERVER-IP}"))"
    else
        echo "${PANEL_SCHEME}://$(url_host "$host"):${PANEL_PORT}${PANEL_PATH}"
    fi
}

print_summary() {
    local version line="═══════════════════════════════════════════════════════════════"
    version=$("${INSTALL_DIR}/manifold" -v 2>/dev/null || echo unknown)
    echo
    local product=Manifold
    [ "$(installed_edition)" != tunnel ] || product="Manifold Relay"
    if [ "$FIRST_INSTALL" -ne 1 ]; then
        echo "${product} ${version} is installed. The existing database and settings were kept."
        echo "Manage it with: manifold"
        return 0
    fi
    echo "$line"
    echo "  ${product} ${version} is installed"
    echo "$line"
    echo "  Username:     ${PANEL_USERNAME}"
    echo "  Password:     ${PANEL_PASSWORD}"
    echo "  Port:         ${PANEL_PORT}"
    echo "  WebBasePath:  ${PANEL_PATH}"
    echo "  Access URL:   $(access_line)"
    if [ -n "$API_TOKEN" ] && [ "$product" = "Manifold Relay" ]; then
        echo "  Relay token:  ${API_TOKEN}"
    elif [ -n "$API_TOKEN" ]; then
        echo "  API token:    ${API_TOKEN}"
    fi
    echo "$line"
    echo "This is shown once. Sign in, then change the login under Panel Settings."
    echo "Open port ${PANEL_PORT}/tcp in your firewall if the page does not load."
    if [ "$PANEL_SCHEME" = http ] && [ "$LISTEN_LOCAL" -eq 0 ]; then
        echo "The panel is on plain HTTP. Get a certificate any time: manifold cert issue -domain NAME"
    fi
    if [ "$product" = "Manifold Relay" ]; then
        echo
        echo "Connect it to your Manifold panel, one of two ways:"
        echo "  - On the panel: Tunnels → Relays → add this relay's Access URL and its relay token."
        echo "    The panel then sets up and keeps this relay's end of every tunnel you give it."
        echo "  - Or paste a tunnel's pairing code here: manifold tunnel import CODE"
    fi
    if [ -n "$SUB_PORT" ]; then
        echo "Subscriptions use port ${SUB_PORT}: port ${DEFAULT_SUB_PORT} is taken by another program on this server."
    fi
    echo
    echo "Manage it with: manifold"
}

main() {
    parse_args "$@"
    [ "$(id -u)" -eq 0 ] || die "run this installer as root"
    command -v systemctl >/dev/null || die "systemd is required"
    command -v tar >/dev/null || die "tar is required"

    local arch
    arch=$(package_arch "$(uname -m)") || die "unsupported architecture: $(uname -m)"
    WORK_DIR=$(mktemp -d)
    trap cleanup EXIT
    if [ -t 0 ]; then
        INTERACTIVE=1
    fi
    resolve_edition

    if [ -z "$PACKAGE" ]; then
        command -v curl >/dev/null || die "curl is required to download a release"
        download_package "$arch" "$WORK_DIR"
    else
        [ -f "$PACKAGE" ] || die "package not found: ${PACKAGE}"
    fi

    install_files "$WORK_DIR"
    install_dependencies
    first_setup
    if [ "$FIRST_INSTALL" -eq 1 ]; then
        info "Finding this server's public address"
        SERVER_IP=$(public_ip || true)
        # A scripted install, such as a node the master sets up over SSH, gets no extra admin token.
        if [ "$INTERACTIVE" -eq 1 ]; then
            setup_ssl
            mint_api_token
        fi
    fi
    start_service
    print_summary
}

# Runs when executed or piped into bash, not when sourced by a test.
if [ -z "${BASH_SOURCE[0]:-}" ] || [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi

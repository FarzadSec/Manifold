#!/usr/bin/env bash
# Installs Manifold from a release package, or updates an existing install.
# Run as root. See `install.sh --help`.
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

usage() {
    cat <<'USAGE'
Usage: install.sh [options]

Installs Manifold, or updates it in place when it is already installed.
The database and settings in /etc/manifold are kept on update.

  --package FILE     Install from a package file instead of downloading one
  --version vX.Y.Z   Install this release instead of the latest
  --port N           Panel port for a first install (default: random)
  --path /p/         Panel URL path for a first install (default: random)
  --username NAME    Panel login for a first install (default: random)
  --password PASS    Panel password for a first install (default: random)
  --no-start         Install the files but do not start the service
  --no-deps          Do not install the system packages the VPN engines need
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

# Downloads the package for $1 (arch) into $2 (directory) and verifies its checksum.
download_package() {
    local arch="$1" dir="$2" asset
    asset="manifold-linux-${arch}.tar.gz"
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

install_files() {
    local work="$1" src
    tar -xzf "$PACKAGE" -C "$work"
    src="${work}/manifold"
    [ -x "${src}/manifold" ] || die "the package does not contain the manifold binary"

    if systemctl is-active --quiet manifold 2>/dev/null; then
        info "Stopping the running service"
        systemctl stop manifold
    fi

    mkdir -p "${INSTALL_DIR}/bin" "$DATA_DIR" "$LOG_DIR"
    # The unit can only open for writing a directory that exists when it starts.
    [ -d /etc/wireguard ] || install -d -m 700 /etc/wireguard
    chmod 700 "$DATA_DIR"
    install -m 755 "${src}/manifold" "${INSTALL_DIR}/manifold"
    cp -f "${src}"/bin/* "${INSTALL_DIR}/bin/"
    chmod 755 "${INSTALL_DIR}"/bin/xray-linux-* "${INSTALL_DIR}"/bin/mtg-linux-* 2>/dev/null || true
    install -m 755 "${src}/manifold.sh" "${INSTALL_DIR}/manifold.sh"
    install -m 755 "${src}/install.sh" "${INSTALL_DIR}/install.sh"
    install -m 755 "${src}/manifold.sh" "$COMMAND"
    install -m 644 "${src}/manifold.service" "$UNIT_FILE"
    [ -f "$ENV_FILE" ] || install -m 600 /dev/null "$ENV_FILE"
    systemctl daemon-reload
}

# Chooses the login, port and path of a new panel. An existing database is left alone.
first_setup() {
    FIRST_INSTALL=0
    [ -f "${DATA_DIR}/manifold.db" ] && return 0
    FIRST_INSTALL=1
    [ -n "$PANEL_PORT" ] || PANEL_PORT=$(free_port) || die "found no free port for the panel"
    # Another panel on the server usually holds the default subscription port,
    # and the subscription server could not start next to it.
    local sub_args=()
    if port_in_use "$DEFAULT_SUB_PORT"; then
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

command_exists() { command -v "$1" >/dev/null 2>&1; }

# missing_dependencies: the packages to install for the VPN engines, by the
# commands they provide.
missing_dependencies() {
    local missing=()
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
    case " $1 " in
        # The distribution's own xl2tpd service would hold port 1701, which the panel's server needs.
        *" xl2tpd "*) systemctl disable --now xl2tpd >/dev/null 2>&1 || true ;;
    esac
    case " $1 " in
        # The panel only hands strongSwan a connection; the daemon itself has to be running.
        *" strongswan "*) systemctl enable --now strongswan >/dev/null 2>&1 || true ;;
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

print_summary() {
    local version
    version=$("${INSTALL_DIR}/manifold" -v 2>/dev/null || echo unknown)
    echo
    echo "Manifold ${version} is installed."
    if [ "$FIRST_INSTALL" -eq 1 ]; then
        cat <<SUMMARY

  Address:   http://SERVER-IP:${PANEL_PORT}${PANEL_PATH}
  Username:  ${PANEL_USERNAME}
  Password:  ${PANEL_PASSWORD}

This is shown once. Sign in, then change the login under Panel Settings.
Open port ${PANEL_PORT}/tcp in your firewall if the page does not load.
SUMMARY
        if [ -n "$SUB_PORT" ]; then
            echo "Subscriptions use port ${SUB_PORT}: port ${DEFAULT_SUB_PORT} is taken by another program on this server."
        fi
    else
        echo "The existing database and settings were kept."
    fi
    echo
    echo "Manage it with: manifold help"
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

    if [ -z "$PACKAGE" ]; then
        command -v curl >/dev/null || die "curl is required to download a release"
        download_package "$arch" "$WORK_DIR"
    else
        [ -f "$PACKAGE" ] || die "package not found: ${PACKAGE}"
    fi

    install_files "$WORK_DIR"
    install_dependencies
    first_setup
    start_service
    print_summary
}

# Runs when executed or piped into bash, not when sourced by a test.
if [ -z "${BASH_SOURCE[0]:-}" ] || [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi

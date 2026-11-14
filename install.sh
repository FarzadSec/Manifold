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

random_port() {
    shuf -i 20000-60000 -n 1
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
    [ -n "$PANEL_PORT" ] || PANEL_PORT=$(random_port)
    [ -n "$PANEL_PATH" ] || PANEL_PATH="/$(random_string 18)/"
    [ -n "$PANEL_USERNAME" ] || PANEL_USERNAME=$(random_string 10)
    [ -n "$PANEL_PASSWORD" ] || PANEL_PASSWORD=$(random_string 16)
    info "Creating the panel database"
    local out
    out=$("${INSTALL_DIR}/manifold" setting -port "$PANEL_PORT" -webBasePath "$PANEL_PATH" \
        -username "$PANEL_USERNAME" -password "$PANEL_PASSWORD" 2>&1) || die "first setup failed: ${out}"
    case "$out" in
        *Failed*) die "first setup failed: ${out}" ;;
    esac
}

start_service() {
    [ "$START_SERVICE" -eq 1 ] || return 0
    info "Starting the service"
    systemctl enable --quiet manifold
    systemctl restart manifold
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
    first_setup
    start_service
    print_summary
}

# Runs when executed or piped into bash, not when sourced by a test.
if [ -z "${BASH_SOURCE[0]:-}" ] || [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi

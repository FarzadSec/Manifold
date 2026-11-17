#!/usr/bin/env bash
# Manages a Manifold install. Installed as /usr/bin/manifold; with no command it
# opens a menu, so the panel can be repaired while its web pages are down.
set -euo pipefail

REPO="${MANIFOLD_REPO:-FarzadSec/Manifold}"
INSTALL_DIR=/usr/local/manifold
BIN="${INSTALL_DIR}/manifold"
DATA_DIR=/etc/manifold
ENV_FILE=/etc/default/manifold
LOG_DIR="${MANIFOLD_LOG_FOLDER:-/var/log/manifold}"
GEO_DIR="${INSTALL_DIR}/bin"
SYSCTL_DIR=/etc/sysctl.d
JAIL=manifold-ipl
MENU_DONE=100

if [ -t 1 ]; then
    red=$'\033[0;31m' green=$'\033[0;32m' yellow=$'\033[0;33m' plain=$'\033[0m'
else
    red="" green="" yellow="" plain=""
fi

IP4_SOURCES=(https://api4.ipify.org https://ipv4.icanhazip.com https://v4.api.ipinfo.io/ip
    https://ipv4.myexternalip.com/raw https://4.ident.me https://check-host.net/ip)
IP6_SOURCES=(https://api6.ipify.org https://ipv6.icanhazip.com https://6.ident.me)

usage() {
    cat <<'USAGE'
Usage: manifold [command]

With no command, manifold opens a menu with everything below.

Service
  start | stop | restart     Control the panel service
  restart-xray               Restart Xray only
  status                     Show the service state
  enable | disable           Start the panel at boot, or not
  log [N]                    Show the last N log lines (default 100)
  log -f                     Follow the log
  doctor                     Look for what keeps the panel from being reached

Panel
  settings                   Show the panel port, URL path, address and login state
  login [NAME PASSWORD]      Set the panel login; random when omitted
  port N                     Set the panel port
  path /p/                   Set the panel URL path
  cert ...                   Get or set the panel's certificate (see: manifold cert)
  backup ...                 Make, open or restore a full backup (see: manifold backup)
  setting ...                Pass options to the panel's own `setting` command
  migrate-db ...             Pass options to the panel's `migrate-db` command
  version                    Show the installed version

IP limit
  iplimit install            Install Fail2ban and the jail that enforces client IP limits
  iplimit remove             Remove the jail
  iplimit status             Show the jail state
  banlog                     Show the last bans

Server
  bbr enable|disable         Switch TCP congestion control to BBR, or back
  geo [main|IR|RU|all]       Update Xray's geo files (update-all-geofiles: all)

Maintenance
  update [installer options] Install the latest release, or --version vX.Y.Z
  uninstall [--purge]        Remove Manifold; --purge also deletes its data
USAGE
}

die() {
    echo "${red}error:${plain} $*" >&2
    exit 1
}

info() { echo "${green}$*${plain}"; }
warn() { echo "${yellow}$*${plain}"; }

need_root() {
    [ "$(id -u)" -eq 0 ] || die "run this command as root"
}

random_string() {
    LC_ALL=C tr -dc 'a-zA-Z0-9' </dev/urandom | head -c "$1" || true
}

# confirm QUESTION [y|n]: the answer, with the default for an empty one.
confirm() {
    local reply="" default="${2:-n}" hint="[y/N]"
    [ "$default" = y ] && hint="[Y/n]"
    read -rp "$1 $hint: " reply || true
    [ -n "$reply" ] || reply="$default"
    case "$reply" in y | Y | yes | YES) return 0 ;; *) return 1 ;; esac
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
    [[ "$1" =~ ^(\*\.)?([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$ ]]
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

restart_if_running() {
    if systemctl is-active --quiet manifold 2>/dev/null; then
        systemctl restart manifold
        info "The panel was restarted."
    fi
}

apply_setting() {
    need_root
    local out
    out=$("$BIN" setting "$@" 2>&1) || die "$out"
    case "$out" in
        *Failed*) die "$out" ;;
    esac
    restart_if_running
}

set_login() {
    local name="${1:-}" pass="${2:-}"
    [ -n "$name" ] || name=$(random_string 10)
    [ -n "$pass" ] || pass=$(random_string 16)
    apply_setting -username "$name" -password "$pass"
    echo "Username: ${name}"
    echo "Password: ${pass}"
}

# setting_value KEY: one line of "manifold setting -show".
setting_value() {
    "$BIN" setting -show 2>/dev/null | awk -F': *' -v k="$1" '$1 == k { print $2; exit }'
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

access_url() {
    local port path cert host scheme=http
    port=$(setting_value port)
    path=$(setting_value webBasePath)
    cert=$("$BIN" setting -getCert 2>/dev/null | sed -n 's/^cert: //p')
    if [ -n "$cert" ]; then
        scheme=https
        host=$(cert_name "$cert" || true)
    fi
    [ -n "${host:-}" ] || host=$(public_ip || echo SERVER-IP)
    echo "${scheme}://$(url_host "$host"):${port}${path}"
}

show_settings() {
    "$BIN" setting -show
    "$BIN" setting -getListen | grep -v '^listenIP: *$' || true
    "$BIN" setting -getCert | grep -E '^(cert|key): .' || true
    echo "Access URL: $(access_url)"
}

xray_running() {
    pgrep -f "${INSTALL_DIR}/bin/xray-linux" >/dev/null 2>&1
}

show_status() {
    local state edition=full
    if [ ! -x "$BIN" ]; then
        echo "Panel state: ${red}Not installed${plain}"
        return 0
    fi
    if systemctl is-active --quiet manifold 2>/dev/null; then
        state="${green}Running${plain}"
    else
        state="${yellow}Not running${plain}"
    fi
    echo "Panel state: ${state}"
    if systemctl is-enabled --quiet manifold 2>/dev/null; then
        echo "Start automatically: ${green}Yes${plain}"
    else
        echo "Start automatically: ${red}No${plain}"
    fi
    if [ -f "$ENV_FILE" ] && grep -q '^MANIFOLD_EDITION=tunnel$' "$ENV_FILE"; then
        edition=tunnel
    fi
    if [ "$edition" = tunnel ]; then
        echo "Edition: tunnel server (no Xray)"
    elif xray_running; then
        echo "Xray state: ${green}Running${plain}"
    else
        echo "Xray state: ${red}Not running${plain}"
    fi
}

restart_xray() {
    need_root
    systemctl reload manifold
    info "Xray is restarting; its state shows in a few seconds."
}

# --- menu actions on the panel's settings ---

reset_login() {
    local name="" pass="" args
    confirm "Reset the panel's username and password?" || return 0
    read -rp "New username [random]: " name || true
    read -rp "New password [random]: " pass || true
    [ -n "$name" ] || name=$(random_string 10)
    [ -n "$pass" ] || pass=$(random_string 16)
    args=(-username "$name" -password "$pass")
    if confirm "Also turn off two-factor authentication?"; then
        args+=(-resetTwoFactor)
    fi
    apply_setting "${args[@]}"
    echo "Username: ${green}${name}${plain}"
    echo "Password: ${green}${pass}${plain}"
}

reset_path() {
    local path
    confirm "Give the panel a new random URL path?" || return 0
    path="/$(random_string 18)/"
    apply_setting -webBasePath "$path"
    echo "URL path: ${green}${path}${plain}"
}

reset_settings() {
    confirm "Reset every panel setting to its default? Inbounds, users and the login are kept" || return 0
    need_root
    "$BIN" setting -reset
    restart_if_running
}

change_port() {
    local port=""
    read -rp "New panel port [1-65535]: " port || true
    valid_port "$port" || die "not a port: ${port}"
    apply_setting -port "$port"
    echo "The panel answers on port ${green}${port}${plain}. Open it in the firewall if needed."
}

reset_two_factor() {
    confirm "Turn off two-factor authentication for the panel owner?" || return 0
    apply_setting -resetTwoFactor
}

new_api_token() {
    confirm "Make a new API token? A CLI token made before stops working" || return 0
    need_root
    "$BIN" setting -getApiToken
}

listen_everywhere() {
    apply_setting -listenIP 0.0.0.0
    info "The panel listens on every address again."
}

ssh_forwarding() {
    local listen port path ip choice=""
    listen=$("$BIN" setting -getListen | sed -n 's/^listenIP: //p')
    port=$(setting_value port)
    path=$(setting_value webBasePath)
    echo "  1. Listen on 127.0.0.1 only, and reach the panel through an SSH tunnel"
    echo "  2. Listen on every address again"
    echo "  0. Back"
    read -rp "Choose: " choice || true
    case "$choice" in
        1)
            apply_setting -listenIP 127.0.0.1
            listen=127.0.0.1
            ;;
        2) listen_everywhere; return 0 ;;
        *) return 0 ;;
    esac
    ip=$(public_ip || echo SERVER-IP)
    echo "From your computer:"
    echo "  ${yellow}ssh -L 2222:${listen}:${port} root@${ip}${plain}"
    echo "then open ${yellow}http://localhost:2222${path}${plain}"
}

# --- certificates ---

# issue_cert ARGS...: "manifold cert issue" with ARGS, then the panel serves it.
issue_cert() {
    need_root
    if "$BIN" cert issue "$@"; then
        restart_if_running
        echo "Panel address: ${green}$(access_url)${plain}"
        return 0
    fi
    warn "HTTP-01 needs port 80 free on this server and open to the internet."
    return 1
}

cert_for_domain() {
    local domain="" email=""
    read -rp "Domain that points at this server: " domain || true
    valid_domain "$domain" && [[ "$domain" != \** ]] || die "not a domain name: ${domain}"
    read -rp "Email for Let's Encrypt's notices [none]: " email || true
    if [ -n "$email" ]; then
        issue_cert -domain "$domain" -email "$email"
    else
        issue_cert -domain "$domain"
    fi
}

cert_for_ip() {
    local ip=""
    ip=$(public_ip || true)
    if [ -z "$ip" ] || ! confirm "Get a certificate for ${ip}?" y; then
        read -rp "This server's public IP address: " ip || true
    fi
    valid_ipv4 "$ip" || valid_ipv6 "$ip" || die "not an IP address: ${ip}"
    echo "Let's Encrypt gives an IP address a six-day certificate; the panel renews it on its own."
    issue_cert -domain "$ip"
}

cert_over_dns() {
    local provider="" choice="" names="" token=""
    echo "  1. Cloudflare"
    echo "  2. ArvanCloud"
    read -rp "DNS provider: " choice || true
    case "$choice" in
        1) provider=cloudflare ;;
        2) provider=arvancloud ;;
        *) die "choose 1 or 2" ;;
    esac
    read -rp "Domains, comma separated (*.example.com,example.com): " names || true
    [ -n "$names" ] || die "no domain given"
    read -rsp "The provider's API token: " token || true
    echo
    [ -n "$token" ] || die "no token given"
    MANIFOLD_DNS_TOKEN="$token" issue_cert -domain "$names" -dns "$provider"
}

cert_from_files() {
    local cert="" key=""
    read -rp "Certificate (full chain) file: " cert || true
    read -rp "Private key file: " key || true
    [ -r "$cert" ] || die "cannot read ${cert}"
    [ -r "$key" ] || die "cannot read ${key}"
    need_root
    "$BIN" cert -webCert "$cert" -webCertKey "$key"
    restart_if_running
}

cert_remove() {
    confirm "Stop serving a certificate, so the panel answers over plain HTTP?" || return 0
    need_root
    "$BIN" cert -reset
    restart_if_running
    echo "Panel address: $(access_url)"
}

cert_show() {
    local cert
    cert=$("$BIN" setting -getCert | sed -n 's/^cert: //p')
    if [ -z "$cert" ]; then
        warn "The panel serves no certificate: it answers over plain HTTP."
        return 0
    fi
    echo "cert: ${cert}"
    if command -v openssl >/dev/null 2>&1; then
        openssl x509 -in "$cert" -noout -subject -issuer -enddate -ext subjectAltName 2>/dev/null ||
            warn "The file cannot be read as a certificate."
    fi
}

cert_menu() {
    local choice=""
    echo "  1. Let's Encrypt certificate for a domain"
    echo "  2. Let's Encrypt certificate for this server's IP address"
    echo "  3. Let's Encrypt certificate through Cloudflare or ArvanCloud DNS (wildcards too)"
    echo "  4. Use certificate files I already have"
    echo "  5. Show the panel's certificate"
    echo "  6. Remove the panel's certificate (plain HTTP)"
    echo "  0. Back"
    read -rp "Choose: " choice || true
    case "$choice" in
        1) cert_for_domain ;;
        2) cert_for_ip ;;
        3) cert_over_dns ;;
        4) cert_from_files ;;
        5) cert_show ;;
        6) cert_remove ;;
    esac
}

# --- IP limit ---

# Writes the Fail2ban jail that bans a client address the panel reported as over its IP limit.
# $1 = log folder, $2 = comma-separated ports that must never be blocked, $3 = ban minutes.
write_iplimit_jail() {
    local log_folder="$1" exempt_ports="$2" bantime="${3:-30}"
    mkdir -p "$log_folder" /etc/fail2ban/jail.d /etc/fail2ban/filter.d /etc/fail2ban/action.d
    touch "${log_folder}/ip-limit.log" "${log_folder}/ip-limit-banned.log"

    cat >"/etc/fail2ban/jail.d/${JAIL}.conf" <<JAILCONF
[${JAIL}]
enabled=true
backend=auto
filter=${JAIL}
action=${JAIL}
logpath=${log_folder}/ip-limit.log
maxretry=1
findtime=32
bantime=${bantime}m
JAILCONF

    cat >"/etc/fail2ban/filter.d/${JAIL}.conf" <<'FILTERCONF'
[Definition]
datepattern = ^%%Y/%%m/%%d %%H:%%M:%%S
failregex   = \[LIMIT_IP\]\s*Email\s*=\s*<F-USER>.+</F-USER>\s*\|\|\s*Disconnecting OLD IP\s*=\s*<ADDR>\s*\|\|\s*Timestamp\s*=\s*\d+
ignoreregex =
FILTERCONF

    cat >"/etc/fail2ban/action.d/${JAIL}.conf" <<ACTIONCONF
[INCLUDES]
before = iptables-allports.conf

[Definition]
actionstart = <iptables> -N f2b-<name>
              <iptables> -A f2b-<name> -j <returntype>
              <iptables> -I <chain> -j f2b-<name>

actionstop = <iptables> -D <chain> -j f2b-<name>
             <actionflush>
             <iptables> -X f2b-<name>

actioncheck = <iptables> -n -L <chain> | grep -q 'f2b-<name>[ \t]'

actionban = <iptables> -I f2b-<name> 1 -s <ip> -p tcp -m multiport ! --dports <exemptports> -j <blocktype>
            <iptables> -I f2b-<name> 1 -s <ip> -p udp -m multiport ! --dports <exemptports> -j <blocktype>
            echo "\$(date +"%%Y/%%m/%%d %%H:%%M:%%S")   BAN   [Email] = <F-USER> [IP] = <ip> banned for <bantime> seconds." >> ${log_folder}/ip-limit-banned.log

actionunban = <iptables> -D f2b-<name> -s <ip> -p tcp -m multiport ! --dports <exemptports> -j <blocktype>
              <iptables> -D f2b-<name> -s <ip> -p udp -m multiport ! --dports <exemptports> -j <blocktype>
              echo "\$(date +"%%Y/%%m/%%d %%H:%%M:%%S")   UNBAN   [Email] = <F-USER> [IP] = <ip> unbanned." >> ${log_folder}/ip-limit-banned.log

[Init]
name = default
chain = INPUT
exemptports = ${exempt_ports}
ACTIONCONF
}

# jail_bantime FILE: the ban minutes a written jail holds, 30 when there is none.
jail_bantime() {
    local minutes
    minutes=$(sed -n 's/^bantime=\([0-9]*\)m$/\1/p' "$1" 2>/dev/null || true)
    echo "${minutes:-30}"
}

ssh_ports() {
    local ports
    ports=$(grep -oE '^[[:space:]]*Port[[:space:]]+[0-9]+' /etc/ssh/sshd_config 2>/dev/null | grep -oE '[0-9]+' | paste -sd, - || true)
    echo "${ports:-22}"
}

# Ports a ban must never cover, so an over-limit client cannot lock the operator out.
exempt_ports() {
    local panel_port
    panel_port=$(setting_value port || true)
    if [ -n "$panel_port" ]; then
        echo "$(ssh_ports),${panel_port}"
    else
        ssh_ports
    fi
}

show_banlog() {
    local banned="${LOG_DIR}/ip-limit-banned.log"
    if [ -s "$banned" ]; then
        tail -n 20 "$banned"
    else
        echo "No bans yet."
    fi
}

iplimit() {
    case "${1:-}" in
        install)
            need_root
            if ! command -v fail2ban-client >/dev/null; then
                if command -v apt-get >/dev/null; then
                    apt-get update -qq && apt-get install -y -qq fail2ban
                elif command -v dnf >/dev/null; then
                    dnf install -y -q fail2ban
                else
                    die "install Fail2ban with your package manager, then run this again"
                fi
            fi
            write_iplimit_jail "$LOG_DIR" "$(exempt_ports)" "$(jail_bantime "/etc/fail2ban/jail.d/${JAIL}.conf")"
            systemctl enable --quiet fail2ban
            systemctl restart fail2ban
            echo "The IP limit jail is installed."
            ;;
        write-config)
            write_iplimit_jail "$LOG_DIR" "$(exempt_ports)" "$(jail_bantime "/etc/fail2ban/jail.d/${JAIL}.conf")"
            ;;
        bantime)
            need_root
            valid_port "${2:-}" || die "usage: manifold iplimit bantime MINUTES"
            write_iplimit_jail "$LOG_DIR" "$(exempt_ports)" "$2"
            systemctl restart fail2ban
            echo "Bans now last ${2} minutes."
            ;;
        unban-all)
            need_root
            fail2ban-client reload --restart --unban "$JAIL"
            : >"${LOG_DIR}/ip-limit-banned.log"
            echo "Every address is unbanned."
            ;;
        ban | unban)
            need_root
            valid_ipv4 "${2:-}" || valid_ipv6 "${2:-}" || die "usage: manifold iplimit ${1} IP"
            fail2ban-client set "$JAIL" "${1}ip" "$2"
            ;;
        remove)
            need_root
            rm -f "/etc/fail2ban/jail.d/${JAIL}.conf" "/etc/fail2ban/filter.d/${JAIL}.conf" "/etc/fail2ban/action.d/${JAIL}.conf"
            if systemctl is-active --quiet fail2ban 2>/dev/null; then
                systemctl restart fail2ban
            fi
            echo "The IP limit jail is removed."
            ;;
        status)
            fail2ban-client status "$JAIL"
            ;;
        log) show_banlog ;;
        *) die "usage: manifold iplimit install|remove|status|log|bantime MINUTES|unban-all|ban IP|unban IP" ;;
    esac
}

iplimit_menu() {
    local choice="" value=""
    echo "  1. Install Fail2ban and the IP limit jail"
    echo "  2. Change how long a ban lasts"
    echo "  3. Unban everyone"
    echo "  4. Ban log"
    echo "  5. Ban an IP address"
    echo "  6. Unban an IP address"
    echo "  7. Jail status"
    echo "  8. Remove the jail"
    echo "  0. Back"
    read -rp "Choose: " choice || true
    case "$choice" in
        1) iplimit install ;;
        2)
            read -rp "Ban length in minutes [30]: " value || true
            iplimit bantime "${value:-30}"
            ;;
        3) confirm "Unban every address?" && iplimit unban-all ;;
        4) show_banlog ;;
        5) read -rp "IP address to ban: " value || true; iplimit ban "$value" ;;
        6) read -rp "IP address to unban: " value || true; iplimit unban "$value" ;;
        7) iplimit status ;;
        8) confirm "Remove the IP limit jail?" && iplimit remove ;;
    esac
}

# --- firewall (ufw) ---

# port_list SPEC: SPEC is ports and ranges, as 80,443,2000-2100; each prints on its own line.
port_list() {
    local item first last
    [[ "$1" =~ ^[0-9]+(-[0-9]+)?(,[0-9]+(-[0-9]+)?)*$ ]] || return 1
    for item in ${1//,/ }; do
        first=${item%-*} last=${item#*-}
        valid_port "$first" && valid_port "$last" && [ "$first" -le "$last" ] || return 1
        echo "$item"
    done
}

# ufw_ports allow|delete SPEC: open or close the ports for TCP and UDP.
ufw_ports() {
    local verb="$1" ports item
    ports=$(port_list "$2") || die "not ports: ${2} (as 80,443,2000-2100)"
    for item in $ports; do
        if [ "$verb" = allow ]; then
            ufw allow "${item/-/:}/tcp"
            ufw allow "${item/-/:}/udp"
        else
            ufw delete allow "${item/-/:}/tcp" || true
            ufw delete allow "${item/-/:}/udp" || true
        fi
    done
}

firewall_install() {
    need_root
    local port ssh
    ssh=$(ssh_ports)
    if ! command -v ufw >/dev/null 2>&1; then
        command -v apt-get >/dev/null || die "ufw is for Debian and Ubuntu; use your distribution's own firewall"
        apt-get update -qq && apt-get install -y -qq ufw
    fi
    warn "Turning the firewall on closes every port not opened, your inbounds' too: open them after this."
    confirm "Open SSH, HTTP, HTTPS and the panel's ports, then turn the firewall on?" y || return 0
    for port in ${ssh//,/ } 80 443 $(setting_value port) $(setting_value subPort); do
        ufw allow "${port}/tcp"
    done
    ufw --force enable
}

firewall_menu() {
    local choice="" ports=""
    echo "  1. Install ufw and turn it on"
    echo "  2. List the rules"
    echo "  3. Open ports"
    echo "  4. Close ports"
    echo "  5. Turn the firewall on"
    echo "  6. Turn the firewall off"
    echo "  7. Firewall status"
    echo "  0. Back"
    read -rp "Choose: " choice || true
    case "$choice" in
        1) firewall_install ;;
        2) ufw status numbered ;;
        3) read -rp "Ports to open (80,443,2000-2100): " ports || true; ufw_ports allow "$ports" ;;
        4) read -rp "Ports to close (80,443,2000-2100): " ports || true; ufw_ports delete "$ports" ;;
        5) ufw --force enable ;;
        6) ufw disable ;;
        7) ufw status verbose ;;
    esac
}

# --- BBR ---

bbr() {
    need_root
    local conf="${SYSCTL_DIR}/99-bbr-manifold.conf" previous
    case "${1:-}" in
        enable)
            if [ "$(sysctl -n net.ipv4.tcp_congestion_control)" = bbr ]; then
                info "BBR is already on."
                return 0
            fi
            {
                echo "#$(sysctl -n net.core.default_qdisc):$(sysctl -n net.ipv4.tcp_congestion_control)"
                echo "net.core.default_qdisc = fq"
                echo "net.ipv4.tcp_congestion_control = bbr"
            } >"$conf"
            sysctl -p "$conf" >/dev/null
            ;;
        disable)
            [ -f "$conf" ] || die "BBR was not turned on here"
            previous=$(head -n 1 "$conf" | tr -d '#')
            sysctl -w net.core.default_qdisc="${previous%%:*}" >/dev/null
            sysctl -w net.ipv4.tcp_congestion_control="${previous#*:}" >/dev/null
            rm -f "$conf"
            ;;
        *) die "usage: manifold bbr enable|disable" ;;
    esac
    echo "TCP congestion control: $(sysctl -n net.ipv4.tcp_congestion_control)"
}

# --- geo files ---

# update_geofiles DATASET: download Xray's geo files of one dataset into GEO_DIR.
update_geofiles() {
    local source suffix name dest tmp code failed=0
    case "$1" in
        main) source=Loyalsoldier/v2ray-rules-dat suffix="" ;;
        IR) source=chocolate4u/Iran-v2ray-rules suffix=_IR ;;
        RU) source=runetfreedom/russia-v2ray-rules-dat suffix=_RU ;;
        *) die "geo datasets are main, IR and RU" ;;
    esac
    for name in geoip geosite; do
        dest="${GEO_DIR}/${name}${suffix}.dat"
        tmp="${dest}.tmp.$$"
        if ! code=$(curl -sSfLR -o "$tmp" -z "$dest" -w '%{http_code}' \
            "https://github.com/${source}/releases/latest/download/${name}.dat"); then
            warn "${name}${suffix}.dat: download failed"
            failed=1
        elif [ "$code" = 304 ]; then
            echo "${name}${suffix}.dat: already up to date"
        elif [ -s "$tmp" ]; then
            mv -f "$tmp" "$dest"
            info "${name}${suffix}.dat: updated"
            GEO_UPDATED=1
        else
            warn "${name}${suffix}.dat: the download was empty"
            failed=1
        fi
        rm -f "$tmp"
    done
    return "$failed"
}

update_geo() {
    need_root
    local dataset failed=0
    GEO_UPDATED=0
    case "${1:-all}" in
        all) for dataset in main IR RU; do update_geofiles "$dataset" || failed=1; done ;;
        *) update_geofiles "$1" || failed=1 ;;
    esac
    if [ "$GEO_UPDATED" -eq 1 ] && systemctl is-active --quiet manifold 2>/dev/null; then
        systemctl reload manifold
        info "Xray restarts to read the new files."
    fi
    return "$failed"
}

geo_menu() {
    local choice=""
    echo "  1. Loyalsoldier (geoip.dat, geosite.dat)"
    echo "  2. chocolate4u (geoip_IR.dat, geosite_IR.dat)"
    echo "  3. runetfreedom (geoip_RU.dat, geosite_RU.dat)"
    echo "  4. All"
    echo "  0. Back"
    read -rp "Choose: " choice || true
    case "$choice" in
        1) update_geo main ;;
        2) update_geo IR ;;
        3) update_geo RU ;;
        4) update_geo all ;;
    esac
}

# --- backups ---

backup_now() {
    need_root
    local pass="" dir=/root
    read -rsp "Passphrase to seal it with [none]: " pass || true
    echo
    MANIFOLD_BACKUP_PASSPHRASE="$pass" "$BIN" backup create -out "$dir"
}

backup_restore() {
    need_root
    local file="" pass="" keep=true
    read -rp "Backup file: " file || true
    [ -r "$file" ] || die "cannot read ${file}"
    if [[ "$file" == *.enc ]]; then
        read -rsp "Its passphrase: " pass || true
        echo
    fi
    confirm "Keep this server's panel port, URL path and login?" y || keep=false
    confirm "Stop the panel and restore ${file}?" || return 0
    systemctl stop manifold
    if MANIFOLD_BACKUP_PASSPHRASE="$pass" "$BIN" backup restore -in "$file" -keep-access="$keep"; then
        systemctl start manifold
        info "Restored. Panel address: $(access_url)"
    else
        systemctl start manifold
        die "the restore failed; the panel was started again as it was"
    fi
}

backup_menu() {
    local choice=""
    echo "  1. Make a full backup now (into /root)"
    echo "  2. Restore a full backup"
    echo "  0. Back"
    read -rp "Choose: " choice || true
    case "$choice" in
        1) backup_now ;;
        2) backup_restore ;;
    esac
}

# --- logs and diagnosis ---

follow_log() {
    trap 'true' INT
    journalctl -u manifold -f -n 50 || true
    trap - INT
}

log_menu() {
    local choice=""
    echo "  1. Follow the log (Ctrl+C to stop)"
    echo "  2. Last 100 lines"
    echo "  3. Errors only"
    echo "  4. IP limit bans"
    echo "  0. Back"
    read -rp "Choose: " choice || true
    case "$choice" in
        1) follow_log ;;
        2) journalctl -u manifold --no-pager -n 100 ;;
        3) journalctl -u manifold --no-pager -p err -n 50 ;;
        4) show_banlog ;;
    esac
}

# doctor: check what most often keeps the panel from being reached, with the fix for each.
doctor() {
    local port cert listen pid problems=0 free
    if ! systemctl is-active --quiet manifold 2>/dev/null; then
        echo "${red}✗${plain} The service is not running. Its last lines:"
        journalctl -u manifold --no-pager -n 15 2>/dev/null || true
        echo "  Fix the error above, then: manifold start"
        problems=$((problems + 1))
    else
        echo "${green}✓${plain} The service is running"
    fi
    port=$(setting_value port)
    pid=$(systemctl show manifold --property MainPID --value 2>/dev/null || echo 0)
    if [ -z "$port" ] || ! command -v ss >/dev/null 2>&1; then
        :
    elif [ "${pid:-0}" != 0 ] && ss -H -ltnp "sport = :${port}" 2>/dev/null | grep -q "pid=${pid},"; then
        echo "${green}✓${plain} The panel listens on port ${port}"
    else
        echo "${red}✗${plain} Nothing of the panel listens on port ${port}"
        ss -H -ltnp "sport = :${port}" 2>/dev/null | sed 's/^/  held by: /' || true
        echo "  Pick a free port: menu 7, or manifold port N"
        problems=$((problems + 1))
    fi
    listen=$("$BIN" setting -getListen 2>/dev/null | sed -n 's/^listenIP: //p')
    case "$listen" in
        "" | 0.0.0.0 | "::") ;;
        *)
            echo "${yellow}!${plain} The panel listens on ${listen} only; elsewhere it cannot be reached"
            echo "  Listen everywhere: menu 22 → 3"
            ;;
    esac
    cert=$("$BIN" setting -getCert 2>/dev/null | sed -n 's/^cert: //p')
    if [ -n "$cert" ]; then
        if [ ! -r "$cert" ]; then
            echo "${red}✗${plain} The panel's certificate ${cert} is missing"
            echo "  Get one again (menu 18) or serve plain HTTP (menu 22 → 2)"
            problems=$((problems + 1))
        elif command -v openssl >/dev/null 2>&1 && ! openssl x509 -in "$cert" -noout -checkend 0 >/dev/null 2>&1; then
            echo "${red}✗${plain} The panel's certificate has expired"
            echo "  Get one again (menu 18) or serve plain HTTP (menu 22 → 2)"
            problems=$((problems + 1))
        else
            echo "${green}✓${plain} The panel's certificate is valid"
        fi
    fi
    if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active' &&
        [ -n "$port" ] && ! ufw status 2>/dev/null | grep -qE "^${port}(/tcp)?[[:space:]]"; then
        echo "${red}✗${plain} The firewall is on and port ${port} is not open in it"
        echo "  Open it: menu 20 → 3"
        problems=$((problems + 1))
    fi
    free=$(df -Pk "$DATA_DIR" 2>/dev/null | awk 'NR == 2 { print $4 }')
    if [ -n "$free" ] && [ "$free" -lt 51200 ]; then
        echo "${red}✗${plain} Less than 50 MB free on the disk of ${DATA_DIR}: the database cannot grow"
        problems=$((problems + 1))
    fi
    if [ "$problems" -eq 0 ]; then
        echo "Panel address: $(access_url)"
    fi
    return 0
}

recovery_menu() {
    local choice=""
    echo "  1. Turn off two-factor authentication"
    echo "  2. Remove the panel's certificate and serve plain HTTP"
    echo "  3. Listen on every address again"
    echo "  4. Make a new API token"
    echo "  5. Run the database migrations again"
    echo "  6. Look for what keeps the panel from being reached"
    echo "  0. Back"
    read -rp "Choose: " choice || true
    case "$choice" in
        1) reset_two_factor ;;
        2) cert_remove ;;
        3) listen_everywhere ;;
        4) new_api_token ;;
        5) need_root; systemctl stop manifold; "$BIN" migrate; systemctl start manifold ;;
        6) doctor ;;
    esac
}

# --- install, update, uninstall ---

# run_installer ARGS...: the installer that came with this install, or the latest one.
run_installer() {
    need_root
    local installer="${INSTALL_DIR}/install.sh" tmp
    if [ ! -f "$installer" ]; then
        tmp=$(mktemp)
        curl -fsSL -o "$tmp" "https://raw.githubusercontent.com/${REPO}/main/install.sh" ||
            die "cannot download the installer"
        installer="$tmp"
    fi
    bash "$installer" "$@"
}

update_version() {
    local tag=""
    read -rp "Version to install (as v0.12.0): " tag || true
    [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "not a version: ${tag}"
    run_installer --version "$tag"
}

# remove_engine_files WIREGUARD_DIR IPSEC_CONFIG: delete what the panel wrote
# into folders it shares with other programs. All of it is rendered from the
# database, so a panel that is installed again writes it again.
remove_engine_files() {
    local config
    for config in "$1"/*.conf; do
        [ -f "$config" ] || continue
        if head -n 1 "$config" | grep -q '^# Managed by Manifold: inbound [0-9]*\.'; then
            rm -f "$config"
        fi
    done
    if [ -f "$2" ]; then
        rm -f "$2"
        swanctl --load-all --noprompt >/dev/null 2>&1 || true
    fi
}

# remove_tunnels takes down the tunnel links the panel made, which it leaves up
# across restarts, with their forwarding tables and reply routing. $1 is /sys/class/net.
remove_tunnels() {
    local link alias table pref
    for link in "$1"/mft*; do
        [ -e "$link/ifalias" ] || continue
        alias=$(cat "$link/ifalias" 2>/dev/null || true)
        case "$alias" in manifold-tunnel:*) ip link del dev "$(basename "$link")" 2>/dev/null || true ;; esac
    done
    if command -v nft >/dev/null 2>&1; then
        while read -r _ family table; do
            case "$table" in manifold_tf[0-9]* | manifold_tr[0-9]*) nft delete table "$family" "$table" 2>/dev/null || true ;; esac
        done < <(nft list tables 2>/dev/null)
    fi
    while read -r pref rest; do
        case "$rest" in *"fwmark 0x4d46"*) ip rule del pref "${pref%:}" 2>/dev/null || true ;; esac
    done < <(ip rule show 2>/dev/null)
}

uninstall() {
    need_root
    systemctl disable --now manifold 2>/dev/null || true
    rm -f /etc/systemd/system/manifold.service /usr/bin/manifold
    systemctl daemon-reload
    rm -rf "$INSTALL_DIR"
    remove_engine_files /etc/wireguard /etc/swanctl/conf.d/manifold-l2tp.conf
    remove_tunnels /sys/class/net
    if [ "${1:-}" = "--purge" ]; then
        rm -rf "$DATA_DIR" /var/log/manifold "$ENV_FILE"
        echo "Manifold and its data are removed."
    else
        echo "Manifold is removed. Its data is kept in ${DATA_DIR}."
    fi
}

uninstall_menu() {
    confirm "Remove Manifold from this server?" || return 0
    if confirm "Also delete its database, settings and logs?"; then
        uninstall --purge
    else
        uninstall
    fi
    return "$MENU_DONE"
}

# --- the menu ---

print_menu() {
    cat <<MENU

╔───────────────────────────────────────────────────╗
│  ${green}Manifold Panel Management${plain}                        │
│  ${green}0.${plain} Exit                                          │
│───────────────────────────────────────────────────│
│  ${green}1.${plain} Update                                        │
│  ${green}2.${plain} Install a specific version                    │
│  ${green}3.${plain} Uninstall                                     │
│───────────────────────────────────────────────────│
│  ${green}4.${plain} Reset Username & Password                     │
│  ${green}5.${plain} Reset Web Base Path                           │
│  ${green}6.${plain} Reset Settings                                │
│  ${green}7.${plain} Change Port                                   │
│  ${green}8.${plain} View Current Settings                         │
│───────────────────────────────────────────────────│
│  ${green}9.${plain} Start                                         │
│  ${green}10.${plain} Stop                                         │
│  ${green}11.${plain} Restart                                      │
│  ${green}12.${plain} Restart Xray                                 │
│  ${green}13.${plain} Check Status                                 │
│  ${green}14.${plain} Logs                                         │
│───────────────────────────────────────────────────│
│  ${green}15.${plain} Enable Autostart                             │
│  ${green}16.${plain} Disable Autostart                            │
│───────────────────────────────────────────────────│
│  ${green}17.${plain} Doctor: why can't I reach the panel?         │
│  ${green}18.${plain} SSL Certificate Management                   │
│  ${green}19.${plain} IP Limit Management                          │
│  ${green}20.${plain} Firewall Management                          │
│  ${green}21.${plain} SSH Port Forwarding                          │
│  ${green}22.${plain} Recovery (2FA, certificate, listen, token)   │
│  ${green}23.${plain} Backup & Restore                             │
│───────────────────────────────────────────────────│
│  ${green}24.${plain} BBR                                          │
│  ${green}25.${plain} Update Geo Files                             │
╚───────────────────────────────────────────────────╝
MENU
}

menu_action() {
    case "$1" in
        1) run_installer; return "$MENU_DONE" ;;
        2) update_version; return "$MENU_DONE" ;;
        3) uninstall_menu ;;
        4) reset_login ;;
        5) reset_path ;;
        6) reset_settings ;;
        7) change_port ;;
        8) show_settings ;;
        9) need_root; systemctl start manifold; show_status ;;
        10) need_root; systemctl stop manifold; show_status ;;
        11) need_root; systemctl restart manifold; show_status ;;
        12) restart_xray ;;
        13) systemctl status manifold --no-pager || true ;;
        14) log_menu ;;
        15) need_root; systemctl enable manifold ;;
        16) need_root; systemctl disable manifold ;;
        17) doctor ;;
        18) cert_menu ;;
        19) iplimit_menu ;;
        20) firewall_menu ;;
        21) ssh_forwarding ;;
        22) recovery_menu ;;
        23) backup_menu ;;
        24)
            local choice=""
            read -rp "  1. Enable BBR   2. Disable BBR   0. Back: " choice || true
            case "$choice" in 1) bbr enable ;; 2) bbr disable ;; esac
            ;;
        25) geo_menu ;;
        *) warn "Choose a number from 0 to 25." ;;
    esac
}

# run_action ARGS...: one menu action, in a subshell so a failure returns to the menu,
# with its exit status in ACTION_STATUS. Never call it under || or if: that turns off -e.
run_action() {
    set +e
    (
        set -e
        "$@"
    )
    ACTION_STATUS=$?
    set -e
}

show_menu() {
    need_root
    local num=""
    while true; do
        print_menu
        show_status
        echo
        read -rp "Please enter your selection [0-25]: " num || exit 0
        [ "$num" = 0 ] && exit 0
        echo
        run_action menu_action "$num"
        [ "$ACTION_STATUS" -eq "$MENU_DONE" ] && exit 0
        [ "$ACTION_STATUS" -eq 0 ] || warn "That did not finish (exit ${ACTION_STATUS})."
        read -rp "Press Enter to return to the menu..." _ || exit 0
    done
}

main() {
    if [ $# -eq 0 ]; then
        if [ -t 0 ]; then
            show_menu
        else
            usage
        fi
        return 0
    fi
    local cmd="$1"
    shift
    case "$cmd" in
        start | stop | restart | enable | disable)
            need_root
            systemctl "$cmd" manifold
            ;;
        restart-xray) restart_xray ;;
        status) systemctl status manifold --no-pager ;;
        log)
            if [ "${1:-}" = "-f" ]; then
                journalctl -u manifold -f
            else
                journalctl -u manifold --no-pager -n "${1:-100}"
            fi
            ;;
        banlog) show_banlog ;;
        doctor) doctor ;;
        settings) show_settings ;;
        login) set_login "$@" ;;
        port) apply_setting -port "${1:?usage: manifold port N}" ;;
        path) apply_setting -webBasePath "${1:?usage: manifold path /p/}" ;;
        cert)
            need_root
            "$BIN" cert "$@"
            restart_if_running
            ;;
        setting | migrate-db | backup | migrate) "$BIN" "$cmd" "$@" ;;
        version) "$BIN" -v ;;
        iplimit) iplimit "$@" ;;
        bbr) bbr "$@" ;;
        geo) update_geo "$@" ;;
        update-all-geofiles) update_geo all ;;
        update) run_installer "$@" ;;
        uninstall) uninstall "$@" ;;
        menu) show_menu ;;
        help | -h | --help) usage ;;
        *) die "unknown command: ${cmd} (see: manifold help)" ;;
    esac
}

if [ -z "${BASH_SOURCE[0]:-}" ] || [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi

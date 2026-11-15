#!/usr/bin/env bash
# Manages a Manifold install. Installed as /usr/bin/manifold.
set -euo pipefail

INSTALL_DIR=/usr/local/manifold
BIN="${INSTALL_DIR}/manifold"
DATA_DIR=/etc/manifold
LOG_DIR="${MANIFOLD_LOG_FOLDER:-/var/log/manifold}"
JAIL=manifold-ipl

usage() {
    cat <<'USAGE'
Usage: manifold <command>

Service
  start | stop | restart     Control the panel service
  status                     Show the service state
  enable | disable           Start the panel at boot, or not
  log [N]                    Show the last N log lines (default 100)
  log -f                     Follow the log

Panel
  settings                   Show the panel port, URL path and login state
  login [NAME PASSWORD]      Set the panel login; random when omitted
  port N                     Set the panel port
  path /p/                   Set the panel URL path
  setting ...                Pass options to the panel's own `setting` command
  migrate-db ...             Pass options to the panel's `migrate-db` command
  version                    Show the installed version

IP limit
  iplimit install            Install Fail2ban and the jail that enforces client IP limits
  iplimit remove             Remove the jail
  iplimit status             Show the jail state

Maintenance
  update [installer options] Install the latest release (needs MANIFOLD_TOKEN
                             for a private repository, or --package FILE)
  uninstall [--purge]        Remove Manifold; --purge also deletes its data
USAGE
}

die() {
    echo "error: $*" >&2
    exit 1
}

need_root() {
    [ "$(id -u)" -eq 0 ] || die "run this command as root"
}

random_string() {
    LC_ALL=C tr -dc 'a-zA-Z0-9' </dev/urandom | head -c "$1" || true
}

apply_setting() {
    need_root
    local out
    out=$("$BIN" setting "$@" 2>&1) || die "$out"
    case "$out" in
        *Failed*) die "$out" ;;
    esac
    if systemctl is-active --quiet manifold 2>/dev/null; then
        systemctl restart manifold
    fi
}

set_login() {
    local name="${1:-}" pass="${2:-}"
    [ -n "$name" ] || name=$(random_string 10)
    [ -n "$pass" ] || pass=$(random_string 16)
    apply_setting -username "$name" -password "$pass"
    echo "Username: ${name}"
    echo "Password: ${pass}"
}

# Writes the Fail2ban jail that bans a client address the panel reported as over its IP limit.
# $1 = log folder, $2 = comma-separated ports that must never be blocked.
write_iplimit_jail() {
    local log_folder="$1" exempt_ports="$2"
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
bantime=30m
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

# Ports a ban must never cover, so an over-limit client cannot lock the operator out.
exempt_ports() {
    local ssh_ports panel_port
    ssh_ports=$(grep -oE '^[[:space:]]*Port[[:space:]]+[0-9]+' /etc/ssh/sshd_config 2>/dev/null | grep -oE '[0-9]+' | paste -sd, - || true)
    [ -n "$ssh_ports" ] || ssh_ports=22
    panel_port=$("$BIN" setting -show 2>/dev/null | grep -Eo 'port: .+' | awk '{print $2}' || true)
    if [ -n "$panel_port" ]; then
        echo "${ssh_ports},${panel_port}"
    else
        echo "$ssh_ports"
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
            write_iplimit_jail "$LOG_DIR" "$(exempt_ports)"
            systemctl enable --quiet fail2ban
            systemctl restart fail2ban
            echo "The IP limit jail is installed."
            ;;
        write-config)
            write_iplimit_jail "$LOG_DIR" "$(exempt_ports)"
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
        *) die "usage: manifold iplimit install|remove|status" ;;
    esac
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

uninstall() {
    need_root
    systemctl disable --now manifold 2>/dev/null || true
    rm -f /etc/systemd/system/manifold.service /usr/bin/manifold
    systemctl daemon-reload
    rm -rf "$INSTALL_DIR"
    remove_engine_files /etc/wireguard /etc/swanctl/conf.d/manifold-l2tp.conf
    if [ "${1:-}" = "--purge" ]; then
        rm -rf "$DATA_DIR" /var/log/manifold /etc/default/manifold
        echo "Manifold and its data are removed."
    else
        echo "Manifold is removed. Its data is kept in ${DATA_DIR}."
    fi
}

main() {
    local cmd="${1:-help}"
    [ $# -gt 0 ] && shift
    case "$cmd" in
        start | stop | restart | enable | disable)
            need_root
            systemctl "$cmd" manifold
            ;;
        status) systemctl status manifold --no-pager ;;
        log)
            if [ "${1:-}" = "-f" ]; then
                journalctl -u manifold -f
            else
                journalctl -u manifold --no-pager -n "${1:-100}"
            fi
            ;;
        settings) "$BIN" setting -show ;;
        login) set_login "$@" ;;
        port) apply_setting -port "${1:?usage: manifold port N}" ;;
        path) apply_setting -webBasePath "${1:?usage: manifold path /p/}" ;;
        setting | migrate-db) "$BIN" "$cmd" "$@" ;;
        version) "$BIN" -v ;;
        iplimit) iplimit "$@" ;;
        update)
            need_root
            exec bash "${INSTALL_DIR}/install.sh" "$@"
            ;;
        uninstall) uninstall "$@" ;;
        help | -h | --help) usage ;;
        *) die "unknown command: ${cmd} (see: manifold help)" ;;
    esac
}

if [ -z "${BASH_SOURCE[0]:-}" ] || [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi

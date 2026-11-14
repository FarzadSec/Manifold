#!/bin/sh

# The IP limit needs Fail2ban running inside the container; the jail itself is
# written by the same code the host install uses.
if [ "${MANIFOLD_ENABLE_FAIL2BAN:-${XUI_ENABLE_FAIL2BAN:-}}" = "true" ]; then
    MANIFOLD_LOG_FOLDER="${MANIFOLD_LOG_FOLDER:-${XUI_LOG_FOLDER:-/var/log/manifold}}"
    export MANIFOLD_LOG_FOLDER
    /usr/bin/manifold iplimit write-config
    fail2ban-client -x start
fi

# acme.sh relies on a root crontab entry that is lost when the container is
# recreated. Re-register it and run crond so renewals fire; mount /root/.acme.sh
# as a volume to keep the certificate state.
if [ -f /root/.acme.sh/acme.sh ]; then
    /root/.acme.sh/acme.sh --install-cronjob >/dev/null 2>&1
    crond
fi

exec /app/manifold

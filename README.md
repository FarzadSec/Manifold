# Manifold

One control panel for several VPN protocols. A customer has one account with one quota and one expiry date, and that account works on every protocol the server offers.

**Status:** version 0.1, private project. The base panel is complete and installable; the protocol engines that make it Manifold are being built in phases.

## Protocols

| Protocol | Status |
|---|---|
| VLESS, VMess, Trojan, Shadowsocks, Hysteria, TUIC, MTProto | Working |
| WireGuard (standard kernel WireGuard, `wg-quick`) | Phase 1 |
| OpenVPN | Phase 2 |
| L2TP/IPsec | Phase 3 |

## What works today

- Inbounds and clients, with traffic quota, expiry date and periodic traffic reset per client
- One client attached to several inbounds, and client groups
- Subscription server: share links, JSON and Clash output
- Several servers managed from one panel (nodes over mutual TLS)
- Per-client IP limit enforced with Fail2ban
- Two-factor login (TOTP), API tokens with scopes, and an OpenAPI description of the API
- SQLite by default, PostgreSQL optional
- Notifications through Telegram, Discord and email
- Interface in 14 languages, Persian and right-to-left included

## What is planned

- The three protocol engines above, sharing quota and expiry with every other protocol on the account
- Roles, administrators and resellers with their own limits
- Plans, an audit log, and webhooks for every account event
- A Telegram sales bot that stays in sync with the panel

The full plan is in [manifold/ARCHITECTURE.md](manifold/ARCHITECTURE.md).

## Quick start

On a Linux server with systemd, as root:

```bash
export MANIFOLD_TOKEN=github_pat_xxx   # read access to this repository
curl -fsSL -H "Authorization: Bearer $MANIFOLD_TOKEN" -H "Accept: application/vnd.github.raw" \
  https://api.github.com/repos/FarzadSec/Manifold/contents/install.sh | bash
```

The installer downloads the latest release, installs the `manifold` service and prints the panel address and login once. After that the server is managed with the `manifold` command:

```bash
manifold status
manifold settings
manifold help
```

Docker and offline installs are covered in the [installation guide](manifold/docs/installation.md).

## Documentation

- [Installation](manifold/docs/installation.md)
- [Using the panel](manifold/docs/usage.md)
- [Development](manifold/docs/development.md)
- [Architecture and roadmap](manifold/ARCHITECTURE.md)

## Licence

Manifold is licensed under the GNU General Public License, version 3. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

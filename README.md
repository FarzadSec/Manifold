# Manifold

One control panel for several VPN protocols. A customer has one account with one quota and one expiry date, and that account works on every protocol the server offers.

**Status:** early development, private project. The base panel runs today; the protocol engines that make it Manifold are being built in phases.

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

Docker is the supported way to run Manifold for now.

```bash
git clone git@github.com:FarzadSec/Manifold.git
cd Manifold
docker compose up -d --build
docker compose logs manifold | grep "Username"
```

The last command prints the login that was generated on first start. Open `http://SERVER-IP:2053/`, sign in, and change the login under Panel Settings.

## Documentation

- [Installation](manifold/docs/installation.md)
- [Using the panel](manifold/docs/usage.md)
- [Development](manifold/docs/development.md)
- [Architecture and roadmap](manifold/ARCHITECTURE.md)

## Before the rename is finished

Two things still carry over from the code base Manifold started from, and both are being replaced in the foundation phase:

- The program, its service and its data directory are still named `x-ui`.
- `install.sh`, `update.sh`, the update entry in the `x-ui` menu and the panel's own update action download another project's release. **Do not use them**: they would replace Manifold with that release.

## Licence

Manifold is licensed under the GNU General Public License, version 3. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

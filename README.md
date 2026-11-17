# Manifold

One control panel for several VPN protocols. A customer has one account with one quota and one expiry date, and that account works on every protocol the server offers.

**Status:** version 0.5. The base panel, all four protocol families, tunnels to relays and moving a panel between servers are complete and installable. Resellers, plans and the sales bot are next.

## Protocols

| Protocol | Status |
|---|---|
| VLESS, VMess, Trojan, Shadowsocks, Hysteria, TUIC, MTProto | Working |
| OpenVPN (UDP or TCP, login by username and password) | Working since 0.2 |
| WireGuard (standard kernel WireGuard, `wg-quick`) | Working since 0.3 |
| L2TP/IPsec (the VPN type built into Windows, macOS and iOS) | Working since 0.4 |

## What works today

- Inbounds and clients, with traffic quota, expiry date and periodic traffic reset per client
- OpenVPN inbounds: the panel runs the server, hands each client a ready `.ovpn` profile, counts its traffic and enforces its quota, expiry and device limit
- WireGuard inbounds: each is an interface of the server in `/etc/wireguard`, with a `.conf` and QR code per client, usage read from the interface, and clients taken off it when they are disabled, expired or out of quota
- Taking over a WireGuard server that already runs, WGDashboard's included: users keep their configs, names and usage
- L2TP/IPsec inbounds: the panel runs the L2TP server and hands strongSwan its IPsec side; a customer types four values into the device's own VPN settings and needs no app. Usage is counted per account, and quota, expiry and the device limit cut a session
- OpenVPN profiles set by the admin: MTU, ciphers, split tunnelling, `tls-crypt`, `tls-auth` or no control channel key, extra profile lines, and certificates issued with a chosen key type or brought in
- Tunnels to a relay: kernel GRE, GRETAP, IP6GRE, IP6GRETAP, IPIP and SIT links and BackPack reverse and layer-3 tunnels, with the relay's forwarded ports, their state on the overview, pairing codes, and relays set up by this panel itself
- A tunnel edition for the relay: the same package with tunnels only
- A full backup that moves a panel to another server, with a check of what the move does to every address users' configs carry
- One client attached to several inbounds, and client groups
- Subscription server: share links, JSON and Clash output
- Several servers managed from one panel (nodes over mutual TLS)
- Per-client IP limit enforced with Fail2ban
- Two-factor login (TOTP), API tokens with scopes, and an OpenAPI description of the API
- SQLite by default, PostgreSQL optional
- Notifications through Telegram, Discord and email
- Interface in 14 languages, Persian and right-to-left included

## What is planned

- Roles, administrators and resellers with their own limits
- Plans, an audit log, and webhooks for every account event
- A Telegram sales bot that stays in sync with the panel

The full plan is in [manifold/ARCHITECTURE.md](manifold/ARCHITECTURE.md).

## Quick start

On a Linux server with systemd, as root:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/FarzadSec/Manifold/main/install.sh)
```

The installer downloads the latest release, asks for the panel port and whether to secure the panel with a Let's Encrypt certificate for a domain or for the server's IP, installs the `manifold` service and prints the address, login and an API token once. After that the server is managed with the `manifold` command, which opens a menu like `x-ui` and works while the panel's pages are down:

```bash
manifold            # the menu
manifold doctor     # why the panel cannot be reached
manifold settings
manifold help
```

A relay that forwards users to this panel through tunnels is installed as the tunnel edition:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/FarzadSec/Manifold/main/install.sh) --edition tunnel
```

Docker and offline installs are covered in the [installation guide](manifold/docs/installation.md).

## Documentation

- [Installation](manifold/docs/installation.md)
- [Using the panel](manifold/docs/usage.md)
- [Development](manifold/docs/development.md)
- [Architecture and roadmap](manifold/ARCHITECTURE.md)

## Licence

Manifold is licensed under the GNU General Public License, version 3. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

Release packages also carry, unmodified, the BackPack program that runs BackPack tunnels: [BackPack by Amin Mohammadi (AminMGMT)](https://github.com/AminMGMT/BackPack), licensed under the GNU AGPL, version 3.

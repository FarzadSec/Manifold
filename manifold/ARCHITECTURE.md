# Manifold — Architecture and Roadmap

Version 0.9 · 2026-11-15 · Status: P0, OpenVPN, WireGuard and L2TP/IPsec delivered; see 8.1 to 8.4 for what they leave open

Decision log:

- 2026-11-15 — What a subscription looks up and what a client can be handed come from one table of protocols (`linkBuilders` in `internal/sub`). They were two lists kept by hand, and OpenVPN and L2TP were only in one: from 0.2.0 to 0.4.0 the client's windows and the subscription page showed nothing for them, and the address of an account with no other protocol answered 404. The end-to-end scripts had asked for links by email, a route no page uses; they now ask by subscription ID, as the windows do.
- 2026-11-15 — A subscription format only describes what it can: the raw body leaves out an empty link instead of a blank line, the JSON format skips every protocol Xray has no outbound for (it used to emit a config with only "direct" and "block" for MTProto), and an account with nothing a proxy app can import gets an empty list, not a 404.
- 2026-11-15 — L2TP logins are decided by the panel itself, without the embedded RADIUS server of the first proposal. pppd's `winbind` plugin starts a helper for every login and talks to it over a pipe; the helper is the panel's own program, which relays to the running panel over a unix socket, and the panel verifies MS-CHAPv2 against the account's password. No dictionary files, no second listening port, and the same account table that every other decision uses.
- 2026-11-15 — A server holds one L2TP inbound, on UDP port 1701. No L2TP client can be given another port, so a second inbound could never be reached; both are refused on save with that reason.
- 2026-11-15 — IPsec for L2TP is the server's own strongSwan, given one connection through `swanctl`. The panel runs no IKE daemon. The installer installs and starts strongSwan only where no IPsec service exists; next to an older `ipsec` setup it installs nothing, because two IKE daemons cannot share ports 500 and 4500.
- 2026-11-15 — "Require IPsec" is on by default and enforced by a packet filter rule that is installed before the L2TP server listens: UDP 1701 takes only what arrived through IPsec. If the rule cannot be installed the server does not start. Off means sessions without IPsec are accepted as well, while IPsec stays on offer with the same key.
- 2026-11-15 — The L2TP server assigns both addresses of a link. The `ipcp-accept-*` options of the usual xl2tpd examples are left out: with them a Linux client or a router that proposes its own address was accepted and then disconnected.
- 2026-11-15 — The systemd unit no longer sets `ProtectClock`. From systemd 245 to 251 (Ubuntu 22.04, Debian 11) that setting closes every device node but a few pseudo ones to the service, so OpenVPN could not open `/dev/net/tun` under the unit in 0.2.0 and 0.3.0 on those systems. The capability bounding set already withholds `CAP_SYS_TIME`.
- 2026-11-15 — A release is installed on Ubuntu 22.04, 24.04 and 26.04 before it is published, and on each the three VPN end-to-end scripts drive the installed service under its unit with real clients. The two sandbox faults found so far (`NoNewPrivileges`, `ProtectClock`) each showed on one of these versions only, and only with a session.
- 2026-11-15 — `manifold uninstall` removes what the panel wrote into `/etc/wireguard` and `/etc/swanctl`. Those files are rendered from the database; left behind, a WireGuard config kept a private key and a name `wg-quick` would still bring up.
- 2026-11-15 — The installer installs each dependency on its own, so a package one distribution lacks costs only the protocol that needs it.
- 2026-11-15 — The systemd unit no longer sets `NoNewPrivileges`. Ubuntu 26.04 confines `wg-quick` with AppArmor; the profile runs `ip` and `wg` in profiles of their own, and the kernel refuses a change of profile to a process started with that flag, so no WireGuard interface came up under the unit. The capability bounding set stays and already limits what any child can hold.
- 2026-11-14 — Engine order changed once more: kernel WireGuard is built before L2TP/IPsec. It could be run end to end where the code is written, while L2TP needs kernel PPP that only the CI runners have; and it retires the two WireGuard implementations the product does not want.
- 2026-11-14 — The existing `wireguard` protocol became the kernel engine; no second protocol name. Inbounds, clients, links and subscriptions made before keep their keys and addresses. A node says it runs this engine with the `wireguard` capability.
- 2026-11-14 — The panel adds one route per peer and writes `Table = off`. `wg-quick` only adds routes when an interface comes up, so a peer set later would have none.
- 2026-11-14 — WireGuard interfaces go down when the panel stops. An interface left up would serve accounts nobody bills or cuts off.
- 2026-11-14 — A config in `/etc/wireguard` that the panel did not write is never changed. It becomes an inbound only through an explicit import, which keeps the old file as `<name>.conf.before-manifold` and puts the interface back if the inbound cannot be created.
- 2026-11-14 — An imported server keeps its hooks and its own addresses. `PostUp` and its siblings are therefore inbound settings, run by `wg-quick` as root; the panel's own NAT is added beside whatever they set up.
- 2026-11-14 — AmneziaWG is no longer offered for a new inbound. An inbound made before still opens and runs, and the API still accepts the protocol, so upstream's code and tests stay untouched.

- 2026-11-14 — Engine order changed to OpenVPN, L2TP/IPsec, then kernel WireGuard. The owner looked for the first two in the installed panel and did not find them, while the inbound form already offers a WireGuard (Xray's own, and AmneziaWG) that keeps working until the kernel engine replaces it.
- 2026-11-14 — OpenVPN accounts log in with the client's email and password, and the panel decides every login over the management interface. No client certificates: one account then has the same identity on every protocol, and disabling it needs no revocation list and no restart.
- 2026-11-14 — A profile travels as an `openvpn://` link holding the `.ovpn` text. The panel renders it in one place (Go) and the client pages and the subscription page unwrap it, so the two cannot hand out different files. The subscription body that proxy apps import leaves it out.
- 2026-11-14 — An OpenVPN server keeps running when its last account is switched off, so that account is refused in words instead of timing out against a closed port.
- 2026-11-14 — A node tells its master which native engines it has in the capability header, with the protocol name as the token. The panel version cannot carry this: every Manifold release reports the same base version.
- 2026-11-14 — The panel starts without its subscription server when that port is taken, and the installer moves the port at first setup. Installing next to another panel used to leave a service that died on every start.

- 2026-11-14 — Installing works like other panels: one `bash <(curl …/install.sh)` command that downloads the latest release from its public address. A release is cut by changing `product_version` on `main`; the workflow builds, test-installs and publishes it, then installs the published release once more on a clean machine.
- 2026-11-14 — The repository is public. The installer needs no token; `MANIFOLD_TOKEN` remains for a private copy.
- 2026-11-14 — The panel never updates itself. The code that downloaded and ran an updater is removed; updates go through `install.sh` (`manifold update`). The update routes and the node fan-out remain and answer with a refusal, so masters and nodes keep one API.
- 2026-11-14 — Two versions. `product_version` (0.1.0) is what people see and what release tags equal. The base version stays, because a master gates protocols on the version a node reports.
- 2026-11-14 — Operators use `MANIFOLD_<NAME>` variables; the code keeps reading `XUI_<NAME>` and start-up copies one onto the other. Internal identifiers a user never sees (the Go module path, `XUI_` reads, `window.X_UI_*` globals) are kept to keep upstream merges cheap.
- 2026-11-14 — Manifold installs natively under systemd from release packages built by CI; Docker remains as a second path. The installer downloads through the GitHub API, or installs from a local package file.

- 2026-11-14 — The repository carries only Manifold's own descriptions and guides. The base project's pages, documentation site, media and community files are removed from the product branch and stay readable on `upstream-main`. The same applies to names and links inside the program, which P0 replaces.
- 2026-11-14 — Project name: Manifold. Repository: `FarzadSec/Manifold`, private, a mirror of `MHSanaei/3x-ui` at commit `5297fd3c`.
- 2026-11-14 — WireGuard in this panel is standard kernel WireGuard, driven exactly the way WGDashboard drives it. AmneziaWG is not part of the product.
- 2026-11-14 — Panel first, Mirza Bot after.

## 1. Goal

One panel that serves Xray (V2Ray) protocols, standard WireGuard, OpenVPN and L2TP/IPsec from a single user model, with the management features of 3x-ui plus the documented features of NX Panel, and WireGuard handled the same way WGDashboard handles it.

Order of work: the panel first. Mirza Bot is forked and wired to the panel's API only after the panel is done.

## 2. Source projects (inspected 2026-11-14)

| Project | Stack | Licence | Role here |
|---|---|---|---|
| MHSanaei/3x-ui | Go 1.27, Gin, GORM, SQLite or PostgreSQL; React 19 + Ant Design 6 | GPL-3.0 | **Base — forked** |
| WGDashboard v4.3.5 | Python (Flask) + Vue 3 | Apache-2.0 | Reference implementation for the WireGuard engine; reproduced in Go, not forked |
| nx-panel/panel v0.1.2 | Closed source; node built on sing-box | Proprietary | Feature reference from public docs only |
| mahdiMGF2/mirzabot | PHP ≥ 8.2, MySQL | AGPL-3.0 | Forked in the bot phase |

Why 3x-ui is the base:

- It is the only one of the three panels that is both open source and already multi-protocol: Xray protocols, TUIC, MTProto, Hysteria. It also ships its own WireGuard and AmneziaWG implementations, which this project does not use (see 4.3).
- It already has multi-node management (`runtime.Runtime`, Local or Remote over mTLS), PostgreSQL support, bearer API tokens with scopes, an OpenAPI document, TOTP two-factor login, LDAP sync, a command palette and a PWA.
- It already runs non-Xray engines beside Xray, for example a supervised child process (`mtg-multi` for MTProto, `internal/mtproto/`). WireGuard, OpenVPN and L2TP follow that pattern.
- One account can already attach to many inbounds (`Client`, `ClientInbound`, `ClientGroup`), which is the unified user model this project needs.

## 3. Hard constraints

1. **NX Panel cannot be forked.** Both `nx-panel/panel` and `nx-panel/node` publish only installers, Docker files and docs; their READMEs say the source is not public. The licence forbids modifying, reverse engineering and redistributing. Parity therefore means re-implementing features from its public documentation. None of its code, UI code or assets can be reused.
2. **The three panels cannot be merged at code level** (Go, Python, closed binary). WGDashboard's behaviour is reproduced in Go.
3. **NX serves protocols that Xray does not have**: NaiveProxy, AnyTLS, ShadowTLS, Mieru, Snell, SSH, TrustTunnel, Sudoku, OpenConnect. Matching those needs sing-box as a second core. Not in the MVP; see section 9.
4. **Licences** (not legal advice): the fork stays GPL-3.0, so handing the binary to anyone else (for example resellers who self-host) carries the obligation to offer source. Mirza Bot is AGPL-3.0, which extends that obligation to users who interact with a modified bot over the network.

## 4. Target architecture

```
Manifold (Go, fork of 3x-ui)
├─ Admin API + React SPA            internal/web
├─ Subscription server              internal/sub   links, JSON, Clash, .conf, .ovpn, L2TP credentials
├─ Account model                    one account → many inbounds, shared quota and expiry
├─ Protocol engines
│   ├─ Xray-core                    child process      existing
│   ├─ mtg-multi (MTProto)          child process      existing
│   ├─ TUIC                         in-process         existing
│   ├─ WireGuard (kernel, wg-quick) system tools       NEW  internal/wgquick
│   ├─ OpenVPN                      child process      NEW  internal/openvpn
│   ├─ L2TP/IPsec                   child processes    NEW  internal/l2tp, with the server's strongSwan
│   └─ AmneziaWG, Xray WireGuard    upstream code      not offered, not used
├─ Access model                     roles, admins, resellers, plans, audit   NEW
├─ Event bus → webhook outbox       NEW (bus exists, webhooks do not)
└─ runtime.Runtime                  Local | Remote node over mTLS   existing, extended
```

### 4.1 One account, every protocol

WireGuard, OpenVPN and L2TP are added as new inbound protocol types, so they inherit what inbounds already have: clients, groups, quota, expiry, node sync, traffic reset. Their usage is fed into the same `client_traffics` pipeline by a cron job per engine, the way `tuic_job` and `mtproto_job` do today.

### 4.2 Where a new engine plugs in

Files that reference `model.MTProto` today show the integration surface a non-Xray protocol needs:

- `internal/database/db.go` (migration), `internal/database/model/model.go` (protocol enum)
- `internal/<engine>/manager.go`, `process.go`, orphan reaping
- `internal/web/service/inbound.go`, `inbound_protocol.go`, `inbound_clients.go`, `inbound_<engine>.go`, `inbound_traffic_apply.go`, `port_conflict.go`, `client_crud.go`, `client_bulk.go`, `client_inbound_apply.go`
- `internal/web/runtime/local.go` (so node deployments keep working)
- `internal/sub/service.go` (what the end user receives)
- `internal/web/job/<engine>_job.go` (traffic and limits)
- Frontend: `frontend/src/pages/inbounds/form/protocols/`, `frontend/src/schemas/protocols/`, plus the 14 locale files

Since OpenVPN, an engine does not add its own branches to those files. `internal/web/runtime/native.go` holds a `nativeEngine` interface (apply an inbound, remove an inbound) and a table of engines by protocol; the local runtime, the Xray config builder, the traffic-limit dispatch and the client apply sites ask `IsNativeProtocol` and call the engine. A new engine adds one table entry, its `Desired…Instances` query and its job. `internal/egress` gives every tunnel engine NAT and forwarding in a table of its own.

The rules in `CLAUDE.md` keep applying to fork code: every new route is registered in `frontend/src/pages/api-docs/endpoints.ts`, model changes need a migration in `db.go`, and state-changing operations go through `runtime.Runtime`.

### 4.3 WireGuard engine (built, 0.3.0) — standard kernel WireGuard, the WGDashboard way

What WGDashboard v4.3.5 does, read from its source (`WireguardConfiguration.py`, `Peer.py`): one interface per configuration in `/etc/wireguard/<name>.conf`, brought up and down with `wg-quick`; peers applied live with `wg set <name> peer <key> allowed-ips …` and removed with `… remove`; usage and handshakes read from `wg show`; a restricted peer removed from the interface and parked in a `<name>_restrict_access` table.

The engine (`internal/wgquick`) drives the same files with the same tools:

- A `wireguard` inbound is an interface. Its config is `/etc/wireguard/<name>.conf`, where the name is the inbound's own or `mfwg<id>`. The first line marks the file as the panel's; a file without it is foreign and is not touched.
- A changed client set is applied to the live interface with `wg set`, a preshared key passed on standard input. Only a changed port, key, address, MTU or hook brings the interface down and up.
- Each peer belongs to a panel account. Disabled, expired or out of quota means off the interface; renewed means back on. This is WGDashboard's restrict mechanism with the account model deciding.
- Billing reads `wg show <name> dump` every ten seconds and before any peer is removed, into the same traffic pipeline as every other protocol. A handshake in the last three minutes counts as online.
- Routes: one per peer prefix, added and removed by the panel (`Table = off`). NAT: `internal/egress`, keyed on the interface, so peers outside the server's own subnet are covered too.
- The interface's addresses are the first host of the inbound's subnet unless the inbound names them. The allocator never hands a named server address to a client.
- Hooks (`PreUp`, `PostUp`, `PreDown`, `PostDown`) are inbound settings and are written into the config.
- Under the systemd unit the engine needs `/etc/wireguard` writable, which the unit grants; the kernel loads the module on the first interface.
- On a node, the node's own panel runs the engine; operations reach it through `runtime.Runtime`.

Taking over an existing server (`foreign.go`, `dashboard.go`, service `wireguard_import.go`):

- The import reads the foreign config the way `wg-quick` does, and peer names from the comments WGDashboard, wireguard-install, wg-easy and PiVPN write.
- From WGDashboard's SQLite database it adds names, usage, private keys and restricted peers. A private key is kept only if it derives the peer's public key.
- The interface is taken down, its config moved aside, and the inbound created under the same name with the same port, keys, addresses and hooks. Peers become clients; names that the panel cannot use as emails, or that are taken, are adjusted and the original kept as the comment.

Not carried over from WGDashboard: scheduled peer jobs (an account's expiry and quota do this), share links with an expiry, endpoint history, per-interface backup and restore, and raw config editing.

Not used:

- **AmneziaWG.** Upstream's code stays in the tree. The inbound form no longer offers it for a new inbound.
- **Xray's WireGuard inbound.** The protocol name now means the kernel engine; the Xray config no longer receives these inbounds.

Requirement: root, the `wireguard-tools` package and the kernel WireGuard module on the host (`NET_ADMIN` under Docker).

### 4.4 OpenVPN engine (built, 0.2.0)

- One `openvpn` server process per inbound (`internal/openvpn`), started with the distribution's `openvpn` package (2.5 or newer). One inbound is one transport, UDP or TCP, on one port; offering both takes two inbounds.
- Keys per inbound, issued by the panel when the inbound is saved and kept in its settings, so backup, restore and node sync carry them: an ECDSA CA, a server certificate and a `tls-crypt` key. An edit that leaves the keys out keeps the stored ones.
- The panel attaches to each server's management socket. `management-client-auth` hands it every login and renegotiation; it answers from an in-memory account table. `management-hold` and `management-signal` keep the server from accepting anyone while the panel is not attached.
- A changed account set is applied to the live server: the table is replaced and the sessions it no longer allows are cut with `client-kill`. Only a changed port, transport, subnet, DNS list or key restarts the process.
- The device limit is the account's `limitIp`: the number of simultaneous sessions, counted by the panel at login. It does not depend on Fail2ban.
- Accounting comes from the management interface's per-session byte counters every five seconds, plus the totals a disconnect reports, into the same traffic pipeline as every other protocol. Kernel data-channel offload is switched off, because it keeps those counters out of the management interface.
- Clients reach the internet through kernel NAT (`internal/egress`): an nftables table per inbound and FORWARD accepts tagged with a comment, both removed with the server.
- The profile embeds the login by default (`<auth-user-pass>`, read by OpenVPN 2.6 and OpenVPN Connect). Per inbound it can be left out for older apps, which then ask for it.
- Not built: IPv6 inside the tunnel, client-certificate logins, static addresses per account, and steering tunnel traffic into Xray routing (WARP, outbound rules).

### 4.5 L2TP/IPsec engine (built, 0.4.0)

- One `xl2tpd` per server (`internal/l2tp`), started by the panel on files of the inbound's own under the bin folder, so it shares nothing with an xl2tpd service of the distribution. xl2tpd starts one `pppd` per session. Port 1701/udp is fixed and a server holds one L2TP inbound.
- Logins: pppd requires MS-CHAPv2, which every built-in client offers, and its `winbind` plugin starts `manifold l2tp-auth <socket>` for each one. That short-lived command relays the challenge and response to the running panel over a unix socket; the panel verifies them against the account's password and returns the session key. The account's email is the user name.
- Sessions: pppd runs `manifold l2tp-hook up|down` when a link comes up or goes down, which tells the panel the interface, the tunnel address and the pppd process. The panel counts sessions and logins still under way against the account's `limitIp`, reads usage from the interface's counters every ten seconds plus the totals pppd reports at the end, and cuts the sessions of an account it no longer serves by signalling their pppd.
- IPsec: one `swanctl` connection, `manifold-l2tp`, in `/etc/swanctl/conf.d/`: IKEv1 with the inbound's pre-shared key, transport mode, limited to UDP 1701, with the IKE and ESP offers of Windows, macOS, iOS and Android. strongSwan itself is the server's service.
- The guard: with "Require IPsec" a rule in a table of its own drops what reaches UDP 1701 without having arrived through IPsec (`egress.RequireIPsec`). It is installed before xl2tpd starts, and a failure to install it stops the start.
- Clients reach the internet through the same kernel NAT as OpenVPN (`internal/egress`).
- An account is handed out as an `l2tp://user:password@host?ipsec=…&psk=…` link, rendered in Go. The client pages show it as the four values a device asks for; the subscription body that proxy apps import leaves it out.
- The unit allows `AF_PPPOX`, so that xl2tpd hands sessions to the kernel, and lets the panel write `/etc/swanctl`.
- Not built: IKEv2 from the same strongSwan, a `.mobileconfig` for Apple devices, a key per account, IPv6 inside the tunnel, several devices behind one public address (strongSwan's `connmark` plugin is the known answer), L2TP in the container image, and steering tunnel traffic into Xray routing.

### 4.6 Access model

3x-ui's `User` is only a username and password. To build:

- Roles with permissions (main admin, operator, reseller, custom roles that can only remove permissions)
- Ownership: a reseller sees only the accounts it owns
- Reseller limits: account count, device limit, plan length, traffic allowance charged to a ledger as it is used
- Plans: presets accounts are created from
- Audit log
- Reseller-scoped API tokens beside the existing `monitor`, `node-sync`, `node-admin`, `admin` scopes

### 4.7 Events and webhooks

3x-ui has an in-process event bus with 8 events (`node.up`, `node.down`, `outbound.up`, `outbound.down`, `xray.crash`, `cpu.high`, `memory.high`, `login.attempt`) and Telegram, Discord and email notifiers on top. To build:

- Account events: created, updated, expired, quota exhausted, quota threshold, disabled, deleted, first connection
- Webhook subscribers with their own URL, signing secret and event list
- Outbox delivery with retries, a delivery log and replay

This is the contract Mirza Bot will sync against, so it is frozen before the bot phase starts.

## 5. Gap analysis

"3x-ui today" was checked against the source on 2026-11-14.

| Feature | 3x-ui today | Asked for by | Work |
|---|---|---|---|
| Xray protocols (VLESS, VMess, Trojan, Shadowsocks, Hysteria, TUIC, MTProto) | Present | 3x-ui | None |
| Standard kernel WireGuard (`wg-quick`) | Absent; its WireGuard inbound runs inside Xray in userspace | User, WGDashboard | Built in 0.3.0 (4.3) |
| AmneziaWG | Present | Not wanted | No longer offered for new inbounds (0.3.0) |
| OpenVPN | Absent | User, NX | Built in 0.2.0 (4.4) |
| L2TP/IPsec | Absent | User | Built in 0.4.0 (4.5) |
| Multi-node over mTLS, node health | Present | NX | Extend to new engines |
| Roles, admins, resellers, ownership, ledger | Absent | NX | Build (4.6) |
| Plans | Absent | NX | Build |
| Audit log | Absent | NX | Build |
| Webhooks with outbox and replay | Absent (in-process bus only) | NX, Mirza sync | Build (4.7) |
| API tokens | Present, 4 scopes | NX | Add reseller scope |
| Two-factor login (TOTP) | Present | NX | Check recovery codes and session list |
| Single sign-on | LDAP only | NX | Add OIDC |
| Command palette, PWA | Present | NX | None |
| Backup and restore | Present (DB backup, Telegram delivery) | NX | Compare: encryption, schedule, retention, destinations |
| First-run setup wizard | Not found | NX | Build |
| Panel self-update | Present | NX | Repoint at the fork's releases |
| Hosts and domain fronting | Present (`Host` model, hosts page) | NX | Compare in detail |
| Managed node-to-node tunnels with status view | Partial (`tunnel` protocol, tunnel monitor) | NX | Build the managed layer |
| WARP, NordVPN, PIA outbound integration | Present | NX | None |
| Subscription page themes, usage CSV | Partial or unverified | NX | Later phase |
| Shop: customer portal, payments, agents, promotions | Absent | NX Shop addon | Mirza Bot covers this |
| Addon platform, licence system | Absent | NX | Out of scope |
| NaiveProxy, AnyTLS, ShadowTLS, Mieru, Snell, SSH, OpenConnect and similar | Absent (needs sing-box) | NX | Out of MVP |

## 6. Roadmap

| Phase | Scope | Done when |
|---|---|---|
| P0 Foundation | Private mirror; own README and guides; base project's pages, sponsor feature, links and self-updater removed; program, service, paths, interface and notification texts named Manifold; own installer, management command, systemd unit and release workflow; CI trimmed to one gate | Done 2026-11-14. Open items in 8.1 |
| P2 OpenVPN | Engine 4.4, inbound form, `.ovpn` in the client pages and the subscription page, traffic and limits, node support | Done 2026-11-14 (0.2.0). A real client connects, is billed, is held to its device limit and is cut off when disabled or out of quota, on this server. Open items in 8.2 |
| P1 WireGuard | Engine 4.3, inbound form, `.conf` and QR in the client pages, traffic and limits, node support, takeover of existing servers | Done 2026-11-14 (0.3.0). A real peer connects, is billed, loses its peer when disabled or out of quota, and an interface `wg-quick` already runs is taken over with its WGDashboard names and usage while its peer keeps connecting. Open items in 8.3 |
| P3 L2TP/IPsec | Engine 4.5, inbound form, the account's four values in the client pages, traffic and limits, node support, installer and unit | Done 2026-11-15 (0.4.0). A real L2TP client behind NAT connects through IPsec, is billed, is held to its device limit and is cut off when disabled or out of quota, under the installed service on Ubuntu 22.04, 24.04 and 26.04. No phone or Windows machine has connected yet. Open items in 8.4 |
| P4 Access model | Items in 4.6 | A reseller can create accounts only within its limits and sees only its own |
| P5 Events and API freeze | Items in 4.7; OpenAPI v1 for accounts, plans, resellers, usage | A test receiver gets signed events for every account state change, including after downtime |
| P6 NX-parity operations | Setup wizard, backup upgrades, OIDC, managed tunnels, subscription themes, usage CSV | Checked against the gap table |
| P7 Mirza Bot | Section 7 | A purchase in the bot yields one account with every protocol output, and panel events update the bot without polling |

The protocol engines come first because they carry the most technical risk and everything else builds on the account model they must fit into. The phases keep their numbers; they are built in the order of the table.

## 7. Mirza Bot phase (outline)

Findings that shape it:

- The bot already has a driver layer: `panels/BasePanelDriver.php` with `create`, `read`, `revokeSub`, `remove`, `modify`, `changeStatus`, `resetUsage`, `extendService`, `extraVolume`, `extraTime`, and drivers for 3x-ui (`XuiSingle.php`) and WGDashboard among twelve.
- The Telegram Mini App ships only as a built bundle (`app/assets/*.js`); its source is not in the repository. A better customer UI means writing a new Mini App against `api/miniapp.php`.
- The bot core is two large files: `admin.php` (about 9,500 lines) and `index.php` (about 6,900 lines).

Work:

1. New driver and API client for Manifold, against the OpenAPI v1 frozen in P5.
2. Webhook receiver replacing polling cron jobs where the panel now pushes events.
3. Multi-protocol delivery: subscription link, WireGuard `.conf`, `.ovpn`, L2TP credentials, per product.
4. Reseller mapping between the bot's agent system and panel resellers.
5. New Mini App and bot message redesign.

## 8. Repository and fork maintenance

Branches in `FarzadSec/Manifold`:

- `main` — the product branch.
- `upstream-main` — pristine `MHSanaei/3x-ui` `main`. It only ever fast-forwards from upstream and is then merged into `main`.
- Phase branches (`p0/foundation`, `p1/wireguard`, …) — where work happens before it reaches `main`.

Manifold's documents live in `manifold/`, a directory upstream does not have: this file, and `docs/` with the installation, usage and development guides. The merge procedure and the list of removed paths are in `docs/development.md`.

What must stay from the base project: the `LICENSE` file and the attribution in `NOTICE`. Both are conditions of the GPL-3.0 licence the code is used under. The Go module path (`github.com/mhsanaei/3x-ui/v3`) also stays: it is an internal identifier no user sees, and changing it would rewrite the imports of every Go file and turn every upstream merge into a conflict.

3x-ui moves fast (4,011 commits; last upstream commit 2026-11-13). To keep merges cheap:

- New work lives in new packages: `internal/wgquick`, `internal/openvpn`, `internal/l2tp`, `internal/egress`, and later `internal/rbac` and `internal/webhook`.
- Upstream features the product does not use (AmneziaWG, Xray's WireGuard inbound) are switched off and hidden, not deleted.
- Edits to shared upstream files stay as small as the integration allows.
- Upstream is merged on a fixed cadence, not ad hoc.

GitHub Actions: three workflows. `ci.yml` is the gate: phase branches run the fast jobs (about 20 runner minutes), `main` and pull requests run everything (about 52). `vendor.yml` republishes `go mod vendor` to the `build/vendor` branch whenever dependencies change. `release.yml` builds the Linux packages, installs and exercises them on three Ubuntu versions, and publishes them when `product_version` on `main` names a version with no release yet, or when a version tag is pushed. The base project's release, Docker, CodeQL, mutation-test and cache-cleanup workflows are removed.

Build verification: the workspace this plan is written from cannot reach the Go module proxy, so it builds and tests offline from `build/vendor`, and CI in this repository is the final word on every change.

### 8.1 Open items after P0

- Fixed in 0.2.0: the installer reported success for a panel that died right after starting. It now waits until the service's own process holds the panel port.
- The Nodes page still shows its update actions. Nodes refuse them with a clear message; the buttons, the bulk selection that only served them and their tests are to be removed.
- Done in 0.3.0: AmneziaWG is no longer offered for a new inbound, and `wireguard` is the kernel engine.
- The Nodes page shows each node's base version, not its product version.
- The release workflow's arm64 package is built but only the amd64 package is installed and exercised on a runner.
- The container image is not built or started in CI. The Dockerfile, entrypoint and compose file were changed together with the installer and are checked only by the shell linter.
- `inbound-form-modal.test.tsx` ("keeps the persisted node share strategy through the nodes-loading race") timed out once on a runner and passed on the runs before and after; it is a timing-sensitive test inherited from the base code.

### 8.2 Open items after OpenVPN

- Fixed in 0.4.0: under the installed service no OpenVPN inbound started on systemd 245 to 251 (Ubuntu 22.04, Debian 11), because `ProtectClock` closed `/dev/net/tun`. The engine had only been run with clients outside the unit.
- OpenVPN on a remote node is covered by a unit test of the capability check only. No test runs a master, a node and an OpenVPN client together.
- Shared quota across protocols rests on the one traffic row per account that every engine writes to. It is not exercised by a test that uses an OpenVPN client and an Xray client on the same account.
- The online mark of an OpenVPN account is recorded through the Xray process's online set, as the other side engines do, so it is missing while Xray is down. Traffic and limits do not depend on it.
- "Copy all links" on the Inbounds page includes the `openvpn://` links, which are of no use outside the panel.
- After the panel is killed (not stopped), the NAT table of an OpenVPN inbound stays until the next start cleans it up. `manifold uninstall` right after such a kill leaves it behind.
- The container image now carries OpenVPN but, like the rest of the image, is not built or run in CI.

### 8.3 Open items after WireGuard

- The engine's tests on the development machine ran against `wireguard-go`, which `wg-quick` falls back to on a kernel without the module. The kernel module itself is exercised only on the CI runners: `vpn-e2e` on Ubuntu 26.04 with its AppArmor profile for `wg-quick`, and since 0.4.0 the release smoke test under the systemd unit on Ubuntu 22.04, 24.04 and 26.04. No other distribution is tested.
- Fixed in 0.4.0: `manifold uninstall` left the inbounds' configs, private keys included, in `/etc/wireguard`.
- Under that AppArmor profile a hook can run `ip`, `wg`, `nft`, `iptables` and little else. An imported server whose hooks need more keeps failing to start until the admin extends the profile; the import does not check for this.
- WireGuard on a remote node is covered by a unit test of the capability check only, like OpenVPN.
- An inbound made before 0.3.0 switches from Xray to the kernel interface on update. No test updates a real 0.2.0 server with such an inbound; a client that was given the first address of the subnet would collide with the server's.
- NAT covers IPv4 only. IPv6 addresses of an imported server are carried over; forwarding and NAT for them is left to the server's own hooks.
- The import reads WGDashboard's SQLite database only, and only on the panel's own server. Peers of an interface that lives on a node cannot be imported.
- The branches of `internal/web/service/xray.go` that built Xray's WireGuard peer list are no longer reached. They stay to keep upstream merges cheap.
- Saving a WireGuard inbound on a server without `wireguard-tools` succeeds; the form warns and the inbound starts once the package is there. OpenVPN refuses the save instead. One of the two behaviours should become the rule.
- The container image carries the WireGuard tools but, like the rest of the image, is not built or run in CI.

### 8.4 Open items after L2TP/IPsec

- One browser test opens the panel's pages against the installed service (`deploy/test/panel-ui.cjs`, in the release smoke test): it creates the three engines' inbounds and a client through the forms and requires the client's two windows and the subscription page to show something to hand out for each. Nothing else of the interface is driven this way: editing, limits, nodes and the Xray protocols' forms are covered by component tests only.
- The first tab of the subscription page offers the subscription address and its QR code to every account, also to one that has only OpenVPN or L2TP, where a proxy app would import nothing from it.
- The only client that has connected is the test's own: strongSwan, xl2tpd and pppd on Linux, behind a NAT router. No Windows, macOS, iOS or Android device has. The IKE and ESP offers for them come from their documentation, and Android versions 6 and 7 are known to mishandle the SHA-256 ESP offer that the server prefers.
- No session has run for longer than about a minute. strongSwan rekeys a session after an hour and re-authenticates after four with its defaults; whether the built-in clients stay connected across both has to be seen on real devices. Leaving rekeying to the client (`rekey_time = 0` for the connection and its child) is the usual remedy if they do not.
- Two devices behind one public address may not stay connected together. strongSwan's `connmark` plugin is the known answer and is not used.
- A server behind NAT (a cloud address mapped onto a private one) is not tested. Windows needs a registry change for such a server, and whether strongSwan's default identity is accepted there by every client is unknown.
- Dead sessions are found by PPP's own echo after about two minutes; their IPsec state stays until strongSwan's lifetime ends, because the connection sets no dead peer detection.
- The module that lets the kernel carry L2TP sessions (`l2tp_ppp`) is in `linux-modules-extra` for the kernels Ubuntu 22.04 and 24.04 started with. Without it xl2tpd relays every packet itself, which works and is slower; the installer does not install that package. Both ways are exercised by the release smoke test: the 22.04 runner (kernel 6.8) has no module, the 24.04 and 26.04 runners (kernel 7.0) do.
- The installer's package names for strongSwan outside Debian and Ubuntu, and the service name it starts there, are taken from those distributions' documentation and have not been run.
- L2TP on a remote node is covered by a unit test of the capability check only, like the other two engines.
- An account that is disabled is cut off at the next ten-second tick, not at once, and a new login in that window still succeeds.
- The L2TP and OpenVPN engines each carry their own process supervisor and their own validators for subnets, DNS lists and user names. They are the same in substance and should become one.
- L2TP is not available in the container image.

## 9. Assumptions and open decisions

Assumptions made where no answer was given:

- **NX Panel**: features are re-implemented from its public docs; nothing of its code or UI is reused.
- **WGDashboard**: its Python codebase is not forked; its WireGuard mechanism is reproduced in Go (4.3), and an existing server is taken over through the import in the Inbounds page.
- **Phase one** is the panel only, starting with the protocol engines.

Open decisions:

1. **"Same UI"**: assumed to mean 3x-ui's React and Ant Design interface, extended consistently for the new protocols and pages. NX's interface cannot be copied.
2. **sing-box-only protocols** from NX (constraint 3): excluded from the MVP unless required.
3. **IKEv2**: whether it should be offered next to L2TP from the same strongSwan. Android from version 12 on has no L2TP in its own VPN settings and does have IKEv2, so today such a phone can only be served by another protocol's app.

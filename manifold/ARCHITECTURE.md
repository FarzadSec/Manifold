# Manifold — Architecture and Roadmap

Version 0.4 · 2026-11-14 · Status: P0 in progress

Decision log:

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
│   ├─ L2TP/IPsec                   child processes    NEW  internal/l2tp + internal/radius
│   └─ AmneziaWG, Xray WireGuard    upstream code      switched off and hidden
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

The rules in `CLAUDE.md` keep applying to fork code: every new route is registered in `frontend/src/pages/api-docs/endpoints.ts`, model changes need a migration in `db.go`, and state-changing operations go through `runtime.Runtime`.

### 4.3 WireGuard engine — standard kernel WireGuard, the WGDashboard way

What WGDashboard v4.3.5 does, read from its source (`WireguardConfiguration.py`, `Peer.py`):

- One interface per configuration, stored at `/etc/wireguard/<name>.conf`.
- Interfaces are brought up and down with `wg-quick up|down <name>`; live state is written back with `wg-quick save <name>`.
- Peers are applied live with `wg set <name> peer <public-key> allowed-ips … preshared-key …` and removed with `wg set <name> peer <public-key> remove`.
- Usage, handshakes and endpoints are read from `wg show <name> transfer`, `latest-handshakes` and `endpoints`.
- A restricted peer is removed from the interface and parked in a `<name>_restrict_access` table; allowing it again re-adds it.
- Interface settings it manages: `Address`, `ListenPort`, `PrivateKey`, `DNS`, `MTU`, `Table`, `PreUp`, `PostUp`, `PreDown`, `PostDown`, `SaveConfig`.

Our engine (`internal/wgquick`) does the same thing with the same files and the same `wg` and `wg-quick` tools:

- Several interfaces per server, each an inbound.
- Each peer belongs to a panel account, so quota and expiry are shared with the account's other protocols. Enforcement is WGDashboard's restrict mechanism: remove the peer on disable, expiry or exhausted quota; re-add it on renewal.
- Carried over from WGDashboard: scheduled peer jobs, share links with expiry, session and endpoint history, per-interface backup and restore, raw config editing, hook editing.
- Output: the standard `.conf` and QR code per peer.
- On a node, the node's own panel instance runs the engine locally; operations reach it through `runtime.Runtime`.

Adopting an existing WGDashboard server: because the files and commands are identical, the panel takes over the existing `/etc/wireguard/*.conf` in place and imports peer names, restricted peers and usage totals from WGDashboard's database. Server and peer keys do not change, so users keep their current configs.

Not used:

- **AmneziaWG.** Upstream's code stays in the tree, switched off and hidden, so upstream merges stay cheap.
- **Xray's WireGuard inbound.** Upstream's `wireguard` protocol runs inside Xray in userspace, not through `wg-quick`. It is not offered as the WireGuard product.

Requirement: root and the kernel WireGuard module on the host (`NET_ADMIN` under Docker).

### 4.4 OpenVPN engine (proposal, to validate in a spike)

- One `openvpn` server process per inbound, supervised like `mtg-multi`.
- Panel-managed PKI per inbound: CA, server certificate, `tls-crypt` key.
- Authentication by username and password, decided by the panel through the OpenVPN management interface (`management-client-auth`). Enable, disable, expiry and quota then need no file rewrites and no restarts.
- Accounting from the management interface's per-client byte counters into the traffic job.
- Enforcement with `client-kill` on disable, expiry or exhausted quota; device limit by counting concurrent sessions per account.
- Output: an inline `.ovpn` profile per account (UDP and TCP remotes) from the subscription server.
- Egress: kernel NAT first. Steering into Xray routing, so WARP and outbound rules apply, is a later step.

### 4.5 L2TP/IPsec engine (proposal, to validate in a spike)

- Daemons: strongSwan (IPsec, PSK) + xl2tpd + pppd, written and supervised by the panel.
- AAA: a small RADIUS server embedded in the panel; pppd authenticates and reports interim accounting through its RADIUS plugin. Fallback if that proves fragile: generated `chap-secrets`, `ip-up`/`ip-down` hooks and interface counters.
- Per inbound: PSK, address pool, DNS.
- Output: server address, PSK, username and password on the subscription page; optional `.mobileconfig` for Apple devices.
- Known weaknesses to accept or mitigate: fixed ports (UDP 500, 4500, 1701) and no obfuscation, so it is easy to block; newer Android versions dropped L2TP from the built-in client (to verify). IKEv2 from the same strongSwan install is a cheap add-on.
- The embedded RADIUS can later serve IKEv2-EAP, SSTP or OpenConnect (ocserv) without a new accounting design.

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
| Standard kernel WireGuard (`wg-quick`) | Absent; its WireGuard inbound runs inside Xray in userspace | User, WGDashboard | New engine (4.3) |
| AmneziaWG | Present | Not wanted | Switch off and hide |
| OpenVPN | Absent | User, NX | New engine (4.4) |
| L2TP/IPsec | Absent | User | New engine (4.5) |
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
| P0 Foundation | Private mirror (done); own README and guides, base project's pages removed (done); rename binary, service, paths and install scripts from `x-ui` to `manifold`; remove the sponsor, donate and documentation links and the base project's name from the interface; repoint or disable self-update; CI trimmed to what the fork needs; staging server | Nothing a user sees or runs carries another project's name, a Manifold build installs beside a stock x-ui on one server, and `make verify` passes |
| P1 WireGuard | Engine 4.3, inbound form, `.conf` and QR in subscriptions, traffic and limits, node support, WGDashboard adoption | An existing WGDashboard server is taken over and its users connect without new configs; a peer and an Xray client on one account draw down the same quota |
| P2 OpenVPN | Engine 4.4, inbound form, `.ovpn` in subscriptions, traffic and limits, node support | Same shared-quota test with an OpenVPN client, locally and on a remote node |
| P3 L2TP/IPsec | Engine 4.5, embedded RADIUS, optional IKEv2 | Same test with a native L2TP client |
| P4 Access model | Items in 4.6 | A reseller can create accounts only within its limits and sees only its own |
| P5 Events and API freeze | Items in 4.7; OpenAPI v1 for accounts, plans, resellers, usage | A test receiver gets signed events for every account state change, including after downtime |
| P6 NX-parity operations | Setup wizard, backup upgrades, OIDC, managed tunnels, subscription themes, usage CSV | Checked against the gap table |
| P7 Mirza Bot | Section 7 | A purchase in the bot yields one account with every protocol output, and panel events update the bot without polling |

The protocol engines come first because they carry the most technical risk and everything else builds on the account model they must fit into. WireGuard leads because its mechanism is fully known from WGDashboard's source and live users already depend on it.

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

- New work lives in new packages: `internal/wgquick`, `internal/openvpn`, `internal/l2tp`, `internal/radius`, `internal/rbac`, `internal/webhook`.
- Upstream features the product does not use (AmneziaWG, Xray's WireGuard inbound) are switched off and hidden, not deleted.
- Edits to shared upstream files stay as small as the integration allows.
- Upstream is merged on a fixed cadence, not ad hoc.

GitHub Actions: two workflows remain. `ci.yml` is the gate: phase branches run the fast jobs (about 20 runner minutes), `main` and pull requests run everything (about 52). `vendor.yml` republishes `go mod vendor` to the `build/vendor` branch whenever dependencies change. The base project's release, Docker, CodeQL, mutation-test and cache-cleanup workflows are removed; Manifold's own release workflow is written with the rename.

Build verification: the workspace this plan is written from cannot reach the Go module proxy, so it builds and tests offline from `build/vendor`, and CI in this repository is the final word on every change.

## 9. Assumptions and open decisions

Assumptions made where no answer was given:

- **NX Panel**: features are re-implemented from its public docs; nothing of its code or UI is reused.
- **WGDashboard**: its Python codebase is not forked; its WireGuard mechanism is reproduced exactly in Go (4.3) and existing servers are taken over in P1. Until then the current servers keep running as they are.
- **Phase one** is the panel only, starting with the protocol engines.

Open decisions:

1. **"Same UI"**: assumed to mean 3x-ui's React and Ant Design interface, extended consistently for the new protocols and pages. NX's interface cannot be copied.
2. **sing-box-only protocols** from NX (constraint 3): excluded from the MVP unless required.
3. **L2TP**: confirm it is needed given the weaknesses in 4.5, and whether IKEv2 should ship with it.

# Manifold — Architecture and Roadmap

Version 0.20 · 2026-11-19 · Status: P0, OpenVPN, WireGuard, L2TP/IPsec, tunnels, full backups, the access model, limits and usage, subscriptions, nodes, shared ports, scheduled backups, webhooks, monitoring, the certificate store, importers, tunnel groups, the addon groundwork, the 3x-ui style installer and management menu, Manifold Relay, the app API and the tunnel test delivered; see 8.1 to 8.22 for what they leave open

Decision log:

- 2026-11-19 — A tunnel group's members may be followed by backups that carry its ports only while no member answers, and a balanced group may hash the client's address (`jhash ip saddr`) instead of taking turns (`numgen inc`), so a client's flows all reach one origin, which L2TP/IPsec needs across UDP 500 and 4500. A group's forwards are validated as a kernel link's, since they are the relay's own DNAT rules whatever its tunnels are: UDP alone is allowed, a remapped range is not. A kernel link forwards ESP (`meta l4proto esp`); BackPack cannot carry it. An enabled L2TP inbound on a relay keeps UDP 500 and 4500 from its tunnels' forwards.

- 2026-11-19 — The app frees a seat by saying it left (`POST <sub>/app/v1/disconnected`), since WireGuard has no goodbye and an OpenVPN 2.6 server closes a session five seconds after its own. The panel the app signed in to works out which servers hold that location's inbounds of the protocol, cuts what it holds there at once, and the master passes the rest to the node in its next plan, no longer counting it meanwhile. It names WireGuard by account, location and protocol, OpenVPN only by the tunnel address the app was given, and does nothing for L2TP, which hangs up by itself, so a device can never free another device's seat. A cut WireGuard peer's past traffic no longer counts; it takes a seat again only when it is heard anew.

- 2026-11-19 — The user limit across servers is kept by the master, not by each node asking it at login: every two seconds `POST /panel/api/seats/sync` (node-sync scope) hands a node its plan and returns its seats, one request per node. The plan is what each account holds on the other servers, counted there against its limit for 30 s, and the seats to cut when an account got in on two servers in the same tick, newest first under its lowest limit. A login never waits on the master, and a node cut off from it counts its own seats, so a master outage refuses nobody it would not refuse alone.

- 2026-11-19 — WireGuard holds a user-limit seat while its peer is heard (received bytes grew in the last minute and a handshake in the last three), and since it has no login to refuse, a full account's peers are taken off every interface it is not connected on, every two seconds, and put back when a seat frees: a second device cannot complete a handshake, instead of connecting and being cut. A limited account's configs carry `PersistentKeepalive = 25` unless the client sets its own, since an idle device that sends nothing would lose its seat. An account created over the API on several WireGuard inbounds gets one key for all of them, as the panel's form already made it; edits keep each inbound's key, as before.

- 2026-11-19 — The user limit is one count per account across OpenVPN, L2TP and WireGuard and across every inbound, kept by a seat ledger (`internal/seats`) that every engine of a server asks before it lets a connection in, instead of each engine counting its own sessions: a single-user account on OpenVPN was let into L2TP, into WireGuard and into the same location's other OpenVPN inbound. First come, first served: a further connection is refused until one ends, a lowered limit cuts the newest seats, and the client's IP plays no part, since behind a relay every user arrives from the tunnel's address. Xray stays under the IP limit. OpenVPN and L2TP use the ledger from this change; WireGuard and the count across nodes follow. A server whose devices vanish frees their seats sooner: OpenVPN `keepalive 10 30` (60 s on the server) and PPP echoes every 10 s with 3 misses, and UDP profiles carry `explicit-exit-notify 1` so a client's own disconnect frees its seat at once.

- 2026-11-19 — The tunnel test is run by one panel against the other through a link, not by a tool run on each server by hand: the panel that imports the link drives the test through the other's API with a credential that exists only in memory and reaches nothing but the test's peer route. Each server builds its own end of every candidate from the candidate's place in the catalogue and two secrets, so that credential cannot make the other server build anything outside the catalogue or aimed anywhere but at the test's other server. The relay measures, as users' traffic enters there, over a protocol of the test's own on the origin's side, on TCP and on UDP, so a UDP-only transport is reported as such and the recommendation can tell tunnels for TCP inbounds from tunnels for UDP ones.

- 2026-11-18 — The database is chosen at install and switched from the `manifold` command in both directions, SQLite and PostgreSQL, as 3x-ui offers. The installer hands PostgreSQL to `manifold db postgres`, so one function sets up the server, the role and its password login, client tools as new as the server, and the panel's boot order after it. A move copies every table with the panel stopped and is undone when the panel does not come up on the other side. A panel on PostgreSQL has no `manifold.db`, so the installer recognises an installed panel by the env file too; before, an update would have treated it as a first install and replaced its login

- 2026-11-18 — A PostgreSQL backup is restored through psql as one transaction, built from pg_restore's own SQL: everything but the device index, then the merge of duplicate device rows, then that index. pg_dump writes a unique index only when it is valid, yet a source whose index was damaged still hands over rows that break it; refusing the whole backup over device records, which merge cleanly, left the owner unable to restore it. Duplicates in any other unique key still refuse the restore and change nothing

- 2026-11-17 — The VPN app signs in on the subscription server with the subscription link and a username, not with an account of its own: every customer who has a link can use it, whatever the plan, and nothing new is sold or stored about them. A device signed in to the app takes a slot of the link's HWID limit, the same table every other subscription app fills, so app devices and iOS devices count against one limit and cannot be used to get round it. The username only guards the app's sign-in; the link alone still serves its configurations to other apps, as it always did

- 2026-11-17 — The relay is a product of its own, Manifold Relay, chosen at the installer's first question, but built from the same code and released with it: both ends of a tunnel speak one pairing format and one relay API, and one code base keeps them in step. It has its own package without Xray, mtg or the geo files, its own pages and menu, a tunnel-scoped token in its install summary, and the managing panel shows each relay's version (sent as `X-Manifold-Version` on tunnel answers) so drift is seen

- 2026-11-17 — The installer and the `manifold` command follow 3x-ui's: a first install from a terminal asks for the port and how to secure the panel (a Let's Encrypt certificate for the server's IP is the default), and `manifold` alone opens a numbered menu. Both stay non-interactive without a terminal, because CI, scripts and the master's SSH node install feed them on stdin. Certificates they get go through the certificate store, so the panel renews them like any other

- 2026-11-17 — An IP address is a certificate name like a domain: ordered under Let's Encrypt's `shortlived` profile, over HTTP-01 only. Renewal and expiry warnings are a third and a sixth of each certificate's own life (capped at 30 and 14 days), not fixed days, which a 160-hour certificate would always be inside

- 2026-11-17 — Addons run outside the panel and reach it only through a scoped token and a webhook, not as code loaded into it: a broken or hostile addon is limited to what its manifest asked for and the installer granted, and uninstalling it is revoking two credentials

- 2026-11-17 — Several origins behind one relay are balanced in the relay's kernel (nftables `numgen` over DNAT targets), not in a proxy process: conntrack keeps each connection on the origin it started on, nothing is in the data path to crash, and failover is a table rewrite driven by the tunnel probes the panel already runs

- 2026-11-17 — Old Marzban/PasarGuard links are verified, not looked up: their tokens carry the username and a signature with the old panel's secret, and a new one was minted every time a link was shown, so no list of tokens exists to import. The panel keeps the secret and checks each token as Marzban did, refusing tokens older than the user's revocation

- 2026-11-17 — Certificates are issued by the panel itself on `golang.org/x/crypto/acme`, already in the module graph, instead of calling acme.sh: one process, errors the panel can show, and renewal that knows which inbounds and nodes read a certificate. The master issues (DNS-01 works from anywhere) and copies the pair to nodes, so a node needs neither a token nor a free port 80

- 2026-11-17 — Metrics are an API route behind `stats:read`, not a second listener with its own secret: tokens already have scopes, an expiry and a rate, and Prometheus sends a bearer token as it is

- 2026-11-17 — Webhooks go through an outbox table, not straight from the bus: a delivery survives a restart, is retried on a schedule, and the log the admin reads is the same rows that are sent. A reseller's webhook is dialled through a connection guard that refuses loopback, private, link-local and shared address space after DNS, because its delivery log shows the answer and would otherwise read this server's own services back

- 2026-11-17 — Scheduled backups reuse the full backup of 0.5.0 and add only where it goes: `internal/backupstore` writes to a directory, to S3 with a Signature V4 signer of its own (checked against AWS's worked example; no SDK), or to a server over SSH through its shell, which works with SFTP-only accounts that keep a shell. Sealing is AES-256-GCM under PBKDF2-SHA256 (600000 rounds, the standard library's), with the header authenticated
- 2026-11-17 — Ingress is a layer-4 router of the panel's own (`internal/ingress`): it reads the ClientHello (reassembled across records) or an HTTP request head, picks a route by SNI or Host and ALPN, and hands the stream on byte for byte, so REALITY behind it still sees the client's handshake. It does not terminate TLS; NX Panel's HTTP-level listeners and routes for the panel itself are not built
- 2026-11-17 — A node is installed over SSH by the panel the admin is signed in to, with credentials used for that job only. The host key is read with a handshake that stops at the key, so nothing is offered before the admin accepts it, and every later login checks that key. The node's self-signed certificate is pinned from the installer's output over that SSH channel, not by a first TLS connection that a man in the middle could answer
- 2026-11-17 — A node's periodic usage limit switches its enabled inbounds off through the runtime and records which, so the next period switches back on exactly those; an inbound the admin had switched off is never turned on by it. The limit reads the per-node usage history, so it counts raw bytes, before the node's traffic multiplier
- 2026-11-17 — Node availability is recorded every five minutes as samples; a stretch with no sample is "not watched", never "down", so a panel restart does not read as a node outage
- 2026-11-17 — API tokens are throttled only when the admin names a rate. A default rate throttled masters that drive nodes with admin tokens
- 2026-11-17 — Subscription links are built from a domain plan: a list of hosts (the first for new links, all accepted), a reseller's own domain, and a wildcard that gives each user a host of its own, `k` and nine base-36 characters of an HMAC of the user and the domain, so one blocked name costs one customer and the name never gives the subscription away
- 2026-11-17 — sing-box is a format of its own (`/singbox/`), built from the entries the Clash format collects and merged into an operator base profile, instead of a second link builder that would drift from the Clash one
- 2026-11-17 — Subscription page themes are Go `html/template` pages in a folder with their files, as the base code's single custom folder already was. Two are built into the binary; a catalogue in this repository (`sub-themes/catalog.json`) lists more, installed into `/etc/manifold/sub-themes` only once every file has arrived and the page parses. NX Panel's own theme catalogue is not used: its themes are written for its data model
- 2026-11-17 — The speed limit is one nftables table per host (`manifold_speed`, `internal/speedlimit`), not shaping inside each engine: Xray has no per-user limiter, and one table gives a user one budget per direction across every engine on the host, the way NX Panel's applies on each node separately. Each user has two `limit` objects; packets find theirs through maps keyed by socket mark (Xray), tunnel address (WireGuard, OpenVPN) and ppp interface (L2TP). A change touches only the entries that moved, because replacing a limit object refills its bucket.
- 2026-11-17 — A limited Xray user's traffic goes out of a socket carrying the mark `0x4e000000 | client id`. After the admin's own rules have decided the target, a stateless dialer (freedom, VLESS, VMess, Trojan, Shadowsocks, SOCKS, HTTP, Hysteria) is copied for the user with the mark set; a balancer, a WireGuard or MASQUE outbound, a chain or an outbound with its own mark is reached through a marked loopback SOCKS hop that hands the connection to the real target. Copies of WireGuard would fight over one peer session. Every copied rule keeps its conditions and order, so the user is routed exactly as before.

- 2026-11-17 — The access model follows NX Panel's: three tiers (main admin, operator, reseller), custom roles that only narrow a tier, and one permission catalogue shared by roles and API tokens. Every API route carries a rule in `internal/web/access/routes.go`; a route without one is refused to everyone, and `TestEveryAPIRouteHasAnAccessRule` pins the table to the router both ways.
- 2026-11-17 — A reseller reaches only the users it owns (`clients.owner_id`). The guard sits in front of the client routes and checks the users a request names before the handler runs; lists are filtered in the handlers. Staff routes (import, export, groups, every-user operations, node sync) are never a reseller's, whatever its role holds.
- 2026-11-17 — A reseller is charged its users' traffic in the transaction that adds it to them, local and node traffic alike, so past traffic stays with whoever owned the user. A minute job starts new cycles and switches a reseller's users off when it runs out, marking them, and switches back on only those when it is renewed.
- 2026-11-17 — Inbounds are one pool, stored under the owner's account (`inbounds.user_id`), also across an ownership handover. The base code stored them under whoever made them and read them back by the signed-in account, which would have shown a second admin no inbounds.
- 2026-11-17 — Two-factor authentication belongs to each account. The panel-wide secret moved to the owner; the login page asks for a code when any account has one.
- 2026-11-17 — Plans are copied onto a user once and leave no link but its group, so editing a plan never changes what a customer bought.

- 2026-11-16 — Tunnels are a subsystem of their own (`internal/tunnel`, tables `tunnel_ends` and `tunnel_relays`), not an inbound protocol: a tunnel carries no accounts, and the relay end has no inbounds at all. The panel that serves the accounts runs the client end; the relay users connect to runs the server end and forwards its ports to the client end.
- 2026-11-16 — Kernel links (GRE, GRETAP, IP6GRE, IP6GRETAP, IPIP, SIT) are Manifold's own code driving `ip` and nftables. DrSaeedHub's Tunnel-Panel, which the owner pointed to, is AGPL-3.0; none of its code is used, only the idea of a pairing code with the sides swapped.
- 2026-11-16 — BackPack tunnels run the unmodified BackPack release binary (v1.8.5, checked against its published checksums) as a child process per end, on a TOML file the panel writes, and are read through the snapshot BackPack writes beside it. BackPack is AGPL-3.0 and asks a modified copy to carry another name; linking or forking it would bind the panel to both, a separate program binds it to neither. NOTICE and README credit it.
- 2026-11-16 — A kernel link outlives a panel restart: its alias records a fingerprint of what it was made from, and a restarted panel that finds the same fingerprint leaves the link up. Tearing it down on every update would cut every user who crosses it. `manifold uninstall` removes the links, their tables and rules.
- 2026-11-16 — A relay masquerades the forwarded ports by default, which always works. Keeping each user's address is offered too, because the device limit and the IP log need it: the client end marks connections that came in through the tunnel and routes their replies back by a table of its own, with loose reverse-path filtering on the link.
- 2026-11-16 — The relay's end is set up either by a pairing code imported there, or by this panel itself through the relay's API with a token of a new scope, `tunnel`, which reaches nothing but the relay's tunnel ends. An end is identified by a pair id, so importing a newer code updates it in place.
- 2026-11-16 — The relay is the same package installed as the tunnel edition (`MANIFOLD_EDITION=tunnel`): no Xray, no subscription server, no account jobs, and an interface limited to the overview, tunnels, settings and API. One code base and one release keep the two ends' versions in step.
- 2026-11-16 — A full backup is a `.tar.gz` of the database (SQLite, also exported from a PostgreSQL panel), the certificates and keys the settings and inbounds point at, and a manifest with the version and public address. A restore first reports, changing nothing, what the move does to every address users' configs carry; whether a config keeps working depends only on that address, because accounts keep their keys and passwords.
- 2026-11-16 — An OpenVPN inbound's certificates are the admin's to choose: issued with a key type (ECDSA or RSA, for routers without ECDSA), lifetime and names, or brought in. Profiles verify the server certificate's own name, stored with the inbound; inbounds saved before keep verifying the old fixed name, which their certificates carry.

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
├─ Tunnels to relays                internal/tunnel    NEW  kernel links (ip, nftables), BackPack child processes
├─ Full backup and restore          internal/web/service/migration.go   NEW
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
- Since 0.5.0 an inbound also sets `tun-mtu` and `mssfix` on both ends, a limit on the data ciphers, split tunnelling (no pushed default route), the control channel key (`tls-crypt`, `tls-auth` with `auth SHA256`, or none) and extra profile lines, which may not repeat a directive the panel writes or add a script hook. Its key set can be issued anew (`POST /server/openvpnKeys`: ECDSA P-256 or P-384, RSA 2048 or 4096, lifetime, CA and server names) or brought in, and the form shows each certificate (`POST /server/openvpnCertInfo`). Changing any of these makes the profiles handed out before stale; the form says so.
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

### 4.6 Access model (built, 0.6.0)

- Accounts (`users`) carry a tier, an optional custom role (`roles`), the owner flag, their own two-factor secret and, for a reseller, its allowance: volume per cycle (weekly, monthly or yearly from a day), expiry, user limit, may-edit, plans-only with allowed plans, allowed inbounds, and branding (profile title, support link, announcement) its customers' subscriptions use.
- Permissions (`internal/web/access`): `users`, `plans`, `nodes`, `tunnels`, `pool` (inbounds, hosts, outbounds, routing), `settings`, `stats`, `tools`, `services`, `admins`, `tokens`, `backup`, `audit`. A role keeps a subset of its tier's; a role's caps (users per account, devices per user, plan length) hold over the account's own values.
- A role's users scope (`users_read_scope`, `users_write_scope`: all or own; a reseller is own/own) decides which users `guardClientAccess` lets an account reach; staff routes stay shut to an own-scoped account for that access. (0.17.0)
- Role bounds (`min/max_user_bytes`, `min/max_plan_days`, `min/max_devices_per_user`) are held by `ClientAccess.CheckChange` on create, on the limits an edit changes and on a bulk adjust as it would leave each user; a ceiling refuses an unlimited value. (0.17.0)
- `ReachOf` gives the plans and inbounds an account sells from: the stricter of a reseller account's lists and its role's (`plans_only`, `allowed_plan_ids`, `inbounds_limited`, `allowed_inbound_ids`). (0.17.0)
- `users.granted_permissions` and `denied_permissions` change one account's set after its role, inside its tier; denying a read also takes its write; the owner is never narrowed. (0.17.0)
- `users.disabled` refuses an account at login and per request (`ErrAccountDisabled`); a role's `lock_when_limited` refuses a reseller out of allowance (`ErrAccountLimited`). `ResellerService.Enforce` switches an account's users off when its allowance runs out (unless its role has `keep_users_when_limited`) or it is switched off (when its role has `users_off_when_disabled`), recording it in `users.users_off`, and restores exactly the users it marked. (0.17.0)
- API tokens of the admin scope act as one account, below its tier, with fewer permissions and, when set, their own requests per minute; a token without one is not throttled, since masters drive their nodes with admin tokens too. The machine scopes (`monitor`, `node-sync`, `node-admin`, `tunnel`) keep their allow-lists and act as the owner.
- `api_tokens.max_requests` caps a token's lifetime requests; `Spend` counts a capped token in one conditional update per request and deletes it at the cap when `delete_on_limit` is set; `last_used_at` is written at most once a minute for an uncapped token. (0.17.0)
- Plans (`plans`) fill a new user's limits, length (fixed or from first use), reset, inbounds, group and name prefix or suffix.
- The audit log (`audit_entries`) always holds sign-ins, failed and new-address sign-ins and two-factor changes; while change logging is on, every change a person makes, with secrets redacted, kept for the retention (90 days by default).
- Pages: Admins, Roles, Plans, Audit log; the users page gains an owner column, an owner filter, change owner, a plan picker and, for main admins, an owner picker; a reseller's overview shows its allowance and users; every account has its own password and two-factor dialog.

### 4.7 Events and webhooks (built, 0.10.0)

3x-ui had an in-process event bus with 8 events and Telegram, Discord and email notifiers on top. Manifold adds:

- User events from two sources. Changes through the API (`user.created`, `user.updated`, `user.deleted`, `users.bulk_changed`) come from a middleware that publishes only after the handler answered success. What traffic and dates do to a user (`user.enabled`, `user.disabled`, `user.expired`, `user.expiring` a day ahead, `user.limited`, `user.quota_warning` at 80%, `user.first_connected`, `user.traffic_reset`) comes from a watcher every 15 seconds that compares each user with its last look; a user's first look only records it.
- `node.usage_stopped`/`node.usage_resumed` and `backup.succeeded`/`backup.failed`.
- `webhooks` (HTTP, ntfy or Bale; an event list, empty for all) and an outbox, `webhook_deliveries`: one row per webhook per event, written by a bus subscriber, sent by a delivery loop with a backoff of 30s to 8h over eight retries, kept a week. An HTTP delivery is signed: `X-Manifold-Signature: sha256=HMAC-SHA256(secret, "<timestamp>.<body>")`.
- A reseller's webhooks hear only about its own users and may only reach public addresses.

This is the contract Mirza Bot will sync against.

### 4.8 Tunnels (built, 0.5.0)

- An end is a row of `tunnel_ends`: a pair id shared with the other end, a name, a kind, a role (client or server), and the settings its kind reads. `internal/tunnel` normalizes it, runs it and reports on it; the tunnel job reconciles every fifteen seconds and after every change.
- Kernel kinds: `ip link add mft<id> type <kind> remote … local … ttl … key …`, the MTU (outer headers subtracted from 1500 when not set), the tunnel address, and the alias `manifold-tunnel:<fingerprint>`. A link without that alias prefix is never touched.
- The server end forwards each listed port, or range, with DNAT to the client end's tunnel address in an nftables table `manifold_tf<id>`, and accepts the forwarded traffic in FORWARD (`internal/egress`). With the user's address kept, the client end's table `manifold_tr<id>` marks connections that came in through the link and an `ip rule` sends what carries the mark to a table whose default route is the link.
- BackPack kinds: `backpack -c <bin>/tunnels/t<id>.toml`, restarted with a growing delay after it exits, its configuration rewritten and the process restarted when the end changes. `skip_optz` keeps BackPack from tuning the host behind the panel's back. Reverse server ends listen on the tunnel port and expose the forwards with `127.0.0.1:<port>` targets on the client end; layer-3 ends create `mfbp<id>` and serve the forwards on the side that has them.
- State: an end with an interface is probed with ICMP through the tunnel every two seconds over a window of thirty echoes (down after three lost in a row, degraded above 20 % loss or 800 ms); a reverse end reports what BackPack's snapshot says of its control channel. Bytes come from the interface or the snapshot.
- Pairing: `tunnel.Mirror` swaps the sides (role, tunnel addresses, the remote address), and a pairing code is that end as base64url JSON under `manifold-tunnel://`. A relay in `tunnel_relays` is a panel URL and a tunnel-scoped token, sealed like node tokens; the panel imports the mirrored end there on every save and withdraws it on delete, and `POST /tunnels/resync` sends every relayed tunnel again.
- A save is refused when a port it listens on belongs to another tunnel, an enabled inbound, SSH or the panel, or when its tunnel subnet overlaps another tunnel's.
- Groups (0.12.0): `tunnel_groups` joins 2 to 16 relay ends with an interface under shared forwards, in `manifold_tg<id>`. Balance writes `dnat to numgen inc mod N map { … }` over the members that answer; failover writes a plain DNAT to the first. `ReconcileGroups` runs after every reconcile and every ten seconds, and rewrites the table only when the chosen set changes; with no member answering it uses all of them, so a probe that cannot run never takes the ports away. A group's ports are checked against tunnels, other groups, inbounds and the panel, and a member cannot be deleted while a group lists it.

### 4.9 Manifold Relay, the tunnel edition (built, 0.5.0; its own product in 0.14.0)

- Since 0.14.0 the installer asks Manifold or Manifold Relay on a first install from a terminal (`--edition full|relay` otherwise), and a relay downloads `manifold-relay-linux-<arch>`: the program, BackPack and the scripts. A relay's pages say Manifold Relay and list each tunnel end in full on the overview; its `manifold` menu is its own table; `manifold tunnel list|import` and `manifold relay-token` serve a relay run over SSH alone. `tunnel.Manager.Check` measures an end from outside the panel's process (link, BackPack process and snapshot, three echoes).
- `MANIFOLD_EDITION=tunnel` in `/etc/default/manifold`, set by `install.sh --edition relay` (or the old `tunnel`) and kept on update. `config.IsTunnelEdition` makes the panel start only the tunnel job and housekeeping, keeps the subscription server off, and tells the page through `window.X_UI_EDITION`; the sidebar and command palette then list the overview, tunnels, settings and API only, and the overview hides the Xray controls.
- The installer installs `nftables`, `iptables` and `iproute2` for it and none of the VPN engines.

### 4.21 Tunnel test (built, 0.22.0)

- A link (`manifold-tunnel-test://`, base64url JSON) carries the panel URL, a SHA-256 pin of its certificate, a 256-bit credential, the maker's side and addresses, the first of 80 ports (`BenchPortSpan`) and an expiry. `tunnel.PickBenchBase` keeps the span off the maker's range for outgoing connections' ports, Linux's default one (the other server's, which it cannot read) and the inbound forms' random ports (10000 to 60000), and gives those up in turn only where nothing is left. `TunnelBenchService` holds one link or one run per server; `checkAPIAuth` matches the credential in memory as scope `tunnel-bench`, allowed only `POST /tunnels/bench/peer/:op`, and the handler refuses any other caller.
- The importing panel (driver) says hello (its addresses, the key of the test's protocol, its tunnel subnets); the other (host) picks a /24 in 10.79.0.0/16 and a /64 in fd4d:5454::/48 that neither uses, opens its firewall and, as the origin, the answering port. Then for each candidate: `end start` on the host, the driver's own end, the measurement on whichever is the relay (`measure start`, then `poll` each second), `end stop` on both. The driver pushes its report on every change and every 15 seconds; the host shows that report as its own and ends the test after 90 seconds without a call.
- Ends: `tunnel.BuildBenchPair` from (candidate, slot, both sides, pair id, token) gives the same end on either server; kernel links are `mfx<slot>` with the alias `manifold-tunnel-test:<pair id>` and a GRE key of their own, BackPack runs from `bin/tunnel-test`. Reverse ends forward one port, TCP and UDP, to the answering port. `RemoveBenchLeftovers` and `egress.CloseInput` run when the tunnel job starts.
- Firewall: `egress.OpenInput` inserts INPUT accepts, tagged `manifold:bench`, from the other server for the 80 ports over TCP and UDP and for GRE, IPIP and SIT, and for anything arriving on `mfx+`; with iptables-nft they come before ufw's rules.
- Measurement (`tunnel.Measure`): TCP and UDP are awaited together and the other gets three seconds once one answers. TCP: echoes (and ICMP through an interface for loss), a stream each way, then half the streams each way at once with echoes beside them; uploads are framed, since muxed transports close both ways on a half-close. UDP: 50 echoes 20 ms apart, then a paced ladder each way that quadruples while under 2 % is lost and settles at 95 % of what got through when more is; each step gets its share of the cap in a shorter step, never at a lower rate.
- Rounds: every candidate is screened (short phases, no load); the best few overall plus the best two for TCP and for UDP are measured in full; the best with an interface are tried at MTUs 1400, 1360 and 1280 and keep one only when it wins by three points. Scores are out of 100 against the best of the round: TCP weighs throughput, latency, queueing under load and stability (loss and broken streams); UDP its rate, latency and loss; overall 0.6 TCP and 0.4 UDP. The picks are expressed as the client end's form values.
- Reports are kept in `tunnel_benches` on both servers (the JSON, the pick's key, side and peer).

### 4.10 Full backup and restore (built, 0.5.0)

- `GET /server/getFullBackup`: `manifest.json`, `database.db` and `files/<absolute path>` for every regular file of at most 1 MiB that the web and subscription certificate settings, the Xray template or an inbound's settings name under a certificate or key field.
- `POST /server/inspectBackup` unpacks it beside the database, refusing entries it did not write and files whose checksum differs, reads the backup's database read-only and reports: each address users' configs carry (host rows, external proxies, a custom share address, a listen address, else the subscription or web domain, else the old public IP) as same server, through a relay, breaks, or a domain that already leads here, still leads to the old server, or leads elsewhere; each tunnel's action (resync for a managed relay, a new pairing code where the other end names this server's old address, none otherwise); listen addresses and tunnel bind addresses this machine lacks; ports another program holds; missing engine programs; a newer panel version or another edition.
- `POST /server/restoreBackup` writes the files (a different file already there is kept as `.before-restore`), imports the database keeping this machine's panel port, path and certificate unless told otherwise, clears listen and tunnel bind addresses this machine lacks, optionally replaces the old IP with this one's in hosts, share addresses, external proxies and subscription URLs, resends relayed tunnels, and restarts the panel.

### 4.11 Speed limit (built, 0.7.0)

- `clients.speed_limit` (Mbps, each direction, 0 unlimited), on plans too, carried to nodes inside inbound settings so each host polices its own traffic.
- Xray: `injectSpeedLimits` runs last in `GetXrayConfig`; copies are `~speed~<id>~<tag>`, hops `~speed~<id>~via~<tag>` dialing the loopback inbound `~speed~in~<tag>` (from port 63500). Under `IPIfNonMatch` the user's catch-all matches only resolved addresses, so the second pass still happens. A routing script switches Xray limiting off. Copied outbounds' traffic is booked under the outbound they copy; hop traffic is counted once by the real target.
- nft: the output hook copies the mark to the connection and polices what the socket sends; the input hook polices the replies; the forward hook polices tunnel addresses and ppp interfaces. The burst is half a second of the rate and never under 128 KiB, the largest GSO packet.
- The job applies the table every 5 seconds, and at once when an OpenVPN or L2TP session comes up or goes away; a table removed from outside is installed again within a minute.

### 4.12 Subscriptions (built, 0.8.0)

- Domain plan: `subDomains` (one per line, `*.` for a host per user), a reseller's `users.sub_domain`, and a wildcard in `subURI` behind a CDN. `LoadSubHostPlan` builds the links; the subscription server's host check accepts every host of the plan and rereads it every 30 seconds.
- Formats: raw, Xray JSON, Clash/Mihomo, and sing-box 1.12 (`internal/sub/singbox_service.go`), each with its own path and User-Agent auto-detection on the main link; sing-box is tried before Clash.
- Page themes: `subTheme` is `builtin:<name>`, `installed:<name>` or empty for the panel's page; `subThemeDir` (a folder of the admin's own) overrides it. The page and its files are served at `<subscription>/theme/<file>`; the page source and hidden files never are. `internal/subtheme` holds the helpers (`t`, `qr`, `bytes`, `b64`, `dataURI`, `default`, `percent`), the catalogue client and the installer.

### 4.13 Nodes (built, 0.9.0)

- `nodes.traffic_multiplier` scales the deltas `setRemoteTrafficLocked` charges to quotas and resellers; `client_usage` keeps raw bytes.
- `nodes.usage_*` hold a weekly, monthly or yearly cap; `NodeUsageLimitJob` (every minute) sums the node's daily usage rows since the period's first day and stops or resumes its inbounds, remembered in `usage_stopped_inbounds`.
- `node_healths` holds a five-minute sample per enabled node (`NodeHealthJob`), kept 90 days; `/nodes/stats/:id` cuts a window into availability stretches and load points and adds per-user usage.
- `/nodes/logs/:id` reads the node panel's log through `Remote.FetchLogs`; the node-sync scope may call `/server/logs/:count`.
- `nodes.proxy_url` (http, https, socks5) or `nodes.outbound_tag` decides how the panel reaches a node, through `runtime.ProxyURLFor`, for the probe, every `Remote` call and the certificate fetch. (0.17.0)
- `inbounds.link_group` ties copies of one inbound on several hosts. `DeployToNodes` adds a copy per host with the source's settings and users; the update handler carries an edit to the others with `UpdateLinkedCopies`, each keeping its host, tag, traffic, users, switch and share address; `LinkedIDs` widens a create, attach or detach to every copy before an account's inbound list narrows it. (0.17.0)
- `internal/nodessh` reads host keys and runs scripts over SSH; `NodeService.StartInstall` runs `install.sh` with flags, a self-signed certificate, `manifold cert`, `manifold setting -port -webBasePath` and a node-admin token, as a polled job.

### 4.14 Shared ports (built, 0.9.0)

- `ingress_listeners` (routes as JSON); `IngressService.Apply` binds every enabled row at start and on each save, and swaps routes in place without rebinding.
- Port checks run both ways: a shared port refuses a local TCP inbound's, the panel's or the subscription server's port, and `checkPortConflictTx` refuses an inbound on a shared port.
- PROXY protocol v1 and v2 per route, for targets that should see the client address.

### 4.20 App API (built, 0.15.0)

- `POST <subscription path><subid>/app/v1/login` takes `username` (a client email of that subId, any case) and the device (`deviceId` of 6 to 128 characters, name, system, app version), passes the device through `EnforceHwidForSubID` and stores an `app_sessions` row: sub id, email, the device's HWID hash and the SHA-256 of a 32-byte token. A device has one session; logging in again replaces it. Failures are limited per address and link by `internal/util/loginlimit` (5 in 5 minutes, then 15 minutes), the panel login's limiter, with the address from `internal/web/clientip`.
- `GET .../app/v1/profile` with `Authorization: Bearer` and `X-HWID` checks the token, the device, that the email still has that subId and that app access is on, and runs the HWID gate again, so a device trimmed off a lowered limit is refused. It answers the account (usage, expiry, device limit and count), the subscription's title, support link and announcement, the locations it uses, and one config per entry of `subLinks` rendered as for the subscription page (OpenVPN and L2TP included), each with its location and, when `link.ParseLink` can read it, the Xray outbound.
- `locations` are the places the app offers (name, ISO country code, city, IANA timezone, sort order); `location_inbounds` links each inbound to at most one of them, on any protocol and server, and `deleteInboundRow` drops a deleted inbound's link. A config's `locationId` is its inbound's location, absent when the inbound serves none. The profile names the locations without the panel's inbound ids. 0.16.0 turned each node-keyed row of the old `app_locations` into a location holding that server's inbounds. Removing a device (`DeleteClientHwid`, `ClearClientHwids`, the sub id going away) deletes its sessions.
- The routes hang under every subscription path group; `appEnable` (off by default) is read on every request, so switching it needs no restart.
- 0.18.0: `app_notices` (text, level `info` / `warning` / `critical`, time) is managed under `/panel/api/app/notices` and the profile carries the 50 newest, newest first; the app shows warning and critical as banners. The profile renders OpenVPN with the login even when the inbound sets `promptCredentials`, since the app is signed in and has no password to ask for.

### 4.19 Addons (groundwork, 0.12.0)

- An addon is a manifest (`addons` row): id, name, version, the permissions it needs, the events it hears, a webhook URL and a link to its own UI. Installing creates an admin-scope API token bound to the installing account and narrowed to those permissions (refused when the account lacks one), and a webhook for those events; the plaintext token and signing secret are returned once.
- The contract an addon programs against is the panel's API (`/panel/api/openapi.json`) and the webhook payload of 4.7, nothing addon-specific. Mirza Bot is meant to be the first.

### 4.18 Importers (built, 0.11.0)

- `internal/importer` reads Marzban, PasarGuard, Marzneshin, 3x-ui/x-ui and s-ui from their SQLite file (recognised by tables and columns, read-only), Hiddify from its JSON backup, Marzban/PasarGuard and Remnawave over their APIs, into one `User` shape with a credential per protocol.
- The panel holds a read source for 30 minutes as a preview; apply builds one client per user, attaches it to the inbounds picked for the protocols it has, and creates them through the normal client import (traffic carried over).
- `sub_aliases` maps old link paths to a sub id; `legacy_sub_secrets` holds Marzban/PasarGuard signing secrets. A subscription request for an unknown id is resolved through them before any handler runs; a `"token:"` alias is only reachable through a token that verifies and is not older than the user's revocation.

### 4.17 Certificate store (built, 0.11.0)

- `internal/certstore`: an ACME client on `golang.org/x/crypto/acme` with HTTP-01, TLS-ALPN-01 and DNS-01 solvers, and DNS providers for Cloudflare and ArvanCloud. `internal/certstore/acmetest` is a CA for tests that checks each challenge for real (an HTTP fetch, an `acme-tls/1` handshake, the TXT record).
- `certificates`: domains, challenge, provider token, account key and the PEMs (secrets never leave the API). Files under `<data dir>/certs/<name>/`; `CertRenewJob` every six hours renews in the last third of a certificate's life (at most 30 days), retries a failure after 12 hours. An IP address is ordered as an `ip` identifier under the `shortlived` profile (0.13.0).
- `manifold cert issue` (0.13.0) saves an HTTP-01 or DNS-01 entry, issues it synchronously (`CertificateService.IssueForPanel`) and points the panel at its files, for a panel whose pages cannot be reached.
- A renewal writes the files, asks Xray to restart when an inbound's stream settings name the folder, and copies the pair to the certificate's nodes through `Remote.PutCertificate` (`POST /certs/put`, in the node-sync scope). The panel's own listener reads its certificate through `GetCertificate` and loads the files again when they change.

### 4.16 Monitoring (built, 0.10.0)

- `GET /panel/api/metrics`: the Prometheus text format, written by `internal/metrics` (no client library: the panel only exposes gauges and counters it already holds). It is an API route, so a scoped token with `stats:read` scrapes it; per-user series only with `?users=1`.
- `GET /panel/api/server/summary` feeds the overview's business tiles from the same counts.
- `manifold/grafana/manifold.json`; a test reads every metric name its panels use and fails when the scrape no longer has it.

### 4.15 Scheduled backups (built, 0.10.0)

- `backup_targets` rows: kind, interval, retention, connection fields, write-only secrets. `BackupJob` (every 10 minutes) runs the due ones one at a time.
- Retention deletes only names the panel writes (`manifold-backup-<host>-<UTC stamp>.tar.gz[.enc]`), oldest first.
- Restore and inspect accept a `passphrase` form field; `manifold backup decrypt|restore` does the same on the command line.

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
| Roles, admins, resellers, ownership, ledger | Absent | NX | Built in 0.6.0 (4.6) |
| Plans | Absent | NX | Built in 0.6.0 |
| Audit log | Absent | NX | Built in 0.6.0 |
| Webhooks with outbox and replay | Absent (in-process bus only) | NX, Mirza sync | Built in 0.10.0 (4.7) |
| API tokens | Present, 4 scopes | NX | Add reseller scope |
| Two-factor login (TOTP) | Present | NX | Check recovery codes and session list |
| Single sign-on | LDAP only | NX | Add OIDC |
| Command palette, PWA | Present | NX | None |
| Backup and restore | Present (DB backup, Telegram delivery); full backup with certificates and a move check built in 0.5.0 (4.10) | NX | Scheduled, encrypted, with retention, to a directory, S3 or SSH: built in 0.10.0 (4.15) |
| First-run setup wizard | Not found | NX | Build |
| Panel self-update | Present | NX | Repoint at the fork's releases |
| Hosts and domain fronting | Present (`Host` model, hosts page) | NX | Compare in detail |
| Managed node-to-node tunnels with status view | Built in 0.5.0 (4.8, 4.9): kernel and BackPack tunnels, relays managed by API, state on the overview | NX, user | Groups of origins with failover and balance built in 0.12.0 (4.8); a test of every tunnel kind between two servers with a recommendation built in 0.22.0 (4.21); traffic limits per tunnel and a REALITY tunnel kind open |
| WARP, NordVPN, PIA outbound integration | Present | NX | None |
| Subscription page themes, usage CSV | Partial or unverified | NX | Themes and a theme catalogue built in 0.8.0 (4.12), usage history in 0.7.0 |
| Certificate store with ACME, wildcards, renewal | Absent | NX | Built in 0.11.0 (4.17) |
| Import from other panels, keeping old links | Absent | User | Built in 0.11.0 (4.18) |
| Prometheus metrics, Grafana | Absent | NX | Built in 0.10.0 (4.16) |
| Shop: customer portal, payments, agents, promotions | Absent | NX Shop addon | Mirza Bot covers this |
| Addon platform, licence system | Absent | NX | Addon groundwork built in 0.12.0 (4.19); no licence system |
| NaiveProxy, AnyTLS, ShadowTLS, Mieru, Snell, SSH, OpenConnect and similar | Absent (needs sing-box) | NX | Out of MVP |

## 6. Roadmap

| Phase | Scope | Done when |
|---|---|---|
| P0 Foundation | Private mirror; own README and guides; base project's pages, sponsor feature, links and self-updater removed; program, service, paths, interface and notification texts named Manifold; own installer, management command, systemd unit and release workflow; CI trimmed to one gate | Done 2026-11-14. Open items in 8.1 |
| P2 OpenVPN | Engine 4.4, inbound form, `.ovpn` in the client pages and the subscription page, traffic and limits, node support | Done 2026-11-14 (0.2.0). A real client connects, is billed, is held to its device limit and is cut off when disabled or out of quota, on this server. Open items in 8.2 |
| P1 WireGuard | Engine 4.3, inbound form, `.conf` and QR in the client pages, traffic and limits, node support, takeover of existing servers | Done 2026-11-14 (0.3.0). A real peer connects, is billed, loses its peer when disabled or out of quota, and an interface `wg-quick` already runs is taken over with its WGDashboard names and usage while its peer keeps connecting. Open items in 8.3 |
| P3 L2TP/IPsec | Engine 4.5, inbound form, the account's four values in the client pages, traffic and limits, node support, installer and unit | Done 2026-11-15 (0.4.0). A real L2TP client behind NAT connects through IPsec, is billed, is held to its device limit and is cut off when disabled or out of quota, under the installed service on Ubuntu 22.04, 24.04 and 26.04. No phone or Windows machine has connected yet. Open items in 8.4 |
| P6a Tunnels and moves | 4.8 to 4.10, and the OpenVPN profile options of 4.4 | Done 2026-11-16 (0.5.0). A real GRE link between two network namespaces carries a forwarded port from a third, masqueraded and with the user's address kept; a BackPack reverse tunnel runs the packaged binary at both ends and carries a forwarded port; a real OpenVPN client connects on RSA keys with tls-auth and an MTU; the installed service makes a full backup and restores it, on Ubuntu 22.04, 24.04 and 26.04. Open items in 8.5 |
| P4 Access model | Items in 4.6 | Done 2026-11-17 (0.6.0). A reseller creates users only within its limits and plans and reaches only its own, checked through the API by `internal/web/access_http_test.go`. Open items in 8.6 |
| P5 Events and API freeze | Items in 4.7; OpenAPI v1 for accounts, plans, resellers, usage | Events and webhooks done 2026-11-17 (0.10.0): a test receiver verifies the signature of each delivery, retries are scheduled and a delivery survives a restart in the outbox. The OpenAPI v1 freeze is still open. Open items in 8.10 |
| P6 NX-parity operations | Setup wizard, backup upgrades, OIDC, managed tunnels, subscription themes, usage CSV | Largely done 2026-11-17 (0.7.0 to 0.12.0): usage, speed limits, subscriptions and themes, nodes, shared ports, scheduled backups, metrics, certificates, importers, tunnel groups, addon groundwork. The setup wizard and OIDC are still open. Open items in 8.7 to 8.14 |
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

### 8.5 Open items after tunnels and full backups

- Fixed in 0.14.0: BackPack tunnels did not start under the installed service from 0.5.0 on. The service's bin folder is relative to its working directory and BackPack runs in its config's folder, so both paths it was given pointed inside `bin/tunnels`; the tests used absolute temporary paths. The relay smoke test now starts a BackPack end under the unit.
- No tunnel has run between two real servers: the GRE test uses network namespaces on one runner, and the BackPack test runs both ends on one host over loopback. Real paths (GRE through a provider's filter, BackPack transports through DPI) have to be seen.
- Since 0.22.0 the tunnel test's end-to-end runs carry TCP and UDP through GRE, GRETAP, IPIP and SIT, the reverse transports `tcpmux`, `ws` and `udp`, and the layer-3 `udp` and `quic` carriers dialled from either side, also under the installed service. IP6GRE, IP6GRETAP, the other reverse transports and the `pck`, `sni` and `xdi` carriers are covered by unit tests of their configuration only.
- A BackPack process dies with the panel, so a panel restart or update interrupts BackPack tunnels for a few seconds while kernel links stay up.
- Keeping each user's address is built for kernel links only; BackPack reverse tunnels offer the PROXY protocol instead, which the inbounds behind them must accept.
- BackPack's `pck` and `sni` carriers need `AF_PACKET`, which the unit now allows; they are not exercised by a test.
- A relay that is itself behind NAT with a mapped address must have its public address entered in the tunnel; the panel detects only its own public address.
- The full backup holds certificate files of at most 1 MiB found under certificate and key fields; a path written elsewhere (an Xray template outside those keys, a script) is not carried.
- A restore stops at the first error of the import itself; the files it wrote before stay written.
- Since 0.14.0 Manifold Relay's package leaves Xray, mtg and the geo files out; the program itself still holds their code (see 8.16).

### 8.7 Open items after limits and usage (0.7.0)

- Live connections are not listed per connection: Xray reports online addresses per user, not each connection, and the engines report sessions. A sessions view (OpenVPN and L2TP with disconnect, WireGuard handshakes, Xray addresses) is the planned answer.
- The speed limit does not reach MTProto (its own sidecar) or traffic a routing script decides.
- Usage history rows are kept per email; a user deleted and recreated under the same name within a day of the daily prune inherits the old rows.

### 8.9 Open items after nodes (0.9.0)

- SSH install downloads the installer from GitHub on the server; a server that cannot reach GitHub needs the package copied by hand (`install.sh --package`).
- Node templates (one configuration applied to many nodes) are not built; a node's inbounds are created on it from the master as before.
- Live connections per node are still not listed (see 8.7).

### 8.23 Open items after the user limit (0.23.0)

- No end-to-end run holds a single-user account across OpenVPN, L2TP and WireGuard on a master and a node. Each engine's own tests, the seat sync job's test and the node contract cell cover the parts; `make node-e2e` does not start the VPN engines.
- The client window has no list of an account's seats with a button to free one, and no webhook says a connection was refused for a full account.
- One WireGuard key per account means one device per location's WireGuard at a time; a plan for two people who both use WireGuard at one location needs two accounts.
- A node on an older version has no `POST /seats/sync`: it counts its own seats alone and the master's plan never reaches it. A relay needs 0.23.0 to forward ESP.
- DT Connect's quick close was not tried on a Windows client with OpenVPN 2.5 and without DCO.

### 8.22 Open items after the tunnel test (0.22.0)

- No test has crossed a real filtered path: the end-to-end script runs two panels in network namespaces on one host. BackPack's `pck` and `sni` carriers choose the interface of the default route and cannot run there.
- BackPack's `kcp` and `xdi` reverse transports carry about 10 Mbps between namespaces with the panel's settings; whether that is BackPack's own pacing or a setting the panel does not write is not known.
- UDP rates are measured up to what each round's cap allows at a 250 ms step (about 270 Mbps for the quick test's full round); a faster path reads as that.
- A test does not apply what it recommends: the form opens with it, and the forwarded ports and the relay's end are still the admin's.
- The test uses one server's detected address unless the admin enters one; a server whose route to the other leaves by another address (policy routing, several uplinks) needs it entered.

### 8.21 Changes in 0.21.0

- The subscription path serves `<link>/openvpn` and `<link>/wireguard`: one block per app location in the locations' order (inbounds in no location last), the OpenVPN login once, a QR code per WireGuard config and a ZIP of all of them. Files are named after the location and fetched as `/openvpn/<n>`, `/wireguard/<n>` and `/wireguard/all.zip`; the pages answer exactly when the subscription does (subscriptionBody off, as on the info page). For customers without the app, iOS ones included.
- `GET /panel/api/clients/subEntries/:subId` is `subLinks` with each link's inbound and app location, so a sales bot sends one location's file and links the pages for the rest.
- Open: the pages are not themed by the subscription page theme; they carry their own small style.

### 8.20 Changes in 0.20.0

- The installer asks for the database on a first install (`--db`, `--db-dsn`); `manifold` → 27 and `manifold db` show it and move the panel between SQLite and PostgreSQL. `migrate-db --to-sqlite` copies a PostgreSQL panel into SQLite. The status lines name the database, and `doctor` checks that its PostgreSQL server answers.
- A PostgreSQL backup restores through psql in one transaction and merges duplicate device rows; the restore now needs `psql` beside `pg_restore`. The panel runs the newest installed version of `pg_dump`, `pg_restore` and `psql` itself: Debian's wrapper chose the newest `pg_dump` but the local cluster's `pg_restore`, which could not read the panel's own backups, on Ubuntu 22.04 and 24.04 with their distribution's server beside PostgreSQL 18. The script leaves out settings an older server lacks, as `pg_restore` does when it talks to the server itself. Messages that named 3x-ui's `x-ui` command name Manifold's.
- Open: a local PostgreSQL server is set up on Debian, Ubuntu and the RHEL family from the PostgreSQL project's repository and checked in the release job on Ubuntu only; openSUSE and Arch get their distribution's server untested.

### 8.19 Changes in 0.17.0

- The PasarGuard comparison was carried out in Manifold's own model: role scopes, bounds, plans and inbounds, per-account overrides, account switch-off and role policies, token request caps, a node proxy and linked inbounds across nodes. Manifold keeps its full-panel nodes; PasarGuard's per-resource permission catalogue was not copied, since tiers and roles already cover it.
- Open: an edit carried to a linked copy that the copy's node refuses (a port taken there, say) is reported and leaves that copy as it was; unlink it to edit it alone. Linked copies share one remark, so a subscription shows the same name for each; locations tell them apart in the app.

### 8.18 Changes in 0.16.0

- OpenVPN and L2TP read a client's `userLimit`; `limitIp` is left to Xray and kernel AmneziaWG. The `ClientUserLimitSplit` upgrade copied the old cap of every tunnel account, in its record and every inbound's copy, and of each plan reaching a tunnel inbound.
- The subscription page shows, under each service, the account and how many may use it at once, with the password for OpenVPN and L2TP; `?format=info` leaves these out.
- Links without a subscription or panel domain use the public IPv4 the panel looks up at start and every 10 minutes, then the hostname.
- The admin and node models of PasarGuard were compared with Manifold's; the proposal is in the project notes and waits on a decision.

### 8.17 Open items after the app API (0.15.0)

- Sessions do not expire on their own; they end on logout, device removal, a new link or name, or app access being switched off.
- AmneziaWG, TUIC, MTProto and MASQUE entries reach the app as links without an outbound; the app runs what Xray, OpenVPN and the system's L2TP client can.
- Minimum and latest app versions are not announced by the panel; the app reads its own releases.

### 8.16 Open items after Manifold Relay (0.14.0)

- A relay's version is learnt only when the managing panel talks to it (saving a tunnel, a resync, opening the Relays window); nothing polls relays in the background.
- The relay package still holds the whole panel program; only Xray, mtg and the geo files are left out. A build without the Xray library and the inbound code would be smaller still.
- The login page of a relay still says Manifold, so as not to tell an outsider the server is a relay.

### 8.15 Open items after the installer and menu (0.13.0)

- IP certificates are HTTP-01 only: the TLS-ALPN-01 solver does not answer for an IP address (RFC 8738's reverse-DNS SNI).
- The firewall menu manages ufw, so Debian and Ubuntu; elsewhere the admin uses the distribution's own firewall.
- The installer's certificate step needs port 80 free while it runs; it does not offer DNS-01, which the menu and `manifold cert issue -dns` do.

### 8.14 Open items after the addon groundwork (0.12.0)

- The panel does not run addons (no sidecar process or container management) or list a catalogue of them; the admin runs the addon and pastes its manifest.
- An addon's UI is a link, not embedded in the panel.

### 8.13 Open items after tunnel groups (0.12.0)

- An Xray-based tunnel kind (VLESS + REALITY between relay and origin, so the link looks like TLS to a real site) is not built; groups balance kernel and layer-3 tunnels.
- Groups are made on the relay's own panel; a managed relay does not receive them from the panel that manages its tunnels.
- Balance is round-robin per connection; weights and per-user stickiness are not offered.

### 8.12 Open items after the importers (0.11.0)

- Marzban admins, PasarGuard groups and Hiddify owners are not mapped to resellers; an import can give all its users to one owner.
- Remnawave's squads and Marzban's per-user inbound exclusions are not carried; a user lands on every inbound picked for its protocols.
- Users on hold or with a first-use start are imported with their time starting now (each is listed in the preview's warnings).

### 8.11 Open items after the certificate store (0.11.0)

- DNS providers are Cloudflare and ArvanCloud, checked against their documented APIs with recorded calls, not a live account.
- HTTP-01 and TLS-ALPN-01 bind the port themselves; they do not yet share it with the panel's shared-port router (4.14) or a web server already on 80.
- Deleting a certificate leaves the copies on nodes in place.

### 8.10 Open items after webhooks (0.10.0)

- Soroush, Rubika and SMS channels are not built; Bale and ntfy are. A per-admin Telegram channel still goes through the base code's bot.
- Changes a node makes on its own (its own cron) reach the master's webhooks only through the user watcher, not as `user.updated`.

### 8.8 Open items after subscriptions (0.8.0)

- A theme is one for the whole panel; NX's brandings (a theme per reseller or per host) are not built.
- The theme catalogue lists one theme beyond the two built in; more are added to `sub-themes/` in this repository.
- sing-box profiles are checked against the format in tests, not yet by running a sing-box binary in CI.

### 8.6 Open items after the access model

- The token rate limit counts in the panel's memory, so it starts again when the panel restarts.
- Translations of the new pages exist in English, Persian, Russian and Simplified Chinese; the other ten locales show English.
- A reseller's users stay on the nodes they were on; nodes are not yet limited per reseller beyond the allowed inbounds.

## 9. Assumptions and open decisions

Assumptions made where no answer was given:

- **NX Panel**: features are re-implemented from its public docs; nothing of its code or UI is reused.
- **WGDashboard**: its Python codebase is not forked; its WireGuard mechanism is reproduced in Go (4.3), and an existing server is taken over through the import in the Inbounds page.
- **Phase one** is the panel only, starting with the protocol engines.

Open decisions:

1. **"Same UI"**: assumed to mean 3x-ui's React and Ant Design interface, extended consistently for the new protocols and pages. NX's interface cannot be copied.
2. **sing-box-only protocols** from NX (constraint 3): excluded from the MVP unless required.
3. **IKEv2**: whether it should be offered next to L2TP from the same strongSwan. Android from version 12 on has no L2TP in its own VPN settings and does have IKEv2, so today such a phone can only be served by another protocol's app.

# Using Manifold

This guide covers what the panel does today. Sections for L2TP and resellers are added here as each phase lands.

## Sign in

Open the address the installer printed and sign in with the login it showed. `manifold settings` prints the port and URL path again, and `manifold login` sets a new login. Then, under Panel Settings:

1. Change the username and password.
2. Turn on two-factor login.
3. Set a certificate, or put the panel behind a reverse proxy with TLS, before using it over the public internet.

## The pages

| Page | What it is for |
|---|---|
| Overview | State of the server and of the proxy core |
| Inbounds | The entry points clients connect to: a protocol on a port, with its transport and security |
| Clients | Customer accounts: traffic quota, expiry date, and the inbounds each one may use |
| Groups | Sets of clients, for acting on many accounts at once |
| Nodes | Other servers running Manifold that this panel controls |
| Hosts | The addresses and domains that links and subscriptions send clients to |
| Tunnels | Links between this server and a relay, and the ports the relay forwards here |
| Xray Configs | Core settings, routing rules and outbounds |
| Panel Settings | Panel address and certificate, login, subscription server, notifications, API tokens |
| API Docs | Reference for the panel's HTTP API |

## Selling an account: the basic flow

1. **Create an inbound** on the Inbounds page: choose the protocol, the port, and the transport and security settings. Open that port in the firewall.
2. **Create a client** on the Clients page: give it a traffic quota and an expiry date, and choose the inbounds it may use. One client can use several inbounds; its quota and expiry are shared across all of them.
3. **Hand over the access**: either the share link or QR code of one inbound, or the client's subscription URL, which carries every inbound the client may use and follows later changes.
4. **Renew** by raising the quota, moving the expiry date or resetting the used traffic.

A client whose quota is used up or whose expiry date has passed stops working on all of its inbounds until it is renewed.

## Limits on a client

- **Speed limit** caps a client in Mbps, in each direction, on every engine: Xray (VLESS, VMess, Trojan, Shadowsocks, Hysteria and the embedded AmneziaWG and TUIC that ride it), OpenVPN, L2TP and WireGuard. It applies on each server separately: 20 Mbps lets the client use 20 Mbps on every server it is connected to. All of a client's connections on one server share the one budget, and a change reaches connections that are already open within a few seconds.
- **IP limit** is the number of simultaneous addresses on Xray inbounds, enforced by fail2ban. **User limit** is the number of connections an account may hold at once on OpenVPN and L2TP: with 2, a third device is refused until one of the first two disconnects; it needs no fail2ban. The form shows each limit only when the user is on an inbound that enforces it. An upgrade copies the IP limit of every OpenVPN and L2TP user, and of every plan reaching one, to its user limit.
- Plans carry a speed limit too, copied onto every user created from them.
- **Usage** (the chart icon on a client) draws its traffic over the last 24 hours, 7 or 30 days, or a whole month in the panel's calendar, and splits it per server. **CSV** downloads both. Hours are kept for a month and days for 400 days, in the panel's time zone.
- **Traffic reset** has the shapes NX Panel offers: every hour or day, weekly on a weekday, monthly on a day, yearly on a month and day, or every N days counted from the last reset. Monthly and yearly cycles follow the Gregorian or the Persian calendar; a day the month lacks falls on its last day. A reset clears the used traffic only.
- **Customer details** in the client form keeps Telegram, email, phone, Bale, Soroush Plus and Rubika, which the search box finds and exports carry, plus any field of your own, which is only kept.
- With users ticked, **Adjust** also sets their speed, IP and user limits to one value (empty leaves them), **Download CSV** lists them with their subscription links, and **Download config pack** zips a folder per user with its subscription link, its share links, and the `.ovpn` and WireGuard `.conf` files apps open. **Download CSV of all users** and **Delete expired users…** (expired more than N days ago, 30 by default; not-started and auto-renewing users are kept) are in the ⋯ menu. A reseller exports only its own users, and every export is written to the audit log.

The speed limit needs the `nftables` package, which Ubuntu installs by default. It does not reach traffic a routing script decides, and Xray traffic that leaves through a WireGuard outbound such as WARP is limited on its way there, through a loopback hop the panel adds.

## Admins, resellers and plans

**Admins** in the sidebar lists everyone who signs in. Each account is on a tier:

- **Main admin** reaches everything, including admins, roles, API tokens, backups and the audit log. One main admin is the **owner**, marked with a crown; only the owner can hand the panel over (**Hand over ownership**).
- **Operator** runs the panel (users, inbounds, nodes, tunnels, settings) but not its administration.
- **Reseller** sees only the users it created, inside an allowance: traffic per cycle, a reset cycle (weekly, monthly or yearly from a day), an account expiry and a user limit. When it runs out its users are switched off until the next cycle or until you raise the allowance; users switched off by hand stay off. **May edit and delete users**, **Sell from plans only** and **Only chosen inbounds** narrow it further, and its profile title, support link and announcement replace the panel's in its customers' apps.

**Roles** makes custom roles. A role is based on a tier and keeps some of its permissions. It also sets:

- **Users it reaches** (operator and admin roles): every user, every user but changes only its own, or only its own. A reseller always reaches only its own.
- Bounds on what its accounts hand out to each user: the smallest and largest quota, the shortest and longest plan, the fewest and most devices. 0 leaves a side open; a largest value also refuses an unlimited user. They hold on create, on an edit that changes one of them, and on a bulk change. Users made before a bound existed stay editable.
- Users per account, for resellers.
- **Sell from plans only** and **Only chosen inbounds**, as on a reseller account. A reseller with lists of its own gets the stricter of the two.
- What happens when an account is switched off or out of allowance: switch its users off while the account is off; keep a reseller's users on when its allowance runs out (by default they go off); lock a reseller's sign-in until it is renewed.

An account can also be given permissions its role leaves out, or lose some it gives (**Permissions for this account** in the account form), always inside its tier. **Switched off** in the account form stops the account from signing in and stops its tokens; the panel owner cannot be switched off.

**Plans** holds the presets users are created from; pick one in the user form and its limits are filled in and applied by the server.

API tokens (**Settings → Security**) act as one account. Pick the account, optionally a lower tier and fewer permissions, the requests per minute (0, the default, is no limit) and how many days the token is valid. **Requests in total** caps the requests a token makes over its life; past it the token is refused, or deleted when **Delete it when used up** is on. The token list shows its use and when it was last used.

**Audit log** records sign-ins always, and every change while **Log admin changes** is on. Every account sets its own password and two-factor authentication from **My account** in the sidebar.

## OpenVPN

An OpenVPN inbound makes the panel run an OpenVPN server on that port. It needs the `openvpn` package on the server, which the installer installs.

**Create the inbound.** On the Inbounds page choose the protocol `openvpn` and a port, then on the protocol tab:

| Field | Meaning |
|---|---|
| Transport | UDP is faster. TCP gets through networks that block or slow down UDP. One inbound serves one of them; create two inbounds to offer both |
| Client subnet | The private IPv4 network connected clients get their addresses from, between /16 and /29. Give every OpenVPN inbound its own |
| DNS servers | Pushed to clients while they are connected |
| Ask for the login | Off: the profile carries the username and password, so the app connects without asking. On: the app asks for them. Turn it on for apps older than OpenVPN 2.6 |

The certificates and keys are created when you save and stay with the inbound. Open the port in the firewall for the transport you chose.

Further fields set what the server and every profile carry:

| Field | Meaning |
|---|---|
| MTU, MSS fix | `tun-mtu` and `mssfix` on the server and in every profile. Lower the MTU, for example to 1400, when large transfers stall on a route. Empty keeps OpenVPN's defaults |
| Data ciphers | Limits the ciphers a client may agree on; empty offers AES-256-GCM, AES-128-GCM and CHACHA20-POLY1305 |
| Split tunnel | Stops sending the default route, so only the VPN subnet goes through the tunnel |
| Control channel key | `tls-crypt` (the default), `tls-auth`, or none, for routers and old apps that support neither key file, such as older MikroTik |

Under **Certificates** the form shows the CA and the server certificate with their names, key type, expiry date and fingerprint, and offers:

- **Download CA**, for apps that import the CA on its own.
- **Issue new keys**: a new CA, server certificate and control channel key, with a key type (ECDSA P-256 or P-384, RSA 2048 or 4096 for routers that know no ECDSA), a lifetime and the two names. The server name is what every profile checks.
- **Use own keys**: paste a CA, a server certificate it signed and the server's private key in PEM.

New keys take effect when the inbound is saved, and every profile handed out before stops working: clients must download their profile again. The same holds after changing the MTU, the ciphers, the split tunnel or the control channel key.

Under **Client profile**, **Extra profile lines** are added to every profile of the inbound, one OpenVPN directive per line, for example `sndbuf 0`. Lines the panel writes itself (`remote`, `proto`, keys, the login) and script hooks are refused.

**Add clients** on the Clients page as for any other inbound. The client's email is the OpenVPN username and its password is the OpenVPN password, so an email used for OpenVPN may only hold letters, digits and `_ . @ : / = -`.

**Hand over the profile.** A client's information and QR windows show an "OpenVPN profile" block with copy and download; the downloaded `.ovpn` file imports into OpenVPN Connect and other OpenVPN apps. The client's subscription page offers the same file. The subscription URL that proxy apps import does not list OpenVPN, because those apps cannot read it.

**Limits.** Quota, expiry and disabling work as for every protocol and take effect on a connected client within seconds: its session is cut and the app is told access is denied. "User limit" on an OpenVPN client is the number of devices that may be connected at once; a further device is refused. The IP limit does not apply to OpenVPN.

**Coming from 0.2 or 0.3.** On Ubuntu 22.04, Debian 11 and other systems with a systemd older than 252, OpenVPN inbounds did not start under the installed service ("Cannot open TUN/TAP dev" in the log). Updating to 0.4 fixes it; nothing else has to be done.

Not supported yet: IPv6 inside the tunnel, logins by client certificate, and sending OpenVPN traffic through the routing rules and outbounds of Xray Configs.

## WireGuard

A WireGuard inbound is a WireGuard interface of the server itself: the standard kernel WireGuard that `wg-quick` and WGDashboard run. It needs the `wireguard-tools` package, which the installer installs.

**Create the inbound.** On the Inbounds page choose the protocol `wireguard` and a UDP port, then on the protocol tab:

| Field | Meaning |
|---|---|
| Secret Key, Public Key | The server's key pair. Every client config carries the public key, so a new key invalidates the configs already handed out |
| Interface name | The name of the interface and of its file in `/etc/wireguard`. Empty: `mfwg` followed by the inbound's number |
| Subnet, Subnet CIDR | The network new clients get their addresses from. The server takes its first address |
| Server address | Only for a server whose address is not the first of the subnet, or that has more than one, for example `10.0.0.254/24, fd42::1/64` |
| MTU, DNS | The interface's MTU, and the DNS servers written into client configs |
| Hooks | Commands `wg-quick` runs as root before and after the interface goes up or down, one per line; `%i` stands for the interface name |

On a system that confines `wg-quick` with AppArmor, as Ubuntu 26.04 does, a hook can only run what that profile allows: `ip`, `wg`, `nft`, `iptables` and a few basic tools. A hook that runs anything else fails, and the interface with it; `/etc/apparmor.d/local/wg-quick` is where such a system lets you allow more.

The panel writes `/etc/wireguard/<name>.conf`, brings the interface up and gives its clients internet access through NAT. The file belongs to the panel and is rewritten on every change, so change the inbound, not the file. A config in that directory that the panel did not write is never touched; an inbound cannot take its name until you import it.

**Add clients** on the Clients page as for any other inbound. Each gets its own key pair and tunnel address. The client's information and QR windows show the `.conf` and the QR code that the WireGuard apps import.

**Limits.** A client that is disabled, expired or out of quota is taken off the interface within seconds and put back when it is renewed. Usage is read from the interface's own counters. One WireGuard key connects from one place at a time, so "IP limit" adds nothing here.

**While the panel is stopped its WireGuard interfaces are down**, because nothing would count traffic or enforce a limit. They come back when the panel starts.

### Taking over a server that already runs

Inbounds → General Actions → **Import WireGuard servers** lists the interfaces in `/etc/wireguard` that are not inbounds yet. Importing one turns it into an inbound under the same name. Its port, server key, addresses, hooks and every peer's key and address stay as they are, so the configs your users already have keep working. The interface restarts once, and the previous config is kept beside the new one as `<name>.conf.before-manifold`.

If the server ran WGDashboard, its database is found on its own and adds what the config file does not hold:

- peer names, which become the clients' names
- usage so far
- the clients' private keys, so the panel can show their configs again
- restricted peers, which arrive as disabled clients

Stop WGDashboard before importing (`systemctl disable --now wg-dashboard`); two programs managing one interface undo each other's changes. A WGDashboard that keeps its data in PostgreSQL or MySQL is not read; the import then works from the config file alone.

A client imported without a private key keeps connecting with the config it has, and quota, expiry and disabling apply to it like to any other client. The panel only cannot show that config again.

**Coming from 0.2.** WireGuard inbounds made before 0.3 ran inside Xray. From 0.3 they run on a kernel interface with the same keys and addresses, so their clients need no new config.

Not supported yet: NAT for IPv6 inside the tunnel (IPv6 addresses are carried over; routing them is left to your hooks), importing the interfaces of a node, and sending WireGuard traffic through the routing rules and outbounds of Xray Configs.

## L2TP/IPsec

An L2TP inbound makes the panel run an L2TP server with IPsec in front of it. This is the VPN type that Windows, macOS, iOS and Android have built in, so a customer needs no app: only a server address, a username, a password and one shared key. It needs the `xl2tpd`, `ppp` and strongSwan packages on the server, which the installer installs.

**Create the inbound.** On the Inbounds page choose the protocol `l2tp`. The port is fixed: every L2TP client dials UDP port 1701 and none can be told another. For the same reason a server has one L2TP inbound; to offer more, add a node. On the protocol tab:

| Field | Meaning |
|---|---|
| Require IPsec | On: only sessions encrypted with IPsec are accepted. Off: sessions without encryption are accepted too, for the few devices that cannot do IPsec; devices that can still use it with the same key |
| IPsec key (PSK) | The shared key every device enters once, next to the account's username and password. Leave it empty and the panel generates one. A new key disconnects every device until it is entered there |
| Client subnet | The private IPv4 network connected devices get their addresses from, between /16 and /29 |
| DNS servers | Given to devices while they are connected, two at most |
| MTU | The largest packet inside the tunnel. 1280 fits through every network; raise it only if you know the path |

Open UDP ports 500, 4500 and 1701 in the firewall. A server behind NAT needs all three forwarded to it. Port 1701 can be open to everyone: with "Require IPsec" on, the panel itself drops whatever reaches it without having come through IPsec.

**Add clients** on the Clients page as for any other inbound. The client's email is the L2TP username and its password is the L2TP password, so an email used for L2TP may only hold letters, digits and `_ . @ : / = -`.

**Hand over the account.** There is no file or QR code a device imports. A client's information window shows an "L2TP account" block with the four values to type in, each with a copy button, and the QR window offers the same as text to send. On the device:

- **Windows**: Settings → Network & internet → VPN → Add VPN. VPN type "L2TP/IPsec with pre-shared key".
- **macOS**: System Settings → VPN → Add VPN Configuration → L2TP over IPSec. The key goes in "Shared Secret".
- **iOS**: Settings → General → VPN & Device Management → VPN → Add VPN Configuration → Type L2TP. The key goes in "Secret".
- **Android** up to version 11: Settings → Network → VPN → add, type "L2TP/IPSec PSK". Android 12 and later removed L2TP from the built-in VPN settings; such a device needs another protocol.

If the server itself sits behind NAT, as with cloud providers that map a public address onto a private one, Windows refuses it until the DWORD value `AssumeUDPEncapsulationContextOnSendRule` under `HKLM\SYSTEM\CurrentControlSet\Services\PolicyAgent` is set to 2 on the device and the device is restarted. This is a default of Windows, not a setting of the panel.

**Limits.** Quota, expiry and disabling work as for every protocol and take effect on a connected device within seconds: its session is cut and its next login is refused. "User limit" on an L2TP client is the number of devices that may be connected at once; a further device is refused. The IP limit does not apply to L2TP.

**Several devices behind one address.** Two devices that share one public address, such as two phones on the same home router, may not stay connected to the L2TP server at the same time: IPsec tells sessions apart by address, and theirs is the same. Offer such customers WireGuard or OpenVPN.

**IPsec belongs to the server, not to the panel.** The panel adds one connection, named `manifold-l2tp`, to the server's strongSwan and reloads it. A server that already runs IPsec through the older `ipsec` command is left as it is: the installer adds no second service, and an L2TP inbound that requires IPsec does not start there until strongSwan with `swanctl` is installed by hand.

**While the panel is stopped its L2TP server is down**, as for OpenVPN and WireGuard.

Not supported yet: IPv6 inside the tunnel, logins that use certificates instead of the shared key, a key per customer (every device of one inbound shares one), and sending L2TP traffic through the routing rules and outbounds of Xray Configs.

## Tunnels

A tunnel joins this server to a relay: users connect to the relay, and the relay forwards their ports through the tunnel to this server. Users' configs then name the relay, so this server can be moved, or its IP blocked and changed, without handing anyone a new config.

The two ends have names. The **client** end runs on the panel that serves the accounts, this one. The **server** end runs on the relay, which is best a server running **Manifold Relay** (see [installation](installation.md#manifold-relay)): the installer's second choice, with tunnels and nothing else.

| Kind | What it is |
|---|---|
| GRE, GRETAP, IP6GRE, IP6GRETAP, IPIP, SIT | Kernel links: fast, no encryption of their own (users' connections stay encrypted end to end). Each end needs the other's public IP, and the protocol (GRE is IP protocol 47) must pass the route |
| BackPack reverse | The client end dials the relay over one of twelve transports (TCP, TCP mux, stealth, WebSocket with or without TLS and mux, KCP, QUIC, UDP, PCK, XDI). Gets through filtered paths; the relay never needs this server's address |
| BackPack layer-3 | An encrypted link with an interface on each side, over UDP, QUIC, PCK, SNI or XDI. One side listens on the tunnel port and the other dials it |

BackPack tunnels run the BackPack program by Amin Mohammadi (AminMGMT), shipped unmodified in the package.

**Create a tunnel** on the Tunnels page with **Add tunnel**. A new tunnel gets a free tunnel subnet from 10.80.0.0/16, a port and a secret. Under **Forwarded ports**, list the ports the relay should expose and the port here each one goes to: `443` → same, or `2201` → `22`, or a range such as `10000-10009`.

**Set up the relay's end**, one of two ways:

1. **Pairing code.** The share button of a tunnel shows a code with the sides already swapped. On the relay's Tunnels page, **Import pairing code** and paste it. Importing a newer code of the same tunnel updates it in place.
2. **Managed relay.** The relay's installer prints its **relay token**, an API token of the **tunnel** scope (make a new one with `manifold relay-token`, or under Panel Settings → Security). Here, under **Relays**, add the relay's panel URL and that token, then choose the relay in the tunnel. This panel then creates, updates and deletes the relay's end itself. A tunnel-scoped token can do nothing on the relay but that. The Relays window asks each relay for its tunnels when it opens and shows whether it answers and which version it runs, marked when it differs from this panel's.

On the relay, `manifold tunnel import CODE` takes a pairing code from the command line, and `manifold tunnel list` shows each end's state measured on the spot.

**Users' addresses** (kernel links): with **Masquerade on the relay**, every user seems to come from the relay. **Keep each user's address** lets the panel see each user's own IP, which the device limit and the IP log need; the replies are then routed back through the tunnel by a routing table of its own.

**State.** The Tunnels page and the overview show each tunnel as up, degraded (more than 20 % loss or slow), down, connecting, disabled or error, with latency and loss measured through the tunnel and the traffic it carried. Kernel links stay up when the panel restarts or updates; `manifold uninstall` takes them down.

Open on the relay's firewall the forwarded ports and, for BackPack, the tunnel port.

**Several origins behind one relay.** On the relay, make one tunnel to each origin server (kernel links or BackPack layer 3), without forwards of their own, then under **Groups of origins** join them and give the ports there. **Failover** sends every connection to the first origin in the list that answers its probe and moves to the next one within about ten seconds when it stops answering; **balance** gives each new connection to the next origin that answers, and an existing connection stays where it started. When no origin answers, the ports go to all of them rather than nowhere. Users then reach every origin through the same relay address. Groups live on the relay, so for a managed relay open the relay's own panel to make one.

### Tunnel test

Before choosing a tunnel, the **Tunnel test** card of the Tunnels page tries them all between two servers and recommends the best. It works the same on Manifold and on Manifold Relay.

1. On one of the two servers, **Make a test link**. Say whether this server is the panel that serves the accounts or the relay. Check the panel address the other server will reach this one at, and this server's addresses if detection would get them wrong (behind NAT, or with several IPs). The link is valid for 30 minutes and serves one test.
2. On the other server, **Run a test link**: paste it, choose a test and the tunnels to try, and start. Either server can make the link. The one that runs it must reach the other's panel.

| Test | What it does |
|---|---|
| Quick | Screens every tunnel, then measures the best three in full; no MTU tuning |
| Standard | Screens every tunnel, measures the best five in full, and tries the two best that have an MTU at 1400, 1360 and 1280 |
| Thorough | Longer phases and more streams: the best eight in full, the MTU of the best three |

The window shows about how long each test takes and the most data it can move between the two servers. A slow path moves less.

Every tunnel kind is built in turn on both servers: the six kernel links, the twelve BackPack reverse transports, and the five BackPack layer-3 carriers with either server dialling. Each is measured the way users' traffic crosses it, from the relay to the origin:

- **TCP:** connection time, latency, jitter and loss, one stream each way, and several streams each way at once with pings beside them to show queueing under load.
- **UDP:** latency and loss, and the rate it holds each way. A tunnel that carries only UDP, such as BackPack's `udp` transport, is shown as UDP only instead of failed.

A tunnel the servers cannot run is skipped with the reason, such as a kernel without IPIP, BackPack missing, or a kind that needs IPv6 on both servers. The live view shows the stage, the tunnel under way and the data moved. Both servers show the same progress, and **Cancel** on either stops it on both.

**The result** names three tunnels:

- the **recommended tunnel**, best for every inbound because it carries TCP and UDP well;
- the best for **TCP inbounds** (VLESS, VMess, Trojan, OpenVPN over TCP);
- the best for **UDP inbounds** (WireGuard, L2TP, OpenVPN over UDP, Hysteria).

Each comes with the settings the test found best: the transport or carrier, which server dials, and an MTU when a smaller one carried clearly more. On the panel that serves the accounts, **Create this tunnel** opens the tunnel form filled with them. Add the ports to forward, save, and set up the relay's end as usual. The full table lists every tunnel with its TCP and UDP rates, latency, loss and score, and the reason behind every skip or failure. **History** keeps each test on both servers.

While a test runs it uses 80 ports on both servers, TCP and UDP, from the first one the link names, and GRE, IPIP and SIT. It opens them in the firewall (also behind ufw) for the other server's address only, and takes everything down when the test ends, is cancelled, or the other server stops answering for 90 seconds. The link's credential reaches nothing on the server but the test, and stops working when the test ends.


## Moving from another panel

**Import** (sidebar) brings users over from another panel:

| Panel | What to give it | Old links |
|---|---|---|
| Marzban | `/var/lib/marzban/db.sqlite3`, or its address and admin login (any database) | `/sub/<token>` and `/sub/<token>/<client>`, checked against its secret |
| PasarGuard | `/var/lib/pasarguard/db.sqlite3`, or its address and admin login | the same as Marzban |
| Marzneshin | `/var/lib/marzneshin/db.sqlite3` | `/sub/<username>/<key>` |
| 3x-ui, x-ui | `/etc/x-ui/x-ui.db` | `/sub/<subId>` (the sub id is kept) |
| s-ui | `/usr/local/s-ui/db/s-ui.db` | `/sub/<name>` |
| Hiddify | the JSON backup (Settings → Backup) | the user's UUID as the sub id |
| Remnawave | its address and an API token | the short UUID as the sub id |

The preview shows how many users there are, which protocols they have and who is already on this panel (those are skipped). Then pick, for each protocol, the inbounds its users go to; a user keeps its UUID (VLESS/VMess) or password (Trojan, Shadowsocks, Hysteria 2), quota, usage, expiry, note and reset cycle. Users with none of the picked protocols can go to fallback inbounds with new credentials.

To keep old links working, leave **Keep the old subscription links working** on, point the old subscription domain at this server and give the subscription server the old path. For a Marzban or PasarGuard read over the API, paste the old panel's subscription secret (`SELECT secret_key FROM jwt;`) so links it handed out at any time are recognised; without it only each user's current link is. A token from before the old panel revoked a user's links stays refused.

## Moving to another server

A **full backup** (Overview → Backup → Full backup for a move) holds everything the move needs in one `.tar.gz`: the database with every client, inbound and tunnel, the certificates and keys the settings and inbounds point at, and this server's public address.

On the new server, install Manifold, then **Restore a full backup**. Before changing anything, the panel shows what the move does:

- For every address users' configs carry, whether it still leads to the new server. A **domain** keeps working once its A record points at the new IP; the check says whether it already does. An address of a **relay** keeps working as it is. The old server's **bare IP** stops working unless that IP is moved to the new server: those users need new configs, or keep the IP.
- For every tunnel, what its other end needs: a managed relay is sent the new address automatically after the restore; a tunnel paired by code needs its new pairing code imported on the relay; a BackPack reverse tunnel whose relay is dialled by this server needs nothing.
- Ports held by another program, missing programs (openvpn, wireguard-tools, xl2tpd) and a backup from a newer panel, any of which stops the restore until fixed.

The restore keeps this server's panel port, path and certificate unless told otherwise, so the panel stays where you reach it; after it, sign in with the old server's login. Inbounds that listened on an address the new server lacks listen on every address. Optionally the old IP is replaced by the new one wherever the panel stores it (hosts, external proxies, share addresses, subscription URL); users then need their configs again.

Every account keeps its keys, UUIDs, passwords and certificates, so **what decides whether a user's config keeps working is only the address in it**. Point users at a domain or a relay, and a move costs them nothing.

## Subscriptions

**Settings → Subscription → Subscription domains** lists the hosts links are built on, one per line. New links use the first; every host on the list keeps answering, so a domain that gets blocked is replaced by putting a new one on top while users' apps refresh onto it. `*.sub.example.com` gives every user a host of its own, such as `k4m2xr8qv.sub.example.com`, so one blocked name costs one customer; it needs a wildcard DNS record and a wildcard certificate. A reseller's **Subscription domain** (its account form) puts its customers' links on its own domain. Behind a CDN, a subscription URI such as `https://*.cdn.example.com/sub/` names each user the same way.

**sing-box profile** (Settings → Subscription, on by default at `/singbox/`) serves the same entries as a full sing-box profile for SFA, SFI, Hiddify and Karing: a `proxy` selector, an `auto` URL test, and WireGuard as an endpoint. **Base profile** takes a profile of your own whose DNS, inbounds and route are kept as written. With **Serve sing-box apps on the main link** on, those apps get the profile from the ordinary link. The QR window shows the sing-box link on its own tab.

**Page theme** decides what a user sees when the subscription link is opened in a browser. **Aurora** (dark) and **Paper** (light) are built in; **Catalogue** lists more themes from the Manifold repository, installed with one click and updated when a newer version appears. Where GitHub is blocked, **Catalogue address** can point at a mirror of `sub-themes/catalog.json`. Every theme shows usage, expiry, the subscription QR code, buttons that add the subscription to Happ, v2rayNG, Hiddify, Streisand, sing-box and Clash, and each configuration with its own QR code, in English, Persian (right to left), Russian or Chinese, from `?lang=` or the browser. **Custom theme folder** still takes a folder of your own and overrides the picked theme; writing one is described in `sub-themes/README.md`.

**File pages.** Customers who use OpenVPN Connect or the WireGuard app instead of a VPN app get every location's file from two pages under their own subscription link: `<subscription link>/openvpn` and `<subscription link>/wireguard`, such as `https://sub.example.com:2096/sub/k4m2xr8qv/openvpn`. Each page lists one block per app location, in the locations' order and with the country's flag, then the inbounds that serve no location under their remark, each with a download button. The OpenVPN page shows the login once at the top with copy buttons; the WireGuard page adds each config's QR code for the phone app and a **Download all (ZIP)** button, which the WireGuard apps import in one go. Files are named after their location (`Germany.ovpn`, `Germany-2.ovpn` for a second server there, `Netherlands.conf`) and can be fetched directly: `/openvpn/<n>`, `/wireguard/<n>` (n counts from 1 in page order) and `/wireguard/all.zip`. The pages are in Persian, right to left, by default and in English for a browser that asks for it or with `?lang=en`; they answer exactly when the subscription link does.

The subscription server is on by default and listens on its own port (2096). Its address, path and the extra formats (JSON and Clash, both off by default) are set under Panel Settings. Open that port in the firewall before handing out subscription links.

## App

**Settings → App** lets customers sign in to a VPN app with their subscription link and their username. **App access** is off until it is switched on and saved. The app asks the subscription server, so the subscription server must be on and reachable; it answers on every subscription path, so whichever link the customer has works.

- The username is the client's name (email) in the panel, in any letter case. It is what a leaked link alone does not give: five wrong usernames from one address on one link block that address for 15 minutes. The link itself still works in other apps, as before.
- A device signed in to the app takes a slot of the client's **device limit** (HWID), the same limit the link's other apps share, so a customer on iOS who uses Streisand or Happ and a customer on Windows who uses the app are counted alike. App devices appear in the client's device list with their name and system; removing one there signs it out of the app. Changing the subscription link or the client's name signs out every device.
- The app shows the account's usage, expiry and devices, and every configuration of the subscription by server: Xray protocols (VLESS, VMess, Trojan, Shadowsocks, Hysteria 2, WireGuard) come with the Xray outbound to run, OpenVPN with its profile and L2TP with its account.
- **Locations** are the places the app offers, such as Germany: a name, a two-letter country code for the flag, a city and a timezone, which the app can give the device while it is connected. Link to a location every inbound that serves that place, on any protocol and any server; an inbound serves one location at most. **What serves where** lists every inbound with its location, and a location chosen there moves the inbound. An inbound in no location is still used by the app's quick connect. An upgrade turns each server that had a location into a location holding that server's inbounds.

**Settings → App notices** publishes news, changes and important announcements to every signed-in app. Write the text (in Persian: the app always shows it right to left, in a Persian font, whatever language the customer uses), pick a level and press **Publish**:

- **Info** goes to the app's bell only.
- **Warning** also shows an amber banner at the top of the app, and **Critical** a red one, until the customer closes it with its ✕. A notice that arrives while the app sits in the tray also comes as a system notification.

The app fetches its profile every 10 minutes and whenever the customer presses its refresh button, so a notice, a new location or a changed config reaches it within that time. **Delete** removes a notice; apps drop it on their next refresh. The app receives the 50 newest notices.

The app always receives OpenVPN profiles with the login in them, even when the inbound's **Ask for the login** option leaves it out for other apps: the customer signed in to the app and never types an OpenVPN password.

The API the app uses is under Subscription Server in the API documentation (`app/v1/login`, `profile`, `logout`).

## Several servers

Install Manifold on each additional server, create an API token there, and add the server on the Nodes page of the main panel. The main panel then sends inbounds and clients to the node and collects its traffic figures, so one account and one quota cover every server.

**Install over SSH** (Nodes page) does both steps for you. Give the server's address, the port its panel should listen on, and an SSH user (root, or one with passwordless sudo) with a password or private key. **Read host key** shows the server's SSH fingerprint before anything is sent; compare it with `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` on the server and accept it. The panel then logs in, runs the installer, gives the node a self-signed certificate and pins it from the installer's own output, mints a node-admin token, and adds the node. The log streams in the window. The password or key is used for that install only and never stored; the server needs to reach GitHub to download the installer.

Update each node with `manifold update` on that node, or with **Update over SSH** in the node's menu, which runs the same updater over SSH. The update action on the Nodes page is refused by every node: a panel never downloads or runs an updater itself.

**Reach through a proxy** (node form) connects the panel to the node through an http, https or socks5 proxy, such as `socks5://127.0.0.1:1080`, instead of directly. It replaces **Connection outbound**, which goes through one of the panel's own Xray outbounds.

**Clone** in an inbound's menu with **Keep the copies in step** on puts a copy of the inbound on each node you pick, with the same port, settings and users. From then on an edit to any copy reaches all of them, and a user put on or taken off one copy is put on or taken off all, as long as the account may use that copy. Each copy keeps its own traffic, its own switch and its node's address. The Node column marks linked copies; **Unlink from its copies** lets one copy go its own way.

**Traffic multiplier** (node form) counts users' traffic on that node against their quota times the factor: 2 for an expensive route, 0.5 for a cheap one. Usage history keeps the real bytes. **Periodic usage limit** caps the node's own traffic per week, month or year, in either calendar: at the limit the panel switches the node's inbounds off and the list says **Stopped by usage limit**; when the next period starts, or the limit is raised, exactly those inbounds are switched on again. An inbound you had switched off yourself stays off.

**Stats** on a node shows its health over an hour to a whole month: an availability bar (green answered, red not reporting, grey not watched while the panel was off), uptime counted only over watched time, CPU and memory, and each user's traffic through that node, with CSV. Samples are taken every five minutes and kept 90 days. **Logs** reads the node panel's own log at the level you choose.

## Shared ports

**Shared ports** (sidebar) lets one TCP port, usually 443, serve many things. Each connection is handed, untouched, to the target that its TLS server name (SNI) or HTTP `Host` names, so a REALITY inbound, a TLS inbound, the panel and the subscription server can all answer on 443.

1. Move each service to a free local port: an inbound to `127.0.0.1:8443`, the panel to its own port as it is.
2. Add a shared port on 443 with one route per service: its host names (for REALITY, the server name it imitates; `*.example.com` covers one label under the domain) and its target, such as `127.0.0.1:8443`.
3. Optionally send everything else to a web site, or leave it closed.

Routes are tried from the top; a route can also require an ALPN such as `h2`. A target sees the shared port as its client unless the route sends a PROXY header; then turn on **acceptProxyProtocol** in that inbound's transport so the device limit and the IP log see the real address. A shared port cannot take a port a local TCP inbound, the panel or the subscription server holds, and an inbound cannot take a shared port's. Shared ports run on this server only.

## Command line

The `manifold` command manages the service and the panel settings, and works while the panel's pages are down. On its own it opens a menu ([Installation](installation.md#managing-the-service)); `manifold help` lists the commands.

| Command | Effect |
|---|---|
| `manifold settings` | Print the port, the URL path and whether the login is still a default one |
| `manifold login [NAME PASS]` | Set the login; random when omitted, printed once |
| `manifold port N` | Set the panel port |
| `manifold setting -subPort N` | Set the subscription server's port |
| `manifold path /p/` | Set the URL path the panel is served under |
| `manifold setting -listenIP IP` | Bind the panel to one address |
| `manifold setting -resetTwoFactor` | Turn two-factor login off, for a lost device |
| `manifold cert issue -domain NAME` | Get a Let's Encrypt certificate over HTTP-01 for a domain or the server's IP, serve the panel with it and keep it renewed. `-dns cloudflare` or `-dns arvancloud` with `MANIFOLD_DNS_TOKEN` proves it over DNS instead |
| `manifold cert -webCert FILE -webCertKey FILE` | Set the panel's certificate and key; `manifold cert -reset` goes back to plain HTTP |
| `manifold doctor` | Check what keeps the panel from being reached |
| `manifold backup create [-out DIR] [-passphrase P]` | Write a full backup; `manifold backup restore -in FILE` puts one back with the panel stopped |
| `manifold setting -getApiToken` | Print a fresh API token and invalidate the previous one. `-tokenName` and `-tokenScope` (`admin`, `monitor`, `node-sync`, `node-admin`, `tunnel`) choose which |
| `manifold db` | Which database the panel runs on, whether its PostgreSQL server answers, and the client tools ([SQLite or PostgreSQL](installation.md#sqlite-or-postgresql)) |
| `manifold db postgres [DSN]` | Move the panel onto PostgreSQL: one installed here, or the server DSN names |
| `manifold db sqlite` | Move the panel back onto SQLite |
| `manifold db client [MAJOR]` | Install the PostgreSQL client tools the panel's backups and restores use |
| `manifold migrate-db --dsn DSN` | Copy the SQLite database into PostgreSQL, without switching the panel |
| `manifold migrate-db --to-sqlite FILE` | Copy the panel's PostgreSQL database into a new SQLite file, without switching the panel |
| `manifold migrate-db --dump FILE` | Write the SQLite database as a portable text dump |

`login`, `port`, `path` and `cert` restart the panel themselves. After `manifold setting ...`, run `manifold restart`.

## API

Everything the interface does goes through the HTTP API. The API Docs page lists every endpoint, and the OpenAPI description is served at `/panel/api/openapi.json`. Requests authenticate with a bearer token; give each integration its own token with the narrowest scope that works.

For a sales bot, `GET /panel/api/clients/subEntries/:subId` returns the same links as `subLinks/:subId`, in the same order, each with the inbound it comes from and that inbound's app location: `{"link", "inboundId", "protocol", "locationId", "location", "countryCode", "city", "sortOrder"}`. `locationId` is 0 for an inbound in no location and `inboundId` 0 for a client's external link. A bot can then send one location's OpenVPN or WireGuard file and point to the file pages above for the rest.

## Certificates

**Certificates** (sidebar) gets certificates from Let's Encrypt and renews them in the last third of their life (30 days before a 90-day one expires). Give the domains and how to prove them:

- **DNS-01**: the panel writes a TXT record through Cloudflare (an API token with Zone → DNS → Edit) or ArvanCloud (an API key). The only way to get a wildcard (`*.example.com`, which does not cover `example.com` itself; add both). Works behind a CDN and needs no free port.
- **HTTP-01**: the domain points at this server and port 80 is free while the order runs. The server's IP address works too: Let's Encrypt gives it a six-day certificate, which the panel renews about two days before it ends.
- **TLS-ALPN-01**: the same on port 443, so only when Xray is not on 443.

Each certificate is kept as `/etc/manifold/certs/<name>/fullchain.pem` and `privkey.pem`. In an inbound's TLS settings, **From the certificate store** fills both paths; for the panel's own HTTPS, put the same paths under Settings. After a renewal Xray restarts if an inbound reads the files, and the panel serves the new certificate within a minute without a restart. Pick nodes under **Copy to nodes** and every renewal is copied to the same path on them. **Upload** keeps a certificate you got elsewhere (the chain and its key, checked to belong together); it is not renewed. The **Test CA** switch uses Let's Encrypt staging, which has no rate limits but is not trusted by browsers.

Webhooks get `cert.issued`, `cert.failed`, and `cert.expiring` (once a day, from two weeks ahead, or the last sixth of a short certificate's life, for one that will not renew by itself).

## Webhooks

**Webhooks** (sidebar) sends events out as they happen. A webhook is your own URL, an [ntfy](https://ntfy.sh) topic (push notifications on a phone) or a Bale chat (a bot token and the chat id; the bot must be in the chat). Pick the events it hears, or none for all of them. **Send a test** queues a `webhook.test` event; the history button shows every delivery with what was sent and what came back, and sends one again.

Events: `user.created`, `user.updated`, `user.deleted`, `users.bulk_changed`, `user.enabled`, `user.disabled`, `user.expired`, `user.expiring` (a day before), `user.limited`, `user.quota_warning` (80% used), `user.first_connected`, `user.traffic_reset`, `node.up`, `node.down`, `node.usage_stopped`, `node.usage_resumed`, `outbound.up`, `outbound.down`, `xray.crash`, `cpu.high`, `memory.high`, `login.attempt`, `backup.succeeded`, `backup.failed`.

An HTTP webhook receives a POST with JSON `{"id", "type", "time", "source", "data"}`; for user events `data` holds the email, sub id, owner, traffic, quota and expiry. Check the signature before trusting it:

```python
import hmac, hashlib
expected = "sha256=" + hmac.new(secret.encode(), (timestamp + ".").encode() + body, hashlib.sha256).hexdigest()
ok = hmac.compare_digest(expected, request.headers["X-Manifold-Signature"])
```

`timestamp` is the `X-Manifold-Timestamp` header; reject old ones. `X-Manifold-Delivery` is the event id, the same on every retry. A new HTTP webhook gets a secret if you leave it empty, shown once. Without a 2xx answer a delivery is retried after 30 seconds, then up to 8 hours apart, about a day in all.

A reseller sees only its own webhooks, hears only about its own users, and can only send to public addresses.

## Addons

**Addons** (sidebar) installs programs that run next to the panel, such as a sales bot, from their manifest (`addon.json`):

```json
{
  "id": "my-bot",
  "name": "My bot",
  "version": "1.0.0",
  "permissions": ["users:read", "users:write"],
  "events": ["user.created", "user.limited", "user.expired"],
  "webhook": "https://bot.example.com/manifold",
  "panelUrl": "https://bot.example.com/admin"
}
```

Installing gives the addon an API token that acts as your account, limited to the permissions it lists and never more than you have, and a signed webhook for the events it lists (see Webhooks for the signature). Both are shown once; put them into the addon's settings. Installing the same id again updates its permissions and events and keeps the token. The switch turns the token and the webhook off together; uninstalling removes them, after which the addon can do nothing on the panel.

## Monitoring

The overview opens with the business side: active users, who is online, who expires within a day, who is out of traffic, today's traffic and how many nodes answer. A failing backup target or webhook deliveries given up show as a warning under the tiles.

For Prometheus, create an API token (Settings → API tokens) with `stats:read` and scrape `/panel/api/metrics` with it:

```yaml
scrape_configs:
  - job_name: manifold
    scheme: https
    metrics_path: /<web base path>/panel/api/metrics
    authorization:
      credentials: <token>
    static_configs:
      - targets: ["panel.example.com:2053"]
```

It carries this server (CPU, memory, disk, load, network, connections), Xray, users by state, users online, today's traffic, each inbound's traffic, each node (up, latency, CPU, memory), webhook deliveries waiting or given up, and each backup target's last run. `?users=1` adds a series per user; leave it off on a panel with many users.

`manifold/grafana/manifold.json` in this repository is a Grafana dashboard for it: Dashboards → New → Import, pick the Prometheus data source.

## Backup

**Backups** (sidebar) makes full backups on a schedule. A target is this server (`/etc/manifold/backups` by default), an S3-compatible bucket (AWS, Cloudflare R2, ArvanCloud, MinIO; give the endpoint without the bucket) or another server over SSH or SFTP (read and accept its host key first). Each target has its own interval in hours (0 for by hand only) and how many of the newest backups to keep; older ones there are deleted, and files the panel did not write are never touched. **Run now** backs up at once, and the folder button lists what the target holds.

With a **passphrase** every backup to that target is sealed with AES-256-GCM and named `….tar.gz.enc`. Restoring one under Overview → Backup asks for it. Without the passphrase nobody can restore it, so keep it somewhere other than the server. From the command line, with the panel stopped:

```bash
manifold backup decrypt -in backup.tar.gz.enc -out backup.tar.gz -passphrase '…'
systemctl stop manifold
manifold backup restore -in backup.tar.gz.enc -passphrase '…'
systemctl start manifold
```

`MANIFOLD_BACKUP_PASSPHRASE` can carry the passphrase instead of `-passphrase`.

For a move to another server, use the full backup described above. Everything the panel knows is in `/etc/manifold/`. For a consistent copy, either stop the panel first or write a dump:

```bash
manifold migrate-db --dump /etc/manifold/backup.dump
```

Keep copies off the server.

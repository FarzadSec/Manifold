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

## Admins, resellers and plans

**Admins** in the sidebar lists everyone who signs in. Each account is on a tier:

- **Main admin** reaches everything, including admins, roles, API tokens, backups and the audit log. One main admin is the **owner**, marked with a crown; only the owner can hand the panel over (**Hand over ownership**).
- **Operator** runs the panel (users, inbounds, nodes, tunnels, settings) but not its administration.
- **Reseller** sees only the users it created, inside an allowance: traffic per cycle, a reset cycle (weekly, monthly or yearly from a day), an account expiry and a user limit. When it runs out its users are switched off until the next cycle or until you raise the allowance; users switched off by hand stay off. **May edit and delete users**, **Sell from plans only** and **Only chosen inbounds** narrow it further, and its profile title, support link and announcement replace the panel's in its customers' apps.

**Roles** makes custom roles: a tier, the permissions kept from it, and caps on users per account, devices per user and plan length. **Plans** holds the presets users are created from; pick one in the user form and its limits are filled in and applied by the server.

API tokens (**Settings → Security**) act as one account. Pick the account, optionally a lower tier and fewer permissions, the requests per minute and how many days the token is valid.

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

**Limits.** Quota, expiry and disabling work as for every protocol and take effect on a connected client within seconds: its session is cut and the app is told access is denied. "IP limit" on an OpenVPN client is the number of devices that may be connected at once; a further device is refused.

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

**Limits.** Quota, expiry and disabling work as for every protocol and take effect on a connected device within seconds: its session is cut and its next login is refused. "IP limit" on an L2TP client is the number of devices that may be connected at once; a further device is refused.

**Several devices behind one address.** Two devices that share one public address, such as two phones on the same home router, may not stay connected to the L2TP server at the same time: IPsec tells sessions apart by address, and theirs is the same. Offer such customers WireGuard or OpenVPN.

**IPsec belongs to the server, not to the panel.** The panel adds one connection, named `manifold-l2tp`, to the server's strongSwan and reloads it. A server that already runs IPsec through the older `ipsec` command is left as it is: the installer adds no second service, and an L2TP inbound that requires IPsec does not start there until strongSwan with `swanctl` is installed by hand.

**While the panel is stopped its L2TP server is down**, as for OpenVPN and WireGuard.

Not supported yet: IPv6 inside the tunnel, logins that use certificates instead of the shared key, a key per customer (every device of one inbound shares one), and sending L2TP traffic through the routing rules and outbounds of Xray Configs.

## Tunnels

A tunnel joins this server to a relay: users connect to the relay, and the relay forwards their ports through the tunnel to this server. Users' configs then name the relay, so this server can be moved, or its IP blocked and changed, without handing anyone a new config.

The two ends have names. The **client** end runs on the panel that serves the accounts, this one. The **server** end runs on the relay, which is best a second Manifold installed as the **tunnel edition** (see [installation](installation.md)): a panel with tunnels and nothing else.

| Kind | What it is |
|---|---|
| GRE, GRETAP, IP6GRE, IP6GRETAP, IPIP, SIT | Kernel links: fast, no encryption of their own (users' connections stay encrypted end to end). Each end needs the other's public IP, and the protocol (GRE is IP protocol 47) must pass the route |
| BackPack reverse | The client end dials the relay over one of twelve transports (TCP, TCP mux, stealth, WebSocket with or without TLS and mux, KCP, QUIC, UDP, PCK, XDI). Gets through filtered paths; the relay never needs this server's address |
| BackPack layer-3 | An encrypted link with an interface on each side, over UDP, QUIC, PCK, SNI or XDI. One side listens on the tunnel port and the other dials it |

BackPack tunnels run the BackPack program by Amin Mohammadi (AminMGMT), shipped unmodified in the package.

**Create a tunnel** on the Tunnels page with **Add tunnel**. A new tunnel gets a free tunnel subnet from 10.80.0.0/16, a port and a secret. Under **Forwarded ports**, list the ports the relay should expose and the port here each one goes to: `443` → same, or `2201` → `22`, or a range such as `10000-10009`.

**Set up the relay's end**, one of two ways:

1. **Pairing code.** The share button of a tunnel shows a code with the sides already swapped. On the relay's Tunnels page, **Import pairing code** and paste it. Importing a newer code of the same tunnel updates it in place.
2. **Managed relay.** On the relay, create an API token with the **tunnel** scope (Panel Settings → Security). Here, under **Relays**, add the relay's panel URL and that token, then choose the relay in the tunnel. This panel then creates, updates and deletes the relay's end itself. A tunnel-scoped token can do nothing on the relay but that.

**Users' addresses** (kernel links): with **Masquerade on the relay**, every user seems to come from the relay. **Keep each user's address** lets the panel see each user's own IP, which the device limit and the IP log need; the replies are then routed back through the tunnel by a routing table of its own.

**State.** The Tunnels page and the overview show each tunnel as up, degraded (more than 20 % loss or slow), down, connecting, disabled or error, with latency and loss measured through the tunnel and the traffic it carried. Kernel links stay up when the panel restarts or updates; `manifold uninstall` takes them down.

Open on the relay's firewall the forwarded ports and, for BackPack, the tunnel port.

## Moving to another server

A **full backup** (Overview → Backup → Full backup for a move) holds everything the move needs in one `.tar.gz`: the database with every client, inbound and tunnel, the certificates and keys the settings and inbounds point at, and this server's public address.

On the new server, install Manifold, then **Restore a full backup**. Before changing anything, the panel shows what the move does:

- For every address users' configs carry, whether it still leads to the new server. A **domain** keeps working once its A record points at the new IP; the check says whether it already does. An address of a **relay** keeps working as it is. The old server's **bare IP** stops working unless that IP is moved to the new server: those users need new configs, or keep the IP.
- For every tunnel, what its other end needs: a managed relay is sent the new address automatically after the restore; a tunnel paired by code needs its new pairing code imported on the relay; a BackPack reverse tunnel whose relay is dialled by this server needs nothing.
- Ports held by another program, missing programs (openvpn, wireguard-tools, xl2tpd) and a backup from a newer panel, any of which stops the restore until fixed.

The restore keeps this server's panel port, path and certificate unless told otherwise, so the panel stays where you reach it; after it, sign in with the old server's login. Inbounds that listened on an address the new server lacks listen on every address. Optionally the old IP is replaced by the new one wherever the panel stores it (hosts, external proxies, share addresses, subscription URL); users then need their configs again.

Every account keeps its keys, UUIDs, passwords and certificates, so **what decides whether a user's config keeps working is only the address in it**. Point users at a domain or a relay, and a move costs them nothing.

## Subscriptions

The subscription server is on by default and listens on its own port (2096). Its address, path and the extra formats (JSON and Clash, both off by default) are set under Panel Settings. Open that port in the firewall before handing out subscription links.

## Several servers

Install Manifold on each additional server, create an API token there, and add the server on the Nodes page of the main panel. The main panel then sends inbounds and clients to the node and collects its traffic figures, so one account and one quota cover every server.

Update each node with `manifold update` on that node. The update action on the Nodes page is refused by every node: a panel never downloads or runs an updater itself.

## Command line

The `manifold` command manages the service and the panel settings; `manifold help` lists everything.

| Command | Effect |
|---|---|
| `manifold settings` | Print the port, the URL path and whether the login is still a default one |
| `manifold login [NAME PASS]` | Set the login; random when omitted, printed once |
| `manifold port N` | Set the panel port |
| `manifold setting -subPort N` | Set the subscription server's port |
| `manifold path /p/` | Set the URL path the panel is served under |
| `manifold setting -listenIP IP` | Bind the panel to one address |
| `manifold setting -resetTwoFactor` | Turn two-factor login off, for a lost device |
| `manifold setting -webCert FILE -webCertKey FILE` | Set the panel's certificate and key |
| `manifold setting -getApiToken` | Print a fresh API token and invalidate the previous one. `-tokenName` and `-tokenScope` (`admin`, `monitor`, `node-sync`, `node-admin`, `tunnel`) choose which |
| `manifold migrate-db --dsn DSN` | Copy the SQLite database into PostgreSQL |
| `manifold migrate-db --dump FILE` | Write the SQLite database as a portable text dump |

`login`, `port` and `path` restart the panel themselves. After `manifold setting ...`, run `manifold restart`.

## API

Everything the interface does goes through the HTTP API. The API Docs page lists every endpoint, and the OpenAPI description is served at `/panel/api/openapi.json`. Requests authenticate with a bearer token; give each integration its own token with the narrowest scope that works.

## Backup

For a move to another server, use the full backup described above. Everything the panel knows is in `/etc/manifold/`. For a consistent copy, either stop the panel first or write a dump:

```bash
manifold migrate-db --dump /etc/manifold/backup.dump
```

Keep copies off the server.

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
| Xray Configs | Core settings, routing rules and outbounds |
| Panel Settings | Panel address and certificate, login, subscription server, notifications, API tokens |
| API Docs | Reference for the panel's HTTP API |

## Selling an account: the basic flow

1. **Create an inbound** on the Inbounds page: choose the protocol, the port, and the transport and security settings. Open that port in the firewall.
2. **Create a client** on the Clients page: give it a traffic quota and an expiry date, and choose the inbounds it may use. One client can use several inbounds; its quota and expiry are shared across all of them.
3. **Hand over the access**: either the share link or QR code of one inbound, or the client's subscription URL, which carries every inbound the client may use and follows later changes.
4. **Renew** by raising the quota, moving the expiry date or resetting the used traffic.

A client whose quota is used up or whose expiry date has passed stops working on all of its inbounds until it is renewed.

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

**Add clients** on the Clients page as for any other inbound. The client's email is the OpenVPN username and its password is the OpenVPN password, so an email used for OpenVPN may only hold letters, digits and `_ . @ : / = -`.

**Hand over the profile.** A client's information and QR windows show an "OpenVPN profile" block with copy and download; the downloaded `.ovpn` file imports into OpenVPN Connect and other OpenVPN apps. The client's subscription page offers the same file. The subscription URL that proxy apps import does not list OpenVPN, because those apps cannot read it.

**Limits.** Quota, expiry and disabling work as for every protocol and take effect on a connected client within seconds: its session is cut and the app is told access is denied. "IP limit" on an OpenVPN client is the number of devices that may be connected at once; a further device is refused.

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
| `manifold setting -getApiToken` | Print a fresh API token and invalidate the previous one. `-tokenName` and `-tokenScope` (`admin`, `monitor`, `node-sync`) choose which |
| `manifold migrate-db --dsn DSN` | Copy the SQLite database into PostgreSQL |
| `manifold migrate-db --dump FILE` | Write the SQLite database as a portable text dump |

`login`, `port` and `path` restart the panel themselves. After `manifold setting ...`, run `manifold restart`.

## API

Everything the interface does goes through the HTTP API. The API Docs page lists every endpoint, and the OpenAPI description is served at `/panel/api/openapi.json`. Requests authenticate with a bearer token; give each integration its own token with the narrowest scope that works.

## Backup

Everything the panel knows is in `/etc/manifold/`. For a consistent copy, either stop the panel first or write a dump:

```bash
manifold migrate-db --dump /etc/manifold/backup.dump
```

Keep copies off the server.

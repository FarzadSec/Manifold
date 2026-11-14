# Using Manifold

This guide covers what the panel does today. Pages for WireGuard, OpenVPN, L2TP and resellers are added here as each phase lands.

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

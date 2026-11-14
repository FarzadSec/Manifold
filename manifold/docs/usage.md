# Using Manifold

This guide covers what the panel does today. Pages for WireGuard, OpenVPN, L2TP and resellers are added here as each phase lands.

## Sign in

Open `http://SERVER-IP:2053/` and sign in with the login from [first start](installation.md#first-login). Then, under Panel Settings:

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

1. **Create an inbound** on the Inbounds page: choose the protocol, the port, and the transport and security settings. In Docker, publish that port (see [Ports](installation.md#ports)).
2. **Create a client** on the Clients page: give it a traffic quota and an expiry date, and choose the inbounds it may use. One client can use several inbounds; its quota and expiry are shared across all of them.
3. **Hand over the access**: either the share link or QR code of one inbound, or the client's subscription URL, which carries every inbound the client may use and follows later changes.
4. **Renew** by raising the quota, moving the expiry date or resetting the used traffic.

A client whose quota is used up or whose expiry date has passed stops working on all of its inbounds until it is renewed.

## Subscriptions

The subscription server is on by default and listens on its own port (2096). Its address, path and the extra formats (JSON and Clash, both off by default) are set under Panel Settings. In Docker the port has to be published before links work from outside.

## Several servers

Install Manifold on each additional server, create an API token there, and add the server on the Nodes page of the main panel. The main panel then sends inbounds and clients to the node and collects its traffic figures, so one account and one quota cover every server.

## Command line

Settings can be read and changed without the web interface:

```bash
docker compose exec manifold /app/x-ui setting -show
```

| Command | Effect |
|---|---|
| `setting -show` | Print the port, the URL path and whether the login is still a default one |
| `setting -username NAME -password PASS` | Set the login |
| `setting -port N` | Set the panel port |
| `setting -webBasePath /path/` | Set the URL path the panel is served under |
| `setting -listenIP IP` | Bind the panel to one address |
| `setting -resetTwoFactor` | Turn two-factor login off, for a lost device |
| `setting -webCert FILE -webCertKey FILE` | Set the panel's certificate and key |
| `setting -getApiToken` | Print a fresh API token and invalidate the previous one. `-tokenName` and `-tokenScope` (`admin`, `monitor`, `node-sync`) choose which |
| `migrate-db --dsn DSN` | Copy the SQLite database into PostgreSQL |
| `migrate-db --dump FILE` | Write the SQLite database as a portable text dump |

Restart the panel after a change: `docker compose restart manifold`.

## API

Everything the interface does goes through the HTTP API. The API Docs page lists every endpoint, and the OpenAPI description is served at `/panel/api/openapi.json`. Requests authenticate with a bearer token; give each integration its own token with the narrowest scope that works.

## Backup

Everything the panel knows is in `./db/` on the host. For a consistent copy, either stop the panel first or write a dump:

```bash
docker compose exec manifold /app/x-ui migrate-db --dump /etc/x-ui/backup.dump
```

The dump appears as `./db/backup.dump`. Keep copies off the server.

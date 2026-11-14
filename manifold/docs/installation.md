# Installing Manifold

Manifold installs from a release package as a systemd service. The server downloads a ready-made package; nothing is compiled on it.

## Requirements

- A Linux server with systemd, on x86-64 or ARM64
- Root access
- An open TCP port for the panel and one for every inbound you create

## Install

As root on the server:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/FarzadSec/Manifold/main/install.sh)
```

The installer:

1. downloads the latest release for the server's architecture and verifies its checksum,
2. installs the program, the `manifold` command and the `manifold` service,
3. chooses a random panel port, URL path, username and password,
4. starts the service and prints the address and login.

The login is shown once. Sign in, then change it under Panel Settings. Replace `SERVER-IP` in the printed address with the server's address, and open the printed port in the firewall.

### Choosing the port and login yourself

Options go after the command:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/FarzadSec/Manifold/main/install.sh) \
  --port 8443 --path /panel/ --username admin --password 'a-long-password'
```

| Option | Effect |
|---|---|
| `--port N` | Panel port |
| `--path /p/` | URL path the panel is served under |
| `--username NAME`, `--password PASS` | Panel login |
| `--version vX.Y.Z` | Install this release instead of the latest |
| `--package FILE` | Install from a package file instead of downloading |
| `--no-start` | Install without starting the service |

These first-install options are ignored when a database already exists.

### Installing without GitHub access on the server

If the server cannot reach GitHub, or reaches it only through a rate limit, download `manifold-linux-amd64.tar.gz` (or `arm64`) from the repository's Releases page on another machine, copy it to the server, then:

```bash
tar -xzf manifold-linux-amd64.tar.gz manifold/install.sh
bash manifold/install.sh --package manifold-linux-amd64.tar.gz
```

## Where things live

| Path | Holds |
|---|---|
| `/usr/local/manifold/` | The program, the proxy core and its data files |
| `/etc/manifold/` | The database (`manifold.db`) and keys |
| `/var/log/manifold/` | Log files |
| `/etc/default/manifold` | Environment variables for the service |
| `/usr/bin/manifold` | The management command |

Manifold shares no file, service name or command with other panels, so it can be installed on a server that already runs one. Give it its own ports.

## Managing the service

```bash
manifold status          # service state
manifold restart         # also: start, stop, enable, disable
manifold log             # last 100 log lines; `manifold log -f` follows
manifold settings        # panel port, URL path, login state
manifold login           # set a new random login and print it
manifold login NAME PASS # set a chosen login
manifold port 8443       # change the panel port
manifold path /panel/    # change the URL path
manifold help            # everything else
```

## Client IP limits

The per-client IP limit is enforced by Fail2ban. Set it up once:

```bash
manifold iplimit install
```

`manifold iplimit status` shows the jail, `manifold iplimit remove` deletes it.

## Update

```bash
manifold update
```

The database and settings are kept. Without GitHub access, use `manifold update --package FILE`.

The panel has no update button: it never downloads or runs an updater itself.

## Settings through environment variables

Variables go in `/etc/default/manifold`, one `NAME=value` per line, followed by `manifold restart`.

| Variable | Effect |
|---|---|
| `MANIFOLD_PORT` | Panel port, overriding the stored setting |
| `MANIFOLD_DB_TYPE`, `MANIFOLD_DB_DSN` | Use PostgreSQL instead of SQLite |
| `MANIFOLD_LOG_LEVEL` | `debug`, `info`, `notice`, `warning` or `error` |

`.env.example` in the repository lists every variable.

## Remove

```bash
manifold uninstall           # keeps /etc/manifold
manifold uninstall --purge   # deletes the data as well
```

## Docker

The repository also builds as a container image. This path compiles on the server, so it needs more memory and time than the package install.

```bash
git clone https://github.com/FarzadSec/Manifold.git
cd Manifold
docker compose up -d --build
docker compose logs manifold | grep "Username"
```

- The login is generated on first start and written to the log once. To set a new one: `docker compose exec manifold manifold login`, then `docker compose restart manifold`.
- `docker-compose.yml` publishes only the panel port (2053). Add every inbound port, and the subscription port (2096) if you use subscriptions, under `ports:`.
- Data is in `./db/`, certificates in `./cert/`.
- Update with `git pull` and `docker compose up -d --build`.

## Installing from a private copy

If you keep a private copy of the repository, set `MANIFOLD_REPO=owner/name` and `MANIFOLD_TOKEN` (a token with read access to its contents) before running the installer or `manifold update`. The installer then sends the token with every download. Leave `MANIFOLD_TOKEN` unset for the public repository: a wrong token makes GitHub answer 401.

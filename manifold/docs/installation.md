# Installing Manifold

Docker is the supported way to run Manifold for now. There are no release packages yet, so the image is built on the server from this repository.

## Requirements

- A Linux server with Docker Engine and the Compose plugin
- Access to this private repository from that server
- Outbound access to GitHub during the build: the image downloads Xray-core, the MTProto helper and the routing data files
- An open TCP port for the panel (2053 by default) and one for every inbound you create

## Install

```bash
git clone git@github.com:FarzadSec/Manifold.git
cd Manifold
docker compose up -d --build
```

The first build compiles the interface and the panel, so it takes a few minutes.

## First login

No default login exists. On first start the panel generates a username and password and writes them to its log once:

```bash
docker compose logs manifold | grep "Username"
```

Open `http://SERVER-IP:2053/`, sign in with that login, then change it under Panel Settings.

If the line is gone from the log, set a new login from the command line:

```bash
docker compose exec manifold /app/x-ui setting -username NEW_NAME -password NEW_PASSWORD
docker compose restart manifold
```

## Ports

`docker-compose.yml` publishes only the panel port. Every other port the server answers on has to be added under `ports:` before clients can reach it:

- each inbound's port
- the subscription server's port (2096 by default), if you hand out subscription links

```yaml
    ports:
      - "2053:2053"
      - "2096:2096"
      - "443:443"
```

Apply the change with `docker compose up -d`.

## Where data lives

| Host path | Holds |
|---|---|
| `./db/` | The panel database (`x-ui.db`) and its keys |
| `./cert/` | TLS certificates you place there |
| `./acme/` | Certificate renewal state |

Back up `./db/` to keep users, inbounds and settings.

## Settings in `docker-compose.yml`

| Variable | Effect |
|---|---|
| `XUI_PORT` | Panel port. Publish the same port under `ports:` |
| `XUI_INIT_WEB_BASE_PATH` | URL path the panel is served under on first start (default `/`) |
| `XUI_ENABLE_FAIL2BAN` | Enforces the per-client IP limit. Needs the `NET_ADMIN` and `NET_RAW` capabilities already set in the file |
| `XUI_DB_TYPE`, `XUI_DB_DSN` | Use PostgreSQL instead of SQLite |

The variable names still start with `XUI_`; they change with the rename described in the README.

### PostgreSQL

```bash
docker compose --profile postgres up -d --build
```

Then uncomment `XUI_DB_TYPE` and `XUI_DB_DSN` in `docker-compose.yml` and run `docker compose up -d` again.

## Update

```bash
git pull
docker compose up -d --build
```

The database in `./db/` is kept and migrated on start.

## Remove

```bash
docker compose down
```

Delete `./db/`, `./cert/` and `./acme/` to remove the data as well.

## Do not use yet

`install.sh`, `update.sh`, the update entry in the `x-ui` menu and the panel's own update action still download another project's release and would replace Manifold with it. They are being rewritten in the foundation phase.

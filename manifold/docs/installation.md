# Installing Manifold

Manifold installs from a release package as a systemd service. The server downloads a ready-made package; nothing is compiled on it.

## Requirements

- A Linux server with systemd, on x86-64 or ARM64
- Root access
- An open TCP port for the panel and one for every inbound you create
- For OpenVPN, WireGuard and L2TP inbounds: the `openvpn`, `wireguard-tools`, `xl2tpd`, `ppp`, strongSwan, `nftables` and `iptables` packages. The installer installs them on Debian, Ubuntu, Fedora, RHEL and its rebuilds, openSUSE and Arch; elsewhere install them yourself. Each is installed on its own, so one that a distribution does not carry costs only the protocol that needs it, and the installer names it
- For WireGuard inbounds: a kernel with WireGuard, which every mainstream distribution has had since Linux 5.6
- For L2TP inbounds: a kernel with PPP and IPsec, which every mainstream distribution has. UDP ports 500, 4500 and 1701 must reach the server. L2TP is faster when the kernel carries the sessions itself; if `modinfo l2tp_ppp` finds no module, as on Ubuntu 22.04 and 24.04 with their original kernels, `apt install linux-modules-extra-$(uname -r)` adds it. Restart the panel afterwards (`manifold restart`): the L2TP server looks for the module once, when it starts

Releases are installed and exercised with real VPN clients on Ubuntu 22.04, 24.04 and 26.04 before they are published. Other distributions get the same package but are not tested this way.

## Install

As root on the server:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/FarzadSec/Manifold/main/install.sh)
```

The installer:

1. downloads the latest release for the server's architecture and verifies its checksum,
2. installs the program, the `manifold` command and the `manifold` service,
3. installs the system packages the VPN engines need, if they are missing,
4. asks whether you want to choose the panel port (a random free one otherwise), and picks a random URL path, username and password,
5. asks how the panel should be reached (below),
6. starts the service, waits until the panel holds its port, and prints the address, the login and an API token in a box.

How the panel is reached:

| Choice | What you get |
|---|---|
| 1. Domain | A Let's Encrypt certificate for a domain that points at this server |
| 2. IP address (the default) | A Let's Encrypt certificate for the server's public IP: six days long, renewed by the panel itself |
| 3. Your own files | The panel serves a certificate and key you already have |
| 4. Plain HTTP | No certificate; you can also have the panel listen on 127.0.0.1 only and reach it through an SSH tunnel |

Choices 1 and 2 prove the name over HTTP-01, so port 80 must be free and open to the internet while the installer runs. If the certificate cannot be had, the install goes on over plain HTTP and says so; get one later from `manifold` → 18, or with `manifold cert issue -domain NAME`. The panel renews certificates it got itself, IP ones included.

The installer asks only when it runs in a terminal. Piped, scripted or in CI it asks nothing, takes `--port` or a random port, and starts the panel on plain HTTP.

If the panel does not come up, the installer prints the service's last log lines and exits with an error instead of an address.

Two of the packages come with services of their own, and the installer sets up the ones it installed itself: strongSwan's is started, because L2TP hands it one connection, and the distribution's `xl2tpd` service is switched off, because it would hold the port the panel's own L2TP server needs. Services that were on the server before are left as they are. On a server that already runs IPsec through the older `ipsec` command the installer does not install strongSwan's `swanctl` service next to it; L2TP with IPsec stays unavailable there until you move that setup to `swanctl` yourself.

A package you install by hand later is found without a reinstall or a restart: the inbound form stops saying its engine is missing, and an inbound that could not start for lack of it starts within ten seconds.

The login and the API token are shown once. Sign in, then change the login under Panel Settings. If the installer could not find the server's public address it prints `SERVER-IP` in its place. Open the printed port in the firewall.

### The tunnel edition, for a relay

A relay that users connect to and that forwards their ports through tunnels to the main panel needs no Xray, inbounds or subscriptions. Install it as the tunnel edition:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/FarzadSec/Manifold/main/install.sh) --edition tunnel
```

It is the same package. The edition is recorded as `MANIFOLD_EDITION=tunnel` in `/etc/default/manifold`, so `manifold update` keeps it; it installs only `nftables`, `iptables` and `iproute2`, and its panel shows the overview, the tunnels, the settings and the API. To let the main panel manage the relay's tunnels, create an API token with the tunnel scope on the relay (Panel Settings → Security, or `manifold setting -getApiToken -tokenName owner -tokenScope tunnel`) and add the relay under Tunnels → Relays on the main panel. See [Tunnels](usage.md#tunnels).

Running the installer again with `--edition full` turns a relay into a full panel.

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
| `--no-deps` | Do not install system packages; OpenVPN and WireGuard inbounds then need `openvpn`, `wireguard-tools` and `nftables` installed by hand |

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

The WireGuard and L2TP engines also write where the programs they drive look: one config per WireGuard inbound in `/etc/wireguard/`, each starting with a line that names Manifold, and `/etc/swanctl/conf.d/manifold-l2tp.conf` for the IPsec side of L2TP.

Manifold shares no file, service name or command with other panels, so it can be installed on a server that already runs one. The installer picks a free panel port, and when another program holds the default subscription port (2096) it gives Manifold's subscription server a free one and prints it. Inbound ports are yours to keep apart.

## Managing the service

`manifold` on its own opens a menu, as `x-ui` does on a 3x-ui server. It works while the panel's pages are down, which is when it is needed:

| | |
|---|---|
| 1–3 | Update, install a given version, uninstall |
| 4–8 | Reset the login (and two-factor), the URL path or all settings; change the port; show the settings and the panel's address |
| 9–16 | Start, stop, restart, restart Xray, status, logs, start at boot or not |
| 17 | Doctor: checks the service, the panel port, the listen address, the certificate, the firewall and the disk, and says how to fix what it finds |
| 18 | Certificates: Let's Encrypt for a domain, for the server's IP, or through Cloudflare or ArvanCloud DNS; your own files; show or remove the panel's certificate |
| 19 | IP limit: install Fail2ban, ban length, unban everyone, ban and unban an address, ban log |
| 20 | Firewall (ufw): turn it on with SSH, HTTP, HTTPS and the panel's ports open; open and close ports |
| 21 | SSH port forwarding: listen on 127.0.0.1 only and print the `ssh -L` command, or listen everywhere again |
| 22 | Recovery: turn two-factor off, drop the panel to plain HTTP, listen everywhere, a new API token, run the database migrations again |
| 23 | Backups: write a full backup to `/root`, sealed with a passphrase if you give one; restore one |
| 24–25 | BBR on or off; update Xray's geo files (Loyalsoldier, chocolate4u, runetfreedom) |

The same things as commands:

```bash
manifold status          # service state
manifold restart         # also: start, stop, enable, disable, restart-xray
manifold log             # last 100 log lines; `manifold log -f` follows
manifold doctor          # why the panel cannot be reached
manifold settings        # panel port, URL path, address, login state
manifold login           # set a new random login and print it
manifold login NAME PASS # set a chosen login
manifold port 8443       # change the panel port
manifold path /panel/    # change the URL path
manifold cert issue -domain panel.example.com   # or the server's IP address
manifold cert -reset     # serve the panel over plain HTTP
manifold backup create -out /root -passphrase P
manifold geo all         # update the geo files
manifold bbr enable
manifold help            # everything else
```

## Client IP limits

The per-client IP limit is enforced by Fail2ban. Set it up once:

```bash
manifold iplimit install
```

`manifold iplimit status` shows the jail, `manifold banlog` the last bans, `manifold iplimit bantime 60` makes a ban last an hour, `manifold iplimit unban-all` lifts every ban, and `manifold iplimit remove` deletes the jail.

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

Both also remove the WireGuard configs and the IPsec connection the panel wrote; they are rebuilt from the data if the panel is installed again. The system packages stay, and so do the `*.before-manifold` originals of WireGuard interfaces the panel took over.

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
- OpenVPN inbounds need the `NET_ADMIN` capability and the `/dev/net/tun` device, both set in `docker-compose.yml`. WireGuard inbounds need `NET_ADMIN` and the host's own WireGuard kernel module; their configs live inside the container, so mount `/etc/wireguard` if they should outlive it. L2TP inbounds are not available in the container: their IPsec side is a strongSwan service of the host, which the image does not run, and the inbound form says the engine is not installed. The container path is not exercised by the project's tests.
- Data is in `./db/`, certificates in `./cert/`.
- Update with `git pull` and `docker compose up -d --build`.

## Installing from a private copy

If you keep a private copy of the repository, set `MANIFOLD_REPO=owner/name` and `MANIFOLD_TOKEN` (a token with read access to its contents) before running the installer or `manifold update`. The installer then sends the token with every download. Leave `MANIFOLD_TOKEN` unset for the public repository: a wrong token makes GitHub answer 401.

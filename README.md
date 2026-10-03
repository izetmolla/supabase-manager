![Supabase Manager: run and manage all your Supabase environments from one simple panel](docs/assets/banner.svg)

# Supabase Manager

**Manage all your Supabase environments from one simple panel.**

Created by **Izet Molla** ([izetmolla@gmail.com](mailto:izetmolla@gmail.com)).

## Fast install

On a Linux server (amd64 or arm64), as root:

```bash
curl -fsSL https://raw.githubusercontent.com/izetmolla/supabase-manager/main/script/install.sh | sudo bash

or

curl -sSL https://raw.githubusercontent.com/izetmolla/supabase-manager/main/script/install.sh | sudo ADDR=0.0.0.0:8080 bash -s update
```

The [installer](https://github.com/izetmolla/supabase-manager/blob/main/script/install.sh) checks the system and the port, installs Docker if it is missing, pulls `izetmolla/supabase-manager`,
prepares the projects folder and data volume, starts the container and waits until it is healthy.
Then open the printed URL and create the first admin account.

By default it listens on `127.0.0.1:8080` (reach it through an SSH tunnel). Settings are passed as
environment variables and saved to `/etc/supabase-manager/install.env`:

```bash
# expose on all interfaces and store projects elsewhere
curl -fsSL https://raw.githubusercontent.com/izetmolla/supabase-manager/main/script/install.sh | sudo ADDR=0.0.0.0:8080 PROJECTS_ROOT=/srv/supabase bash

# update to the latest image (data and projects are kept)
curl -fsSL https://raw.githubusercontent.com/izetmolla/supabase-manager/main/script/install.sh | sudo bash -s update

# remove the container (PURGE=1 also deletes the data volume)
curl -fsSL https://raw.githubusercontent.com/izetmolla/supabase-manager/main/script/install.sh | sudo bash -s uninstall
```

Other options: `VERSION` (image tag), `PUID` / `PGID`, `TZ`, `SECURE_COOKIES`.

Running more than one Supabase project on a server usually means juggling `supabase start` in
several folders, port clashes, hand-edited `config.toml` files and a Studio per project. Supabase
Manager does that for you: create or import a project, press **Start**, and every environment is
listed in one place with its status, logs, settings and Studio.

- **One place for every environment:** start, stop and restart projects, see their containers,
CPU, memory and live logs.
- **No port juggling:** each project gets its own non-colliding port block and Docker network.
- **Settings without editing files:** auth providers, services, ports, networking and storage are
forms; secrets are stored encrypted.
- **One URL to remember:** each project's Studio, Mailpit and API are reached through the panel,
behind its login.
- **Always up to date:** the Supabase CLI and the manager itself update from the panel.

It is one Docker container (or one binary) that drives the host's Docker, so it runs next to your
existing setup.

## Features

- Users with `admin` / `member` roles, first-run setup, audit log
- Create projects (`supabase init` plus automatic, non-colliding port blocks) or import existing ones
- Start, stop, and restart with live CLI output; container list, CPU/memory stats, and live logs
- Auth providers (Apple, Google, GitHub, ...) with callback URL hints; client secrets are stored
encrypted and injected as `env(...)` references, never written into `config.toml`
- Auth settings, enabled services, ports, and a raw `config.toml` editor (validated, with a `.bak` backup)
- Migrations (create, edit, apply, reset, diff), TypeScript type generation, Edge Function scaffolding
- Built-in reverse proxy: each project's Studio, Mailpit, and API are served on the manager's own
port behind its login, so only one port has to be reachable
- Supabase CLI management: installs the latest release when the CLI is missing, checks for
updates, installs a specific version, optional automatic updates
- Proxy Manager (admins, off by default): nginx and Traefik containers configured from one place,
with domains, routes, load balancing, Let's Encrypt and other ACME certificates, access lists,
staged deploys, revisions and rollback, health checks and live access logs
- Per-project networking: publish ports on `127.0.0.1` or `0.0.0.0`, a dedicated Docker network
with its own driver, subnet, gateway, IP range, MTU, IPv6 and driver options, or an existing network

## Requirements

- Linux with Docker (the user running the manager needs access to `/var/run/docker.sock`)
- [Supabase CLI](https://supabase.com/docs/guides/cli) on `PATH` or `SUPABASE_BIN`; if it is
missing, the manager downloads the latest release on startup (Linux/macOS, amd64/arm64)
- To build: Go 1.27+, Node 24+ and pnpm

## Quick start (production)

```bash
cp .env.example .env        # set JWT_SECRET and ENCRYPTION_KEY
make build                  # pnpm build → services/manager/web/dist, then bin/supabase-manager (UI embedded)
./bin/supabase-manager
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080) and create the first admin account. The setup page only works while no
users exist.

The binary is self-contained because the UI is embedded. Run it from the directory that holds
`.env` and `data/`, or pass configuration as real environment variables.

## Docker

### Run the published image

No checkout needed; this pulls `izetmolla/supabase-manager:latest` and runs it with everything it
needs (Docker socket, projects folder, data volume):

```bash
PROJECTS_ROOT=${PROJECTS_ROOT:-/etc/supabase-manager/projects}

# Creates the projects folder on the host and the data volume, both owned by your user.
docker run --rm -v supabase-manager-data:/data -v "$PROJECTS_ROOT":/projects alpine sh -c \
  "mkdir -p /data/home /data/bin && chown -R $(id -u):$(id -g) /data && chown $(id -u):$(id -g) /projects && chmod 0755 /projects"

docker run -d --name supabase-manager --restart unless-stopped \
  --init --network host \
  --user "$(id -u):$(id -g)" --group-add "$(stat -c %g /var/run/docker.sock)" \
  -e ADDR=127.0.0.1:8080 -e PROJECTS_ROOT="$PROJECTS_ROOT" \
  -v supabase-manager-data:/data \
  -v "$PROJECTS_ROOT":"$PROJECTS_ROOT" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  izetmolla/supabase-manager:latest
```

Projects are stored in `/etc/supabase-manager/projects` unless `PROJECTS_ROOT` is set. Docker
creates the folder on the host if it is missing, and the `alpine` step hands it to your user (only
the top level, so database files inside project folders keep their owners). It is safe to run
again.

Open [http://127.0.0.1:8080](http://127.0.0.1:8080) and create the first admin account. `JWT_SECRET` and `ENCRYPTION_KEY`
are generated on first start and kept in the data volume.

To update, open **System** (admin menu) > **Supabase Manager** > **Check for updates** and
**Update** (see [Updating from the panel](#updating-from-the-panel)), or by hand:
`docker pull izetmolla/supabase-manager:latest && docker rm -f supabase-manager`, then run the last
command again; data and projects are kept.

### Build and run from source

The production image is built `FROM scratch`. It contains only the manager binary, the Supabase
CLI with the glibc files it needs, the static `docker` CLI (the Supabase CLI shells out to it), CA
certificates and timezone data. The manager and `docker` binaries are stripped and UPX-compressed
in a shrink stage (`izetmolla/containerws:binoptimization`); the Supabase CLI is left untouched
because it is a Bun binary with an embedded payload. There is no shell in the image.

The container drives the host's Docker daemon through the mounted socket, like Portainer, so
project stacks run as sibling containers on the host.

```bash
make docker-build                       # services/manager/Dockerfile -> izetmolla/supabase-manager:latest (+ :<version>)
make docker-publish                     # build and push :latest and :<version> (+ the git tag), and the proxy images
make proxy-images                       # build the Proxy Manager images locally (nginx and Traefik with sm-proxy-agent)
make docker-up                          # run detached, restart unless stopped
make docker-logs                        # follow logs
make docker-down                        # remove the container (data is kept)
```

Images are named `$(DOCKER_REGISTRY)/$(DOCKER_IMAGE_PREFIX)-<service>`, defaulting to
`izetmolla/supabase-manager`. The build context is the repo root; the Dockerfile builds the UI with
pnpm, embeds it into the Go binary and stamps `VERSION` / `COMMIT_SHA` into `services/version`.

Or with Docker Compose:

```bash
make compose-up
# same as:
PUID=$(id -u) PGID=$(id -g) DOCKER_GID=$(stat -c %g /var/run/docker.sock) docker compose up -d --build
```

A one-shot `prepare` service creates `PROJECTS_ROOT` and fixes ownership before the manager starts.

On Kubernetes, `make k8s-apply` applies `k8s/general.yaml` and `k8s/manager.yaml` (hostNetwork,
node Docker socket, `PROJECTS_ROOT` hostPath, PVC for `/data`); `make docker-deploy` publishes and
restarts the deployment.

`make docker-up` accepts `PROJECTS_ROOT`, `ADDR`, `PUID`, `PGID`, `DOCKER_GID`, `IMAGE`,
`CONTAINER` and `DATA_VOLUME`, for example `make docker-up ADDR=0.0.0.0:8080`. Bump the bundled
tools with `make docker-build SUPABASE_CLI_VERSION=x.y.z`; newer CLI versions installed from the
UI are kept in the data volume.

How the container is wired, and why:


| Mount / option                                      | Purpose                                                                                                         |
| --------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `--network host`                                    | The manager proxies to project ports on `127.0.0.1` and probes free ports on the host                           |
| `/var/run/docker.sock` + `--group-add <socket gid>` | Create and manage the Supabase containers, networks and volumes                                                 |
| `PROJECTS_ROOT:PROJECTS_ROOT`                       | Project folders, mounted at the **same path** because the Supabase CLI passes them to the daemon as bind mounts |
| volume `supabase-manager-data` → `/data`            | Database, generated `JWT_SECRET` / `ENCRYPTION_KEY` (`/data/.env`), CLI installs, CLI home                      |
| `--user PUID:PGID`                                  | Runs as the user owning `PROJECTS_ROOT` (default: whoever runs `make`); the image default is 1000:1000          |
| `--init`                                            | Reaps processes left behind by the Supabase and Docker CLIs                                                     |


`make docker-up` creates `PROJECTS_ROOT` (default `/etc/supabase-manager/projects`) if needed and
chowns it and the data volume to `PUID:PGID` before starting, so changing the user is safe. Database files of the projects live in Docker volumes managed by the Supabase CLI and
survive container recreation as well.

Access to the Docker socket is equivalent to root on the host: keep `ADDR` on `127.0.0.1` and put
a TLS reverse proxy in front if the UI has to be reachable from elsewhere.

Maintenance commands:

```bash
make docker-users                       # list users
make docker-user U=admin@example.com    # show one user
make docker-passwd U=admin@example.com  # set a password (interactive, hidden input)
make docker-cli ARGS="user get 1 --json"
make docker-shell                       # alpine shell with the container's mounts (the image has none)
make docker-backup                      # ./backups/supabase-manager-data-<date>.tgz
make docker-import-local                # move ./data and the secrets from ./.env into the volume
```

### Versions and releases

Releases are git tags `manager/vX.Y.Z`; each one is published as `izetmolla/supabase-manager:X.Y.Z`
and `:latest`, together with `izetmolla/supabase-manager-proxy-nginx` and `-traefik` under the same
tags. The version is stamped into the binary and shown in the panel; a release manager creates proxy
containers from the proxy images of its own version, other builds use `:latest`.

```bash
make version                            # current release, e.g. 1.4.0
make release next                       # update, fix, tidy, vet, lint, commit the changes, then release-patch
make deploy-all                         # build and push every image (manager + proxy images) at the current
                                        # version: no checks, no version bump, no new tag
make deploy-all next                    # full pipeline: update, fix, fmt, tidy, proto, vet, lint, tests, frontend
                                        # checks, build, integration tests, commit, then release-patch and push
                                        # every image (manager + proxy images) to Docker Hub
make deploy-all V=2.1.0                 # the same with an explicit version
make check                              # vet, lint, test, frontend lint and typecheck (no changes, no publish)
make release-patch                      # 1.4.0 -> 1.4.1: tag, build + push :1.4.1 and :latest, push the git tag
make release-minor                      # 1.4.0 -> 1.5.0
make release-major                      # 1.4.0 -> 2.0.0
make release V=2.1.0                    # explicit version
make bump-minor                         # tag + push the git tag only, no image
```

`script/release.sh` refuses to tag uncommitted changes (`ALLOW_DIRTY=1` overrides) and an existing
version. The tag is created first and removed again if the image cannot be published; it is pushed
to `origin` (`GIT_REMOTE=...`, or `PUSH_TAG=0` to keep it local) after the image.
`make docker-publish` and `make docker-deploy` push the matching tag as well. A build that is not
exactly on a release tag is versioned `X.Y.Z-<commits>-g<sha>`, so publishing it never overwrites a
release image.

### Updating from the panel

**System > Supabase Manager** compares the running version with the highest `X.Y.Z` tag of
`UPDATE_REPOSITORY` on Docker Hub (checked hourly, or with **Check for updates**). **Update** pulls
the new image and starts a short-lived `supabase-manager-updater` container from it, which re-creates
the manager container with the same name, settings, mounts and networks on the new image. If the
new container exits or restarts within 15 seconds, the previous one is restored. The page reloads
once the new version answers.

This works with `docker run`, `make docker-up` and Docker Compose (the container must have the
Docker socket mounted). On Kubernetes the button is disabled; use `make docker-deploy`.

## Development

The backend is Go (Fiber v3, GORM with SQLite or Postgres); the UI is Vite, React, Tailwind v4 and
shadcn/ui. Both ship as one binary (`services/manager`): the UI is embedded with `go:embed`, so
nothing is loaded from a CDN.

```bash
make install-frontend      # pnpm install in frontend/
make dev
```

This runs the API on `:8080` (`go run ./services/manager/cmd`) and the Vite dev server on `:5173`,
which proxies `/api` to the API. Open [http://localhost:5173](http://localhost:5173).

Other workflows:

```bash
make run-manager           # API with air live reload (go install github.com/air-verse/air@latest)
make run-frontend manager  # Vite dev server only
make test                  # go test across the workspace
make test-integration      # Proxy Manager tests against Docker (nginx -t, Traefik, and the proxy images end to end)
make fmt | tidy | lint     # per-module, via script/go-modules.sh
make upgrade               # bump Go in go.mod / go.work / Dockerfiles
```

shadcn/ui components live in `frontend/packages/ui` and are imported as
`@workspace/ui/components/<name>`; add new ones from that folder with `pnpm dlx shadcn add <name>`.

## Command line

The same binary can manage users directly against the configured database. Run it from the same
directory (or with the same environment) as the server.

```bash
./bin/supabase-manager user list [--json]
./bin/supabase-manager user get <id|email> [--json]
./bin/supabase-manager user set-password <id|email>          # prompts twice, input hidden
echo 'new-password' | ./bin/supabase-manager user set-password admin@example.com --password-stdin
```

Setting a password signs the user out of all existing sessions and is recorded in the audit log.

## Configuration

Settings come from environment variables. A `.env` file in the working directory is also read,
but real environment variables take precedence.


| Variable                 | Default                                                               | Description                                                               |
| ------------------------ | --------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `ADDR`                   | `127.0.0.1:8080`                                                      | Listen address                                                            |
| `DB_DRIVER`              | `sqlite`                                                              | `sqlite` or `postgres`                                                    |
| `DB_DSN`                 | `data/manager.db`                                                     | SQLite file path or Postgres DSN                                          |
| `JWT_SECRET`             | insecure dev value                                                    | Signs session tokens. **Set this.**                                       |
| `ENCRYPTION_KEY`         | derived from `JWT_SECRET`                                             | Encrypts stored secrets. Changing it makes them unreadable.               |
| `PROJECTS_ROOT`          | `~/supabase-projects` (`/etc/supabase-manager/projects` in the image) | Where new projects are created                                            |
| `SUPABASE_BIN`           | `supabase`                                                            | Supabase CLI binary                                                       |
| `TOOLS_DIR`              | `data/bin`                                                            | Where the manager installs Supabase CLI releases                          |
| `SUPABASE_AUTO_INSTALL`  | `true`                                                                | Install the latest CLI on startup when none is found                      |
| `DOCKER_SOCKET`          | `/var/run/docker.sock`                                                | Docker Engine socket                                                      |
| `PORT_RANGE_START`       | `54300`                                                               | Base of the first port block                                              |
| `PORT_BLOCK`             | `100`                                                                 | Size of each project's port block                                         |
| `INSPECTOR_PORT_START`   | `8083`                                                                | First Edge Runtime inspector port                                         |
| `SECURE_COOKIES`         | `false`                                                               | Set `true` when served over HTTPS                                         |
| `DEV_CORS_ORIGIN`        | empty                                                                 | Allowed origin if the UI is served from another host                      |
| `UPDATE_REPOSITORY`      | `izetmolla/supabase-manager`                                          | Docker Hub repository checked for manager updates                         |
| `SELF_CONTAINER`         | detected                                                              | Name or ID of the manager's own container, if detection fails             |
| `PROXY_MANAGER_URL`      | derived from `ADDR` (`http://127.0.0.1:<port>`)                       | Where proxy instances forward ACME HTTP-01 requests and error pages       |
| `ACME_TLS_ALPN_ADDR`     | `127.0.0.1:5443`                                                      | Listen address of the TLS-ALPN-01 challenge solver                        |
| `PROXY_AGENT_ADDR`       | `127.0.0.1:7070`                                                      | gRPC listen address for proxy agents; a non-loopback address turns on TLS |
| `PROXY_IMAGE_REPOSITORY` | `izetmolla/supabase-manager-proxy`                                    | Proxy image prefix (`-nginx` / `-traefik` is appended)                    |
| `PROXY_IMAGE_TAG`        | the manager's version (`latest` for development builds)               | Tag of the proxy images used by new instances                             |


### Port layout

Project *n* gets `base = PORT_RANGE_START + n * PORT_BLOCK`:


| Service    | Port      |
| ---------- | --------- |
| Shadow DB  | base + 20 |
| API (Kong) | base + 21 |
| Postgres   | base + 22 |
| Studio     | base + 23 |
| Mailpit    | base + 24 |
| Analytics  | base + 27 |
| Pooler     | base + 29 |


Block 0 matches the stock Supabase defaults (54321, 54322, ...), so an existing default project
imports cleanly.

### Service proxy

Every project's web services are also reachable through the manager, on its own address, and
require a manager session:


| URL                    | Upstream                                                     |
| ---------------------- | ------------------------------------------------------------ |
| `/proxy/<slug>/studio` | Redirects to `/project/<slug>`, where Studio is served       |
| `/proxy/<slug>/mail/`  | Mailpit                                                      |
| `/proxy/<slug>/api/`   | API gateway (Kong): REST, Auth, Storage, Realtime WebSockets |


Studio is built without a base path, so it cannot live under a prefix. It is served at the root
instead, using the project slug as Studio's project ref (`/project/<slug>/editor`, ...). Its
root-level requests (`/_next/...`, `/api/platform/...`) are routed by their path, then their
`Referer`, then a `sm_studio` cookie set by the last Studio page loaded. Several projects' Studios
can be open in different tabs at once.

Manager session cookies are removed before requests are forwarded, and cross-origin writes and
WebSocket handshakes are rejected.

### Supabase CLI updates

**System settings** (admin menu, top right) shows the CLI in use, the latest release on GitHub,
and lets admins install the latest or a specific version. Releases are downloaded from
`github.com/supabase/cli`, verified against the release's `checksums.txt`, and installed into
`TOOLS_DIR`; a Homebrew or system installation is never modified. After an install the manager
uses that binary; **Use system CLI instead** switches back to `SUPABASE_BIN` / `PATH`.

With **Install updates automatically** on, the manager checks every 6 hours and installs new
releases while no project command is running.

### Networking

Each project has a **Network** page under Project Settings; new projects get the defaults from
System settings. Changes apply on the next start.


| Mode                                 | Behaviour                                                                                                                                                                                                                                                                        |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Dedicated network (managed, default) | The manager creates `supabase_manager_<slug>` (or a name you choose) with your driver, subnet, gateway, IP range, MTU, IPv6 and driver options, and runs the CLI with `--network-id`. The network is recreated when its settings change and removed when the project is deleted. |
| Supabase CLI default                 | The CLI creates `supabase_network_<project_id>`. Ports use the Docker daemon's default bind address, normally `0.0.0.0`. Imported projects start in this mode.                                                                                                                   |
| Existing Docker network              | Join a network you manage yourself, for example a `macvlan` network or one shared with a reverse proxy.                                                                                                                                                                          |


The bind address (bridge networks) decides where ports are published: `127.0.0.1` for this
machine only, or `0.0.0.0` for all interfaces. Other addresses are rejected because the Supabase
CLI connects to the database on `127.0.0.1` while starting. To expose a project to a single
network only, bind `0.0.0.0` and restrict access with a firewall.

Local Supabase uses well-known credentials (Postgres password `postgres`, demo JWT secret and
keys), so a project bound on `0.0.0.0` can be read and changed by anyone who reaches the host.

Two projects can never share a network: Supabase containers use fixed DNS names (`db`, `kong`,
...). With a subnet pool in the defaults (e.g. `10.210.0.0/16`), each new project gets the next
`/24` that overlaps no Docker network, other project, or host interface.

The driver is picked from the drivers the Docker daemon reports:


| Driver                 | Notes                                                                                                                                                        |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `bridge` (recommended) | Bind address, host bridge interface name and IP masquerading (outbound internet) can be set.                                                                 |
| `overlay`              | Only offered while Docker Swarm is active; created as attachable, optionally encrypted.                                                                      |
| `macvlan`, `ipvlan`    | Not available for managed networks: they publish no ports, which the CLI and the manager need. Use "Existing Docker network" if you know what you are doing. |
| Plugins / other        | Any installed network plugin; options are passed through as-is.                                                                                              |


### Storage (persistent data)

The **Storage** page under Project Settings decides where the database
(`supabase_db_<project_id>`, `/var/lib/postgresql/data`) and the uploaded files
(`supabase_storage_<project_id>`, `/mnt`) are kept. New projects get the defaults from System settings.


| Mode                     | Location                                                                                                                                                |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Docker volumes (default) | Named volumes in Docker's data root (`/var/lib/docker/volumes/...`).                                                                                    |
| Host folder              | `<folder>/<slug>/db` and `<folder>/<slug>/storage` on the host (default `<project>/volumes`). The folder must exist at that path for the Docker daemon. |
| NFS share                | `<server>:<export>/<slug>/{db,storage}`, mounted by Docker (default options `rw,nfsvers=4`).                                                            |
| Volume driver            | Any volume driver with options; `{project}` and `{volume}` are replaced, e.g. CIFS via the `local` driver with `device=//nas/share/{project}/{volume}`. |


Volumes are created before `supabase start`. When the location of an existing volume changes, the
data is copied to the new location on the next start (the original stays until the copy
succeeded); the target must be empty. A new, empty database in custom storage gets migrations and
seed data applied right after the first start, and **Reset database** resets it in place so the
volume keeps its location. Deleting a project removes its volume definitions: data in Docker
volumes is deleted, data in host folders or on NFS stays where it is.

**Containers & volumes** (user menu, admins) lists every container with its mounts and every volume
with its driver, location, users and optional size. Mounts on manager-controlled custom storage are
highlighted and can be filtered.

### Proxy Manager

**Proxy Manager** (left rail, admins only) publishes domains through nginx or Traefik. It is off by
default: an admin turns it on under **System settings → Proxy Manager**. Turning it off stops every
proxy container and the background work (agent server, health checks, certificate renewal) and
hides the UI; the configuration is kept, and the deployed instances start again when it is turned
back on.

The manager stores the configuration and renders it for each proxy instance. Each instance is a
container the manager creates with Docker from the proxy images
(`izetmolla/supabase-manager-proxy-nginx` and `-traefik`, published with every manager release).
They are the official nginx and Traefik images plus `sm-proxy-agent` (`services/proxy-agent`),
which runs as the container's main process: it supervises the proxy, connects back to the manager
over gRPC (`PROXY_AGENT_ADDR`, protocol in `shared/proxyagent/v1`), and receives configurations,
validates them in place, switches to them, reloads the proxy and reports status and access logs.
Each agent authenticates with a per-instance token passed in the container environment; when the
manager listens beyond loopback the connection uses TLS with a pinned, self-signed certificate.
Instances still on the plain `nginx` / `traefik` images are moved to the proxy images on startup.

- **Instances** run on the host network, each with its own bind IP and ports. A host can be served
by several instances. Instance pages show whether the agent is connected and whether the live
configuration matches the deployed revision; a container that comes back with a different
configuration is resynced automatically.
- **Hosts** map domains to an upstream, with path and header routes, strip-prefix and regex
rewrites, WebSockets, timeouts, body limits, redirects, error pages, request/response headers,
CORS, HSTS, forced HTTPS, IP rules, rate limits and raw nginx/Traefik snippets.
- **Upstreams** are `host:port` targets or project services picked from a list (Kong, Studio,
Postgres, ...), with round robin, least connections or IP hash, weights, backup targets, sticky
sessions and health checks. **Streams** forward raw TCP/UDP ports.
- **Access lists** combine basic-auth users (stored as `$apr1$` hashes) with IP allow and deny
rules; host IP rules are checked first and always apply.
- **Certificates** are issued by the manager with [lego](https://go-acme.github.io/lego/):
HTTP-01 (forwarded by the proxies to the manager), TLS-ALPN-01 (nginx only) or DNS-01 with
Cloudflare or any other lego provider. Let's Encrypt (production and staging), ZeroSSL, Buypass,
Google Trust Services and custom ACME directories (with EAB) are supported, and certificates can
also be uploaded. They are
renewed 30 days before expiry, then deployed to the instances that use them.

Changes are drafts until deployed. **Deploy** shows a diff per instance and sends the new
configuration to the agent, which validates it (`nginx -t` on a staging copy; for Traefik, the
errors in its API after the file provider reloads), applies it, and restores the previous
configuration if the proxy rejects it. Every deploy is
kept as a revision that can be previewed and rolled back. Instance pages show status, container
logs, live access logs per host and upstream health; certificates close to expiry are flagged.

Traefik limitations (shown as warnings in the preview): no IP deny rules, access lists always
require both the IP match and the password, no least-connections or IP-hash balancing (weighted
round robin and a sticky cookie are used), and no TLS-ALPN-01.

Ports used by the proxies are listed in [docs/PORTS.md](docs/PORTS.md).

## Security notes

- Keep `ADDR` on `127.0.0.1`. To reach it remotely, use an SSH tunnel
(`ssh -L 8080:127.0.0.1:8080 host`) or a TLS reverse proxy with `SECURE_COOKIES=true`.
- Anyone who can use the manager can control Docker on the host. Only give accounts to trusted people.
- Back up `data/manager.db` (or your Postgres database) together with `ENCRYPTION_KEY`.

## Author

Supabase Manager is created and maintained by **Izet Molla**: [izetmolla@gmail.com](mailto:izetmolla@gmail.com).

## Notes for sandboxed environments

If `/tmp` is mounted `noexec`, point Go and npm elsewhere:

```bash
export GOTMPDIR=$HOME/.cache/gotmp && mkdir -p $GOTMPDIR
export NPM_CONFIG_CACHE=$HOME/.npm-cache
```


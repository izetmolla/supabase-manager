# Ports

Supabase Manager is a single service: the API, the embedded UI and the project service proxy all
listen on one address. There is no gateway.

| Port | Service | Env | Notes |
|-----:|---------|-----|-------|
| **8080** | manager (`services/manager`) | `ADDR` (default `127.0.0.1:8080`) | API (`/api`), UI (embedded), project proxy (`/proxy/<slug>/...`, `/project/<slug>` Studio) |
| **5173** | Vite dev server (`frontend/apps/manager`) | — | Development only; proxies `/api`, `/proxy`, Studio paths to `:8080` (`VITE_API_PROXY_TARGET`) |

## Proxy Manager

The Proxy Manager is off until enabled in System settings; none of these ports are used before.
Proxy instances (containers from `izetmolla/supabase-manager-proxy-nginx` / `-traefik`) use the
host network, so their ports are opened directly on the host at the instance's bind IP. The manager
refuses to deploy an instance whose ports overlap another instance, a stream, the manager's own
address, the agent server, the TLS-ALPN-01 solver or a project port block.

| Port | Service | Env / setting | Notes |
|-----:|---------|---------------|-------|
| **80** / **443** | each proxy instance | instance HTTP / HTTPS port and bind IP | Defaults for the first instance; further instances need another port or bind IP |
| any | TCP/UDP streams | stream listen port | Opened on the instance's bind IP |
| **8090**–8189 | Traefik admin API | instance admin port (auto-assigned) | `127.0.0.1` only; used for ping and `/api/rawdata` checks after a deploy |
| **5443** | manager TLS-ALPN-01 solver | `ACME_TLS_ALPN_ADDR` (default `127.0.0.1:5443`) | Only while a TLS-ALPN-01 challenge runs; nginx instances with TLS-ALPN enabled forward `acme-tls/1` connections here |
| **7070** | manager gRPC server for proxy agents | `PROXY_AGENT_ADDR` (default `127.0.0.1:7070`) | Each container's `sm-proxy-agent` connects here to receive configurations and report status and access logs; TLS when not on loopback |
| 8080 | manager callbacks | `PROXY_MANAGER_URL` (default derived from `ADDR`) | Proxies forward `/.well-known/acme-challenge/` and error pages to the manager |

## Supabase project port blocks

Project *n* gets `base = PORT_RANGE_START + n * PORT_BLOCK` (defaults `54300`, `100`):

| Service | Port |
| --- | --- |
| Shadow DB | base + 20 |
| API (Kong) | base + 21 |
| Postgres | base + 22 |
| Studio | base + 23 |
| Mailpit | base + 24 |
| Analytics | base + 27 |
| Pooler | base + 29 |

Edge Runtime inspector ports start at `INSPECTOR_PORT_START` (default `8083`).

Block 0 matches the stock Supabase defaults (54321, 54322, ...).

## Run locally

```bash
make dev            # API :8080 (go run) + Vite :5173
make run-manager    # API :8080 with air live reload
make build && ./bin/supabase-manager   # single binary with the UI embedded
```

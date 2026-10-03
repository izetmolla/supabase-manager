# Ports

Supabase Manager is a single service: the API, the embedded UI and the project service proxy all
listen on one address. There is no gateway.

| Port | Service | Env | Notes |
|-----:|---------|-----|-------|
| **8080** | manager (`services/manager`) | `ADDR` (default `127.0.0.1:8080`) | API (`/api`), UI (embedded), project proxy (`/proxy/<slug>/...`, `/project/<slug>` Studio) |
| **5173** | Vite dev server (`frontend/apps/manager`) | — | Development only; proxies `/api`, `/proxy`, Studio paths to `:8080` (`VITE_API_PROXY_TARGET`) |

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

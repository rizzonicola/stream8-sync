# Stream8 Sync Server

[![GitHub Repo](https://img.shields.io/badge/GitHub-rizzonicola%2Fstream8--sync-181717?logo=github&logoColor=white)](https://github.com/rizzonicola/stream8-sync)
[![Profile](https://img.shields.io/badge/%40rizzonicola-181717?logo=github&logoColor=white&label=)](https://github.com/rizzonicola)

Optional, self-hosted sync server for [Stream8](../stream8) watch history.
A single Go binary (SQLite storage via `modernc.org/sqlite`, no CGO), with a
small embedded admin web UI to manage API keys.

The full wire protocol is documented in **[server.md](./server.md)** —
useful both to understand how sync works and as a reference for anyone
writing an alternative compatible client or server.

## Features

- **Account isolation** — each API key maps to one account; histories are
  never mixed. Keys are stored only as SHA-256 hashes, never in plaintext.
- **SQLite storage** (WAL mode) — a single `stream8.db` file, no external
  database to install or manage.
- **Tombstone-aware sync protocol** — deletions propagate correctly across
  devices instead of resurfacing on the next sync (see `server.md` §6.1).
- **Admin UI in Italian / English / French / System** — language switcher
  in the top-right corner; API error messages are localized too (via the
  `X-Stream8-Lang` header or the browser's `Accept-Language`).
- **Structured logging** — every sync request (success or failure) is
  logged server-side with account id and entry counts, making
  intermittent sync issues easy to diagnose.

## Quick start (Docker)

```bash
docker compose up -d --build
```

Then open `http://localhost:8080`. Set `ADMIN_USER` / `ADMIN_PASSWORD` in
`docker-compose.yml` first — by default the admin UI is **unprotected**
(see below). Create an account, copy the API key shown (it is shown only
once), and paste it into Stream8 → Settings → Sync, together with the API
URL (port `8081`).

## Quick start (no Docker)

```bash
go build -o stream8-sync .
ADMIN_USER=admin ADMIN_PASSWORD=change-me-to-a-real-password ./stream8-sync
```

## Ports

| Port | Purpose |
|---|---|
| `8080` (`WEB_PORT`) | Admin web UI — protect it with `ADMIN_USER`/`ADMIN_PASSWORD` or keep it on a private network |
| `8081` (`API_PORT`) | Sync API used by the Stream8 site — every request requires a Bearer API key |

## Configuration (environment variables)

| Variable | Default | Purpose |
|---|---|---|
| `WEB_PORT` | `8080` | Admin UI port |
| `API_PORT` | `8081` | Sync API port |
| `DATA_DIR` | `./data` | Where `stream8.db` (SQLite) is stored |
| `ADMIN_USER` / `ADMIN_PASSWORD` | *(unset)* | HTTP Basic Auth for the admin UI. Strongly recommended in production |

## Data

Everything is stored in `DATA_DIR` (default `./data`, mounted as a Docker
volume) in a single SQLite database, `stream8.db` (plus its WAL/SHM
sidecar files). No external database service required.

## API summary

| Method & path | Auth | Purpose |
|---|---|---|
| `GET /v1/health` | — | Health/compatibility check |
| `GET /v1/history` | Bearer API key | Returns the full history for the account |
| `PUT /v1/history` | Bearer API key | Replaces the full history for the account (client-side merge, see `server.md`) |

See `server.md` for the complete request/response format, error codes, and
the client-side merge/tombstone algorithm required for correct multi-device
sync.

## Third-party dependencies

A single Go module dependency (plus its own transitive dependencies),
all permissively licensed:

| Module | License |
|---|---|
| [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite) (pure-Go, no CGO) | BSD-3-Clause |
| ↳ `modernc.org/libc`, `gc/v3`, `mathutil`, `memory`, `strutil`, `token` | BSD-3-Clause |
| `github.com/google/uuid` | BSD-3-Clause |
| `github.com/remyoudompheng/bigfft` | BSD-3-Clause |
| `golang.org/x/sys` | BSD-3-Clause |
| `github.com/dustin/go-humanize` | MIT |
| `github.com/mattn/go-isatty` | MIT |
| `github.com/ncruces/go-strftime` | MIT |
| `github.com/hashicorp/golang-lru/v2` | MPL-2.0 |

See `go.sum` for exact versions, and each module's own repository for the
full license text.

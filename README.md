# pglockr

Live visualizer of PostgreSQL locks and blocking trees. A single Go binary with
an embedded React UI that connects to a target PostgreSQL and shows, in real
time, the **wait-for forest** — who is blocking whom, on which object, with which
lock — and lets an on-call engineer cancel or terminate the head blocker in one
click. See [pglockr-spec.md](pglockr-spec.md) for the full design.

This is the **MVP** (spec §9): one target cluster, 1s polling, live WebSocket
stream, cancel/terminate with audit, single static-token auth, in-memory history.

## Build

Requires Go 1.26+ and Node 18+ (for the UI build).

```sh
make build        # builds web/dist then the ./pglockr binary
```

Or with Docker:

```sh
docker build -t pglockr .
```

## Run

Secrets come from the environment, never the config file:

```sh
PGLOCKR_DSN="postgres://pglockr_ro:***@db-host:5432/mydb" \
PGLOCKR_TOKEN="a-strong-token" \
./pglockr -config pglockr.example.yaml
```

Then open http://localhost:8080 and enter the token.

## Local test bench

No database handy? [`dev/`](dev/README.md) ships a throwaway PostgreSQL plus load
scripts that manufacture blocking trees:

```sh
cd dev
make up           # start PostgreSQL on :55432
make pglockr      # run pglockr against it (token: dev)
make load-chain   # manufacture a 3-deep blocking tree
make load-reset   # clear it
```

## Database roles

pglockr needs `pg_monitor` to read other backends' query texts and
`pg_signal_backend` to cancel/terminate them. Provisioning script:
[scripts/grants.sql](scripts/grants.sql).

> Superuser backends cannot be cancelled/terminated via `pg_signal_backend`
> (a PostgreSQL restriction); the UI surfaces this.

## HTTP API (MVP subset)

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET  | `/healthz` | none | liveness + connection status |
| GET  | `/api/clusters` | token | cluster + poll status |
| GET  | `/api/snapshot?cluster=NAME[&at=RFC3339]` | token | current/nearest forest |
| WS   | `/api/stream?cluster=NAME` | token | live snapshot stream |
| POST | `/api/sessions/{pid}/cancel` | token | `pg_cancel_backend` |
| POST | `/api/sessions/{pid}/terminate` | token | `pg_terminate_backend` |

## Project layout

```
cmd/pglockr      entrypoint, config load, wiring
internal/config  YAML + env config
internal/pg      version-aware queries, cancel/terminate
internal/graph   wait-for forest builder
internal/store   in-memory ring buffer + pub/sub
internal/poller  snapshot loop with backoff
internal/signal  audited actions
internal/auth    static-token middleware
internal/api     REST + WebSocket handlers
web/             React + Vite UI, embedded via go:embed
```

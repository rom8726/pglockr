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

## History persistence

By default snapshot history lives only in an in-memory ring buffer (~5 min),
which resets on restart. Set `PGLOCKR_DB_PATH` to a writable file to persist
history to SQLite (pure-Go driver, no CGO) so the scrubber survives restarts and
extends beyond the ring:

```sh
PGLOCKR_DB_PATH=/var/lib/pglockr/history.db ./pglockr ...
```

Retention defaults to 24h (`persist.retention` in the config; `0` keeps
forever). Running as a sidecar in Kubernetes, point `PGLOCKR_DB_PATH` at a
mounted volume (PVC) so history outlives pod restarts.

## Database roles & first-run setup

pglockr needs `pg_monitor` to read other backends' query texts and
`pg_signal_backend` to cancel/terminate them. Generate a provisioning script for
your own role and apply it as a superuser:

```sh
# Prints SQL to stdout (a strong password is generated and shown on stderr).
pglockr grants --role pglockr_ro | psql "postgres://postgres@HOST:5432/DBNAME"
```

`pglockr grants` flags: `--role`, `--password` (default: generated),
`--no-signal` (read-only viewer, no cancel/terminate). The static
[scripts/grants.sql](scripts/grants.sql) is a reference equivalent.

On startup pglockr runs a **preflight check** of the connected role and, if it
is missing privileges, logs a warning and prints the exact `GRANT` statements to
fix it — it still runs (degraded) so you can see what to grant.

> Superuser backends cannot be cancelled/terminated via `pg_signal_backend`
> (a PostgreSQL restriction); the UI surfaces this.

## HTTP API

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET  | `/healthz` | none | liveness + connection status |
| GET  | `/api/clusters` | token | cluster + poll status |
| GET  | `/api/snapshot?cluster=NAME[&at=RFC3339]` | token | current/nearest forest |
| GET  | `/api/history?cluster=NAME[&from&to]` | token | retained snapshot metadata (scrubber) |
| WS   | `/api/stream?cluster=NAME` | token | live snapshot stream |
| GET  | `/api/locks?cluster=NAME` | token | lock inspector (raw `pg_locks`) |
| GET  | `/api/hot-objects?cluster=NAME` | token | most contended relations |
| POST | `/api/sessions/{pid}/cancel` | token | `pg_cancel_backend` |
| POST | `/api/sessions/{pid}/terminate` | token | `pg_terminate_backend` |

## Project layout

```
cmd/pglockr      entrypoint, config load, wiring
internal/config  YAML + env config
internal/pg      version-aware queries, cancel/terminate
internal/graph   wait-for forest builder
internal/store   in-memory ring buffer + pub/sub
internal/persist SQLite durable history (optional)
internal/poller  snapshot loop with backoff
internal/signal  audited actions
internal/auth    static-token middleware
internal/setup   GRANT script generator + preflight remediation
internal/api     REST + WebSocket handlers
web/             React + Vite UI, embedded via go:embed
```

## Tests

```sh
go test ./...                 # fast unit tests (no database needed)
```

Integration tests (build tag `integration`) run the pg layer against a real
PostgreSQL; they skip unless `PGLOCKR_TEST_DSN` is set:

```sh
PGLOCKR_TEST_DSN="postgres://postgres:postgres@localhost:55432/app?sslmode=disable" \
  go test -tags=integration -race ./...
```

The [`dev/`](dev/README.md) bench is a convenient target (`cd dev && make up`).
CI (`.github/workflows/ci.yml`) runs gofmt + vet + the full `-race` suite against
a Postgres service container, and builds the UI.

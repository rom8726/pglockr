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

## Authentication & roles

Access is role-based: **viewer** (read-only — forest, history, locks, hot
objects), **operator** (+ cancel/terminate), **admin** (+ audit/config,
reserved). Every action is attributed to its principal in the audit log.

Principals are configured with their token in a per-user env var:

```yaml
auth:
  principals:
    - { name: oncall,    role: operator, tokenEnv: PGLOCKR_TOKEN_ONCALL }
    - { name: dashboard, role: viewer,   tokenEnv: PGLOCKR_TOKEN_DASHBOARD }
```

`PGLOCKR_TOKEN`, if set, is a single **admin** token (backward compatible). The
UI is role-aware (viewers don't see cancel/terminate) and `GET /api/me` returns
the current principal.

**SSO via a trusted proxy** (`auth.mode: proxy`) — offload authentication to an
upstream proxy (oauth2-proxy/Istio/Pomerium) that injects the user + groups as
headers; pglockr maps groups to roles:

```yaml
auth:
  mode: proxy
  proxy:
    trustMode: secret          # require a shared-secret header (forgery-proof);
                               # or "network" when pglockr is reachable only via the proxy
    secretEnv: PGLOCKR_PROXY_SECRET
    userHeader: X-Forwarded-Email
    groupsHeader: X-Forwarded-Groups
    roleMappings:
      - { group: pglockr-admins, role: admin }
      - { group: pglockr-ops,    role: operator }
      - { group: pglockr-viewers, role: viewer }
```

With `trustMode: secret`, identity headers are ignored unless the proxy presents
the shared secret, so a client reaching pglockr directly can't forge identity.
A runnable oauth2-proxy + OIDC demo is in [dev/sso/](dev/sso/README.md). In proxy
mode the UI shows no login form — the proxy has already authenticated.

### Audit trail

Every cancel/terminate is recorded with its principal, target PID, the victim's
query, and the outcome. With SQLite persistence enabled the trail is durable
(survives restarts) and intentionally exempt from history retention pruning;
without it, the last 1000 entries are kept in memory. Admins can read it via
`GET /api/audit` or the **Audit** tab.

### Query-text redaction

pglockr reads other backends' query texts, which often contain sensitive values.
Set `redaction.enabled: true` (or `PGLOCKR_REDACT=1`) to mask literals at
ingestion — `UPDATE accounts SET email = 'bob@x.io' WHERE id = 42` becomes
`UPDATE accounts SET email = ? WHERE id = ?`. Masking happens before a snapshot
reaches the ring buffer, persistent history, the live stream, or the audit log,
so raw texts never leave the database.

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
| GET  | `/api/me` | viewer | current principal (name + role) |
| GET  | `/api/clusters` | viewer | cluster + poll status |
| GET  | `/api/snapshot?cluster=NAME[&at=RFC3339]` | viewer | current/nearest forest |
| GET  | `/api/history?cluster=NAME[&from&to]` | viewer | retained snapshot metadata (scrubber) |
| WS   | `/api/stream?cluster=NAME` | viewer | live snapshot stream |
| GET  | `/api/locks?cluster=NAME` | viewer | lock inspector (raw `pg_locks`) |
| GET  | `/api/hot-objects?cluster=NAME` | viewer | most contended relations |
| POST | `/api/sessions/{pid}/cancel` | operator | `pg_cancel_backend` |
| POST | `/api/sessions/{pid}/terminate` | operator | `pg_terminate_backend` |
| GET  | `/api/audit?limit=N` | admin | recent actions, newest first |

## Project layout

```
cmd/pglockr      entrypoint, config load, wiring
internal/config  YAML + env config
internal/pg      version-aware queries, cancel/terminate
internal/graph   wait-for forest builder
internal/store   in-memory ring buffer + pub/sub
internal/persist SQLite durable history + audit (optional)
internal/audit   action trail types + in-memory sink
internal/redact  SQL literal masking (ingestion-time redaction)
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

# pglockr dev bench

A throwaway PostgreSQL plus load scripts that manufacture blocking trees, so you
can run pglockr against something live without a real database.

## Requirements

Docker (Compose v2) and Go (for `make pglockr`). No local `psql` needed — all SQL
runs inside the container.

## Quick start

```sh
cd dev
make up            # start PostgreSQL on localhost:55432
make pglockr       # in another terminal: run pglockr against it (token: dev)
make load-chain    # manufacture a 3-deep blocking tree
make tree          # see it straight from the DB
# ...open http://localhost:8080 (token "dev") to see it in the UI...
make load-reset    # clear the load
make down          # stop and wipe the database
```

`make help` lists everything.

## What's in the box

- **`docker-compose.yml`** — `postgres:16` on host port **55432**, tuned with
  `log_lock_waits=on` and `deadlock_timeout=1s` so waits/deadlocks show fast.
- **`init/00-init.sql`** — runs once on first start: creates role `pglockr_ro`
  (`pg_monitor` + `pg_signal_backend`), a non-superuser `appuser` for the load,
  and a seeded `accounts` table.
- **`scenarios/`** — load generators. Each session is a detached `psql` holding a
  row lock and sleeping, tagged `application_name=loadgen-*` so `load-reset` can
  find and terminate them.

## DSN

```
postgres://pglockr_ro:pglockr@localhost:55432/app?sslmode=disable
```

`appuser` is deliberately **not** a superuser so pglockr's `pg_signal_backend`
role can actually cancel/terminate the load sessions (superuser backends are not
signalable).

## Scenarios

| `make` target      | Shape                                                        |
|--------------------|-------------------------------------------------------------|
| `load-simple`      | one idle-in-transaction holder ← one waiter                 |
| `load-chain`       | `leaf → mid → root` (mid is blocked *and* blocking)         |
| `load-fanout`      | one root with several waiters queued behind it              |
| `load-forest`      | two independent trees (two roots) at once                   |
| `load-deadlock`    | crossing locks; PostgreSQL aborts one victim within ~1–2s   |
| `load-reset`       | terminate all load sessions, clearing every lock            |

Stack scenarios for a busier graph (e.g. `make load-chain load-forest`), then
`make tree` or watch it live in the UI. `make logs` tails lock-wait / deadlock
log lines.

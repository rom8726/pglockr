#!/usr/bin/env bash
# Shared helpers for load scenarios. Each "session" is a detached psql process
# inside the db container that holds/waits on a lock and then sleeps, so the
# blocking situation persists until `make load-reset` (or the sleep expires).
set -euo pipefail

DEV_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE=(docker compose -f "$DEV_DIR/docker-compose.yml")

# spawn <name> <psql-args...>
# Runs a detached appuser session tagged application_name=loadgen-<name>.
spawn() {
  local name="$1"; shift
  "${COMPOSE[@]}" exec -d -e PGAPPNAME="loadgen-${name}" db \
    psql -U appuser -d app "$@"
}

# pause briefly so locks are acquired in a deterministic order.
settle() { sleep "${1:-1}"; }

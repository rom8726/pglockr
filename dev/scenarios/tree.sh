#!/usr/bin/env bash
# Print the current blocking situation straight from the database (no pglockr),
# useful for sanity-checking a scenario.
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

"${COMPOSE[@]}" exec -T db psql -U postgres -d app -c "
SELECT
    a.pid,
    a.application_name           AS app,
    a.state,
    a.wait_event_type            AS wait_type,
    pg_blocking_pids(a.pid)      AS blocked_by,
    left(a.query, 48)            AS query
FROM pg_stat_activity a
WHERE a.backend_type = 'client backend'
  AND a.application_name LIKE 'loadgen-%'
ORDER BY a.pid;"

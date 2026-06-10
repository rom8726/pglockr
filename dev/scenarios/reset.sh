#!/usr/bin/env bash
# Terminate every load-generated session, clearing all locks. Runs as the
# superuser so it can kill any loadgen-* backend regardless of state.
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

"${COMPOSE[@]}" exec -T db psql -U postgres -d app -v ON_ERROR_STOP=1 -c "
SELECT pid, application_name, pg_terminate_backend(pid) AS terminated
FROM pg_stat_activity
WHERE application_name LIKE 'loadgen-%';"

echo "load-reset: all loadgen-* sessions terminated."

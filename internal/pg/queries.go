package pg

// snapshotSQL fetches every client backend plus the PIDs that really block it.
// pg_blocking_pids is the source of truth for the graph structure (spec 5.4).
//
// WaitStart (pg_locks.waitstart) is PG14+. To keep one query across versions we
// pull the earliest waitstart for the backend from pg_locks where not granted;
// on PG13 this column does not exist, so versionedSnapshotSQL swaps it out.
const snapshotSQL = `
SELECT
    a.pid,
    a.usename,
    a.application_name,
    a.client_addr,
    a.state,
    a.wait_event_type,
    a.wait_event,
    a.backend_type,
    a.xact_start,
    a.query_start,
    (SELECT min(l.waitstart) FROM pg_locks l WHERE l.pid = a.pid AND NOT l.granted) AS wait_start,
    a.query,
    pg_blocking_pids(a.pid) AS blocked_by
FROM pg_stat_activity a
WHERE a.backend_type = 'client backend'
  AND a.pid <> pg_backend_pid()`

// snapshotSQLNoWaitstart is the PG13 fallback: pg_locks.waitstart is absent, so
// wait duration is approximated from query_start by the caller.
const snapshotSQLNoWaitstart = `
SELECT
    a.pid,
    a.usename,
    a.application_name,
    a.client_addr,
    a.state,
    a.wait_event_type,
    a.wait_event,
    a.backend_type,
    a.xact_start,
    a.query_start,
    NULL::timestamptz AS wait_start,
    a.query,
    pg_blocking_pids(a.pid) AS blocked_by
FROM pg_stat_activity a
WHERE a.backend_type = 'client backend'
  AND a.pid <> pg_backend_pid()`

// edgeLabelsSQL enriches edges with the contended object and conflicting modes.
// Structure comes from pg_blocking_pids; this only labels confirmed pairs, so
// the caller filters the result by known (waiter, blocker) pairs (spec 5.5).
const edgeLabelsSQL = `
SELECT
    w.pid                       AS waiter_pid,
    b.pid                       AS blocker_pid,
    w.locktype,
    w.mode                      AS waiter_mode,
    b.mode                      AS blocker_mode,
    w.relation::regclass::text  AS relation
FROM pg_locks w
JOIN pg_locks b
  ON  w.locktype      = b.locktype
  AND w.database      IS NOT DISTINCT FROM b.database
  AND w.relation      IS NOT DISTINCT FROM b.relation
  AND w.page          IS NOT DISTINCT FROM b.page
  AND w.tuple         IS NOT DISTINCT FROM b.tuple
  AND w.virtualxid    IS NOT DISTINCT FROM b.virtualxid
  AND w.transactionid IS NOT DISTINCT FROM b.transactionid
  AND w.classid       IS NOT DISTINCT FROM b.classid
  AND w.objid         IS NOT DISTINCT FROM b.objid
  AND w.objsubid      IS NOT DISTINCT FROM b.objsubid
  AND w.pid <> b.pid
WHERE NOT w.granted
  AND b.granted`

// versionNumSQL returns server_version_num (e.g. 160004 for 16.4).
const versionNumSQL = `SHOW server_version_num`

-- pglockr role provisioning (spec section 7).
--
-- This is a STATIC reference. Prefer `pglockr grants` to generate an equivalent
-- script with your own role name and a strong random password:
--   pglockr grants --role pglockr_ro | psql "postgres://postgres@HOST:5432/DB"
--
-- Two least-privilege roles:
--   pglockr_ro  — polling: read all stats / query texts (pg_monitor).
--   pglockr_op  — actions: cancel/terminate other backends (pg_signal_backend).
--
-- On MVP pglockr uses a single DSN; point it at a role that has BOTH
-- memberships (e.g. pglockr_ro granted pg_signal_backend), or use pglockr_op.
-- Replace the passwords before running in any real environment.

-- Read-only polling role.
CREATE ROLE pglockr_ro LOGIN PASSWORD 'change-me-ro';
GRANT pg_monitor TO pglockr_ro;            -- read pg_stat_activity query texts + stats
GRANT pg_signal_backend TO pglockr_ro;     -- MVP single-DSN: allow actions too

-- Optional dedicated action role (use when you split read vs act DSNs later).
CREATE ROLE pglockr_op LOGIN PASSWORD 'change-me-op';
GRANT pg_signal_backend TO pglockr_op;

-- Note: superuser backends cannot be cancelled/terminated via pg_signal_backend
-- (PostgreSQL restriction); pglockr surfaces this in the UI.

-- Provisioning for the pglockr dev bench. Runs once on first container start.

-- pglockr's polling/action role: read all stats (query texts) + signal backends.
CREATE ROLE pglockr_ro LOGIN PASSWORD 'pglockr';
GRANT pg_monitor TO pglockr_ro;
GRANT pg_signal_backend TO pglockr_ro;

-- Non-superuser application role that the load scripts run as. It must be a
-- non-superuser so pglockr_ro (pg_signal_backend) can actually cancel/terminate
-- it — superuser backends are not signalable.
CREATE ROLE appuser LOGIN PASSWORD 'app';

-- A tiny workload table. Row locks on these ids are what the scenarios contend.
CREATE TABLE accounts (
    id      int PRIMARY KEY,
    owner   text NOT NULL,
    balance numeric NOT NULL DEFAULT 0
);
INSERT INTO accounts (id, owner, balance)
SELECT g, 'acct-' || g, g * 100
FROM generate_series(1, 10) AS g;

GRANT SELECT, INSERT, UPDATE, DELETE ON accounts TO appuser;

-- Hold a lock on accounts.:hold, then request accounts.:wait. The session ends
-- up holding :hold while (typically) blocking on :wait — the mid-chain shape.
--
-- Usage: psql -v hold=2 -v wait=1 -f lock_two.sql
\set ON_ERROR_STOP on
BEGIN;
UPDATE accounts SET balance = balance + 1 WHERE id = :hold;
\echo holding row :hold, now requesting row :wait
UPDATE accounts SET balance = balance + 1 WHERE id = :wait;
\echo acquired row :wait
\! sleep 86400
ROLLBACK;

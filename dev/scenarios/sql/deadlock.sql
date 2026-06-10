-- Deadlock half: lock accounts.:first, pause, then request accounts.:second.
-- Run two of these with swapped (first, second) at the same time and PostgreSQL
-- detects the cycle within deadlock_timeout and aborts one victim.
--
-- Usage: psql -v first=1 -v second=2 -f deadlock.sql
\set ON_ERROR_STOP off
BEGIN;
UPDATE accounts SET balance = balance + 1 WHERE id = :first;
\! sleep 2
UPDATE accounts SET balance = balance + 1 WHERE id = :second;
\! sleep 86400
ROLLBACK;

-- Acquire a row lock on accounts.:id and hold it. If the row is already locked
-- by another session, this UPDATE blocks (becoming a waiter); otherwise it
-- holds the lock and sits idle-in-transaction at the \! sleep.
--
-- Usage: psql -v id=1 -f lock_row.sql
\set ON_ERROR_STOP on
BEGIN;
UPDATE accounts SET balance = balance + 1 WHERE id = :id;
\echo holding/locking row :id
\! sleep 86400
ROLLBACK;

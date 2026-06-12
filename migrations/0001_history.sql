-- Reference schema for the SQLite history database (internal/persist).
-- pglockr creates this automatically on startup via CREATE TABLE IF NOT EXISTS;
-- this file documents the shape. Each row is one snapshot, with summary columns
-- for fast timeline queries and a gzip(JSON) blob of the full snapshot.

CREATE TABLE IF NOT EXISTS snapshots (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster  TEXT    NOT NULL,
    taken_at INTEGER NOT NULL,   -- unix nanoseconds
    roots    INTEGER NOT NULL,
    edges    INTEGER NOT NULL,
    sessions INTEGER NOT NULL,
    data     BLOB    NOT NULL    -- gzip(JSON(snapshot))
);

CREATE INDEX IF NOT EXISTS idx_snapshots_taken_at ON snapshots(taken_at);

-- Audit trail (immutable; not subject to history retention pruning).
CREATE TABLE IF NOT EXISTS audit (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    at           INTEGER NOT NULL,   -- unix nanoseconds
    actor        TEXT    NOT NULL,
    action       TEXT    NOT NULL,
    pid          INTEGER NOT NULL,
    victim_query TEXT    NOT NULL,
    delivered    INTEGER NOT NULL,
    error        TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_at ON audit(at);

package persist

import (
	"context"
	"time"

	"github.com/rom8726/pglockr/internal/audit"
)

const auditSchema = `
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
CREATE INDEX IF NOT EXISTS idx_audit_at ON audit(at);`

// initAudit creates the audit table; called from Open.
func (s *SQLite) initAudit(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, auditSchema)
	return err
}

// Record persists one audit entry. The audit trail is immutable: entries are
// never updated and intentionally not covered by history retention pruning.
func (s *SQLite) Record(e audit.Entry) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit (at, actor, action, pid, victim_query, delivered, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.At.UnixNano(), e.Actor, e.Action, e.PID, e.VictimQuery, boolToInt(e.Delivered), e.Error,
	)
	return err
}

// Recent returns up to limit audit entries, newest first.
func (s *SQLite) Recent(limit int) ([]audit.Entry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(ctx,
		`SELECT at, actor, action, pid, victim_query, delivered, error
		 FROM audit ORDER BY at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]audit.Entry, 0, limit)
	for rows.Next() {
		var (
			e         audit.Entry
			ts        int64
			delivered int
		)
		if err := rows.Scan(&ts, &e.Actor, &e.Action, &e.PID, &e.VictimQuery, &delivered, &e.Error); err != nil {
			return nil, err
		}
		e.At = time.Unix(0, ts)
		e.Delivered = delivered != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

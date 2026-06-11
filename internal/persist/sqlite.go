// Package persist provides durable snapshot history backed by SQLite, so the
// history scrubber survives process/pod restarts and can extend beyond the
// in-memory ring buffer. It uses the pure-Go modernc.org/sqlite driver to keep
// the binary CGO-free and statically linkable.
package persist

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rom8726/pglockr/internal/graph"
	"github.com/rom8726/pglockr/internal/store"
)

// SQLite persists snapshots to a SQLite database. It satisfies store.Persister.
type SQLite struct {
	db        *sql.DB
	retention time.Duration
	log       *slog.Logger

	mu        sync.Mutex
	lastPrune time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS snapshots (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster  TEXT    NOT NULL,
    taken_at INTEGER NOT NULL,   -- unix nanoseconds
    roots    INTEGER NOT NULL,
    edges    INTEGER NOT NULL,
    sessions INTEGER NOT NULL,
    data     BLOB    NOT NULL    -- gzip(JSON(snapshot))
);
CREATE INDEX IF NOT EXISTS idx_snapshots_taken_at ON snapshots(taken_at);`

// Open opens (creating if needed) a SQLite history database at path. retention,
// if > 0, bounds how far back snapshots are kept; older rows are pruned lazily.
func Open(path string, retention time.Duration, log *slog.Logger) (*SQLite, error) {
	// WAL mode keeps the 1/s writes cheap and non-blocking for readers.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	// modernc.org/sqlite is safe for concurrent use but a single writer avoids
	// SQLITE_BUSY churn; the poller writes serially anyway.
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return &SQLite{db: db, retention: retention, log: log}, nil
}

// Close releases the database.
func (s *SQLite) Close() error { return s.db.Close() }

// Save persists one snapshot and lazily prunes expired rows.
func (s *SQLite) Save(snap graph.Snapshot) error {
	blob, err := encode(snap)
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO snapshots (cluster, taken_at, roots, edges, sessions, data)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		snap.Cluster, snap.TakenAt.UnixNano(), len(snap.Roots), len(snap.Edges), len(snap.Sessions), blob,
	)
	if err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}
	s.maybePrune(ctx)
	return nil
}

// At returns the persisted snapshot whose taken_at is closest to t.
func (s *SQLite) At(t time.Time) (graph.Snapshot, bool, error) {
	target := t.UnixNano()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Nearest neighbour via the index: closest below and closest at/above.
	var (
		bestData []byte
		bestDiff = int64(math.MaxInt64)
		found    bool
	)
	consider := func(query string) error {
		var data []byte
		var ts int64
		err := s.db.QueryRowContext(ctx, query, target).Scan(&data, &ts)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		diff := ts - target
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			bestDiff, bestData, found = diff, data, true
		}
		return nil
	}
	if err := consider(`SELECT data, taken_at FROM snapshots WHERE taken_at <= ? ORDER BY taken_at DESC LIMIT 1`); err != nil {
		return graph.Snapshot{}, false, err
	}
	if err := consider(`SELECT data, taken_at FROM snapshots WHERE taken_at >= ? ORDER BY taken_at ASC LIMIT 1`); err != nil {
		return graph.Snapshot{}, false, err
	}
	if !found {
		return graph.Snapshot{}, false, nil
	}
	snap, err := decode(bestData)
	if err != nil {
		return graph.Snapshot{}, false, err
	}
	return snap, true, nil
}

// History returns metadata for persisted snapshots in [from, to] (zero bounds
// are treated as unbounded), oldest first.
func (s *SQLite) History(from, to time.Time) ([]store.Meta, error) {
	lo := int64(math.MinInt64)
	hi := int64(math.MaxInt64)
	if !from.IsZero() {
		lo = from.UnixNano()
	}
	if !to.IsZero() {
		hi = to.UnixNano()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(ctx,
		`SELECT taken_at, roots, edges, sessions FROM snapshots
		 WHERE taken_at BETWEEN ? AND ? ORDER BY taken_at ASC`, lo, hi)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]store.Meta, 0)
	for rows.Next() {
		var ts int64
		var m store.Meta
		if err := rows.Scan(&ts, &m.Roots, &m.Edges, &m.Sessions); err != nil {
			return nil, err
		}
		m.TakenAt = time.Unix(0, ts)
		out = append(out, m)
	}
	return out, rows.Err()
}

// maybePrune deletes expired rows at most once per minute. Must be called with a
// live context; errors are logged, not returned (pruning is best-effort).
func (s *SQLite) maybePrune(ctx context.Context) {
	if s.retention <= 0 {
		return
	}
	s.mu.Lock()
	if time.Since(s.lastPrune) < time.Minute {
		s.mu.Unlock()
		return
	}
	s.lastPrune = time.Now()
	s.mu.Unlock()

	cutoff := time.Now().Add(-s.retention).UnixNano()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM snapshots WHERE taken_at < ?`, cutoff); err != nil && s.log != nil {
		s.log.Warn("history prune failed", "err", err)
	}
}

func encode(snap graph.Snapshot) ([]byte, error) {
	raw, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decode(blob []byte) (graph.Snapshot, error) {
	zr, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return graph.Snapshot{}, err
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		return graph.Snapshot{}, err
	}
	var snap graph.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return graph.Snapshot{}, err
	}
	return snap, nil
}

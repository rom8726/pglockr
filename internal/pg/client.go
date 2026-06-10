// Package pg is the version-aware data-access layer to the target PostgreSQL
// cluster: it polls activity and locks and executes cancel/terminate signals.
package pg

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rom8726/pglockr/internal/graph"
)

// Client wraps a pgx connection pool to one target cluster and adapts queries
// to the detected server version.
type Client struct {
	pool             *pgxpool.Pool
	statementTimeout time.Duration

	// versionNum is server_version_num, e.g. 160004. Zero until Connect.
	versionNum int
}

// Connect opens a pool to dsn and detects the server version. The caller owns
// the returned Client and must Close it.
func Connect(ctx context.Context, dsn string, statementTimeout time.Duration) (*Client, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	// A small pool is plenty: the poller uses one short-lived query at a time.
	cfg.MaxConns = 4
	cfg.ConnConfig.RuntimeParams["application_name"] = "pglockr"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect pool: %w", err)
	}

	c := &Client{pool: pool, statementTimeout: statementTimeout}
	if err := c.detectVersion(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return c, nil
}

// Close releases the connection pool.
func (c *Client) Close() { c.pool.Close() }

// ServerVersionNum returns the detected server_version_num (e.g. 160004).
func (c *Client) ServerVersionNum() int { return c.versionNum }

// Ping verifies connectivity for health checks.
func (c *Client) Ping(ctx context.Context) error { return c.pool.Ping(ctx) }

func (c *Client) detectVersion(ctx context.Context) error {
	var s string
	if err := c.pool.QueryRow(ctx, versionNumSQL).Scan(&s); err != nil {
		return fmt.Errorf("detect version: %w", err)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("parse server_version_num %q: %w", s, err)
	}
	c.versionNum = n
	return nil
}

// hasWaitstart reports whether pg_locks.waitstart exists (PG14+).
func (c *Client) hasWaitstart() bool { return c.versionNum >= 140000 }

// Snapshot polls the target once and returns sessions (keyed by PID, with
// BlockedBy populated) and the labels for confirmed blocking edges.
func (c *Client) Snapshot(ctx context.Context) (map[int]graph.Session, map[graph.EdgeKey]graph.LockLabel, error) {
	ctx, cancel := context.WithTimeout(ctx, c.statementTimeout)
	defer cancel()

	sessions, err := c.querySessions(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("query sessions: %w", err)
	}
	labels, err := c.queryEdgeLabels(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("query edge labels: %w", err)
	}
	return sessions, labels, nil
}

func (c *Client) querySessions(ctx context.Context) (map[int]graph.Session, error) {
	q := snapshotSQL
	if !c.hasWaitstart() {
		q = snapshotSQLNoWaitstart
	}
	rows, err := c.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make(map[int]graph.Session)
	for rows.Next() {
		var (
			s          graph.Session
			usename    *string
			appName    *string
			clientAddr *net.IPNet
			state      *string
			wet, we    *string
			backend    *string
			xactStart  *time.Time
			queryStart *time.Time
			waitStart  *time.Time
			query      *string
			blockedBy  []int32
		)
		if err := rows.Scan(
			&s.PID, &usename, &appName, &clientAddr, &state,
			&wet, &we, &backend, &xactStart, &queryStart,
			&waitStart, &query, &blockedBy,
		); err != nil {
			return nil, err
		}
		s.User = deref(usename)
		s.AppName = deref(appName)
		if clientAddr != nil {
			s.ClientAddr = clientAddr.IP.String()
		}
		s.State = deref(state)
		s.WaitEventType = deref(wet)
		s.WaitEvent = deref(we)
		s.BackendType = deref(backend)
		s.XactStart = derefTime(xactStart)
		s.QueryStart = derefTime(queryStart)
		s.WaitStart = derefTime(waitStart)
		s.Query = deref(query)
		s.BlockedBy = toIntSlice(blockedBy)
		sessions[s.PID] = s
	}
	return sessions, rows.Err()
}

func (c *Client) queryEdgeLabels(ctx context.Context) (map[graph.EdgeKey]graph.LockLabel, error) {
	rows, err := c.pool.Query(ctx, edgeLabelsSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	labels := make(map[graph.EdgeKey]graph.LockLabel)
	for rows.Next() {
		var (
			waiter, blocker        int
			lockType, wMode, bMode string
			relation               *string
		)
		if err := rows.Scan(&waiter, &blocker, &lockType, &wMode, &bMode, &relation); err != nil {
			return nil, err
		}
		labels[graph.EdgeKey{WaiterPID: waiter, BlockerPID: blocker}] = graph.LockLabel{
			LockType:    lockType,
			Relation:    deref(relation),
			WaiterMode:  wMode,
			BlockerMode: bMode,
		}
	}
	return labels, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func toIntSlice(in []int32) []int {
	if len(in) == 0 {
		return nil
	}
	out := make([]int, len(in))
	for i, v := range in {
		out[i] = int(v)
	}
	return out
}

package pg

import (
	"context"
	"fmt"
)

// Cancel runs pg_cancel_backend(pid): a soft cancel of the backend's current
// query. The returned bool is PostgreSQL's report of whether the signal was
// sent (false typically means the PID no longer exists or is not signalable).
func (c *Client) Cancel(ctx context.Context, pid int) (bool, error) {
	return c.signal(ctx, "pg_cancel_backend", pid)
}

// Terminate runs pg_terminate_backend(pid): drops the connection entirely.
func (c *Client) Terminate(ctx context.Context, pid int) (bool, error) {
	return c.signal(ctx, "pg_terminate_backend", pid)
}

func (c *Client) signal(ctx context.Context, fn string, pid int) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, c.statementTimeout)
	defer cancel()

	var ok bool
	// fn is a fixed internal constant, never user input — safe to interpolate.
	sql := fmt.Sprintf("SELECT %s($1)", fn)
	if err := c.pool.QueryRow(ctx, sql, pid).Scan(&ok); err != nil {
		return false, fmt.Errorf("%s(%d): %w", fn, pid, err)
	}
	return ok, nil
}

package pg

import (
	"context"
	"fmt"
)

// LockRow is one row of the lock inspector (a single entry from pg_locks).
type LockRow struct {
	LockType string `json:"lockType"`
	Object   string `json:"object"`
	Mode     string `json:"mode"`
	Granted  bool   `json:"granted"`
	PID      int    `json:"pid"`
}

// HotObject is a contended relation with its waiter/holder counts.
type HotObject struct {
	Object  string `json:"object"`
	Waiters int    `json:"waiters"`
	Holders int    `json:"holders"`
}

// Locks returns the current lock inspector view, ordered by object.
func (c *Client) Locks(ctx context.Context) ([]LockRow, error) {
	ctx, cancel := context.WithTimeout(ctx, c.statementTimeout)
	defer cancel()

	rows, err := c.pool.Query(ctx, locksSQL)
	if err != nil {
		return nil, fmt.Errorf("query locks: %w", err)
	}
	defer rows.Close()

	out := make([]LockRow, 0)
	for rows.Next() {
		var r LockRow
		if err := rows.Scan(&r.LockType, &r.Object, &r.Mode, &r.Granted, &r.PID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// HotObjects returns the most contended relations (those with at least one
// waiter), most-contended first.
func (c *Client) HotObjects(ctx context.Context) ([]HotObject, error) {
	ctx, cancel := context.WithTimeout(ctx, c.statementTimeout)
	defer cancel()

	rows, err := c.pool.Query(ctx, hotObjectsSQL)
	if err != nil {
		return nil, fmt.Errorf("query hot objects: %w", err)
	}
	defer rows.Close()

	out := make([]HotObject, 0)
	for rows.Next() {
		var h HotObject
		if err := rows.Scan(&h.Object, &h.Waiters, &h.Holders); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

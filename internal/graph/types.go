// Package graph defines the wait-for forest domain model and the builder that
// turns a raw PostgreSQL lock snapshot into a forest of blocking trees.
package graph

import "time"

// Snapshot is a single point-in-time view of the blocking situation on a target
// cluster. It is the JSON contract shared with the web UI.
type Snapshot struct {
	Cluster  string          `json:"cluster"`
	TakenAt  time.Time       `json:"takenAt"`
	Sessions map[int]Session `json:"sessions"` // keyed by PID
	Edges    []Edge          `json:"edges"`
	Roots    []int           `json:"roots"` // head blockers (hold locks, wait for nobody)
}

// Session is a single client backend on the target cluster, enriched from
// pg_stat_activity. Zero-valued time fields mean "unknown / not applicable".
type Session struct {
	PID           int       `json:"pid"`
	User          string    `json:"user"`
	AppName       string    `json:"appName"`
	ClientAddr    string    `json:"clientAddr"`
	State         string    `json:"state"` // active | idle in transaction | ...
	WaitEventType string    `json:"waitEventType"`
	WaitEvent     string    `json:"waitEvent"`
	BackendType   string    `json:"backendType"`
	XactStart     time.Time `json:"xactStart"`
	QueryStart    time.Time `json:"queryStart"`
	WaitStart     time.Time `json:"waitStart"` // PG14+, zero on older versions
	Query         string    `json:"query"`
	BlockedBy     []int     `json:"blockedBy"` // from pg_blocking_pids
	IsRoot        bool      `json:"isRoot"`    // holds locks, waits for nobody
}

// Edge is a directed blocking relation waiter -> blocker, labelled with the
// contended object and the conflicting lock modes.
type Edge struct {
	WaiterPID   int    `json:"waiterPid"`
	BlockerPID  int    `json:"blockerPid"`
	LockType    string `json:"lockType"`   // relation | tuple | transactionid | ...
	Relation    string `json:"relation"`   // resolved name or OID fallback
	WaiterMode  string `json:"waiterMode"` // e.g. AccessExclusiveLock
	BlockerMode string `json:"blockerMode"`
}

// EdgeKey identifies a (waiter, blocker) pair for matching lock labels to the
// edges confirmed by pg_blocking_pids.
type EdgeKey struct {
	WaiterPID  int
	BlockerPID int
}

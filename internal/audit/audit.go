// Package audit defines the immutable action trail: every cancel/terminate is
// recorded with its principal, target, and outcome. The sink is pluggable — a
// bounded in-memory ring by default, SQLite when persistence is enabled.
package audit

import (
	"sync"
	"time"
)

// Entry is one recorded action.
type Entry struct {
	At          time.Time `json:"at"`
	Actor       string    `json:"actor"`  // principal name
	Action      string    `json:"action"` // cancel | terminate
	PID         int       `json:"pid"`
	VictimQuery string    `json:"victimQuery"`
	Delivered   bool      `json:"delivered"`
	Error       string    `json:"error,omitempty"`
}

// Sink records and serves audit entries.
type Sink interface {
	Record(e Entry) error
	// Recent returns up to limit entries, newest first.
	Recent(limit int) ([]Entry, error)
}

// Memory is a bounded in-memory Sink (newest kept). Used when SQLite
// persistence is not configured; entries are lost on restart.
type Memory struct {
	mu      sync.RWMutex
	entries []Entry // append-order, oldest first
	cap     int
}

// NewMemory returns a Memory sink retaining the last capacity entries.
func NewMemory(capacity int) *Memory {
	if capacity < 1 {
		capacity = 1
	}
	return &Memory{cap: capacity}
}

// Record appends an entry, evicting the oldest beyond capacity.
func (m *Memory) Record(e Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
	if len(m.entries) > m.cap {
		m.entries = m.entries[len(m.entries)-m.cap:]
	}
	return nil
}

// Recent returns up to limit entries, newest first.
func (m *Memory) Recent(limit int) ([]Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := len(m.entries)
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]Entry, 0, limit)
	for i := n - 1; i >= n-limit; i-- {
		out = append(out, m.entries[i])
	}
	return out, nil
}

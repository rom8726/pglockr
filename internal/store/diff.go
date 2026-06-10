package store

import "github.com/rom8726/pglockr/internal/graph"

// Diff describes how the set of sessions changed between two snapshots, keyed
// by PID. It lets the UI update incrementally instead of redrawing everything.
type Diff struct {
	Added   []int `json:"added"`   // PIDs present in cur but not prev
	Removed []int `json:"removed"` // PIDs present in prev but not cur
	Changed []int `json:"changed"` // PIDs whose relevant fields changed
}

// DiffSnapshots compares two snapshots by PID. "Changed" is limited to fields
// that affect the forest view (state, wait event, blockers, root flag, query).
func DiffSnapshots(prev, cur graph.Snapshot) Diff {
	var d Diff
	for pid := range cur.Sessions {
		if _, ok := prev.Sessions[pid]; !ok {
			d.Added = append(d.Added, pid)
		}
	}
	for pid, ps := range prev.Sessions {
		cs, ok := cur.Sessions[pid]
		if !ok {
			d.Removed = append(d.Removed, pid)
			continue
		}
		if sessionChanged(ps, cs) {
			d.Changed = append(d.Changed, pid)
		}
	}
	return d
}

func sessionChanged(a, b graph.Session) bool {
	if a.State != b.State || a.WaitEvent != b.WaitEvent ||
		a.WaitEventType != b.WaitEventType || a.IsRoot != b.IsRoot ||
		a.Query != b.Query {
		return true
	}
	return !equalInts(a.BlockedBy, b.BlockedBy)
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

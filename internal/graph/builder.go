package graph

import (
	"sort"
	"time"
)

// LockLabel describes the contended object and conflicting modes for an edge.
// It is sourced from the edge-labels query and matched onto edges that have
// already been confirmed by pg_blocking_pids.
type LockLabel struct {
	LockType    string
	Relation    string
	WaiterMode  string
	BlockerMode string
}

// Build turns a set of sessions (each with BlockedBy populated from
// pg_blocking_pids) plus optional lock labels into a wait-for forest.
//
// pg_blocking_pids is the source of truth for graph structure: edges come only
// from confirmed (waiter, blocker) pairs. The labels map only enriches edges;
// labels for pairs that pg_blocking_pids did not confirm are ignored, so we
// never draw a false edge where no real conflict exists.
func Build(cluster string, takenAt time.Time, sessions map[int]Session, labels map[EdgeKey]LockLabel) Snapshot {
	edges := make([]Edge, 0)
	isBlocker := make(map[int]bool) // PID appears as someone's blocker

	for pid, s := range sessions {
		for _, blocker := range s.BlockedBy {
			isBlocker[blocker] = true
			e := Edge{WaiterPID: pid, BlockerPID: blocker}
			if l, ok := labels[EdgeKey{WaiterPID: pid, BlockerPID: blocker}]; ok {
				e.LockType = l.LockType
				e.Relation = l.Relation
				e.WaiterMode = l.WaiterMode
				e.BlockerMode = l.BlockerMode
			}
			edges = append(edges, e)
		}
	}

	// Roots: head blockers — they block someone but wait for nobody.
	roots := make([]int, 0)
	for pid := range isBlocker {
		s, ok := sessions[pid]
		if !ok {
			continue // blocker not in our session set (e.g. non-client backend)
		}
		if len(s.BlockedBy) == 0 {
			s.IsRoot = true
			sessions[pid] = s
			roots = append(roots, pid)
		}
	}

	// Deterministic ordering for stable diffs and tests.
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].WaiterPID != edges[j].WaiterPID {
			return edges[i].WaiterPID < edges[j].WaiterPID
		}
		return edges[i].BlockerPID < edges[j].BlockerPID
	})
	sort.Ints(roots)

	return Snapshot{
		Cluster:  cluster,
		TakenAt:  takenAt,
		Sessions: sessions,
		Edges:    edges,
		Roots:    roots,
	}
}

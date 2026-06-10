package graph

import (
	"testing"
	"time"
)

func sess(pid int, blockedBy ...int) Session {
	return Session{PID: pid, BlockedBy: blockedBy}
}

func TestBuild_SimpleChain(t *testing.T) {
	// B (200) waits for A (100). A waits for nobody and blocks B -> A is root.
	sessions := map[int]Session{
		100: sess(100),
		200: sess(200, 100),
	}
	labels := map[EdgeKey]LockLabel{
		{WaiterPID: 200, BlockerPID: 100}: {
			LockType: "relation", Relation: "t",
			WaiterMode: "AccessShareLock", BlockerMode: "AccessExclusiveLock",
		},
	}

	snap := Build("c1", time.Now(), sessions, labels)

	if len(snap.Edges) != 1 {
		t.Fatalf("want 1 edge, got %d", len(snap.Edges))
	}
	e := snap.Edges[0]
	if e.WaiterPID != 200 || e.BlockerPID != 100 {
		t.Fatalf("wrong edge: %+v", e)
	}
	if e.Relation != "t" || e.BlockerMode != "AccessExclusiveLock" {
		t.Fatalf("label not attached: %+v", e)
	}
	if len(snap.Roots) != 1 || snap.Roots[0] != 100 {
		t.Fatalf("want root [100], got %v", snap.Roots)
	}
	if !snap.Sessions[100].IsRoot {
		t.Fatalf("session 100 should be flagged IsRoot")
	}
	if snap.Sessions[200].IsRoot {
		t.Fatalf("waiter 200 should not be root")
	}
}

func TestBuild_ForestMultipleRoots(t *testing.T) {
	// Two independent chains: 200->100 and 400->300.
	sessions := map[int]Session{
		100: sess(100),
		200: sess(200, 100),
		300: sess(300),
		400: sess(400, 300),
	}
	snap := Build("c1", time.Now(), sessions, nil)

	if len(snap.Roots) != 2 || snap.Roots[0] != 100 || snap.Roots[1] != 300 {
		t.Fatalf("want roots [100 300], got %v", snap.Roots)
	}
	if len(snap.Edges) != 2 {
		t.Fatalf("want 2 edges, got %d", len(snap.Edges))
	}
}

func TestBuild_MidChainNotRoot(t *testing.T) {
	// 300 -> 200 -> 100. 200 blocks 300 but waits for 100, so 200 is NOT a root.
	sessions := map[int]Session{
		100: sess(100),
		200: sess(200, 100),
		300: sess(300, 200),
	}
	snap := Build("c1", time.Now(), sessions, nil)

	if len(snap.Roots) != 1 || snap.Roots[0] != 100 {
		t.Fatalf("want root [100], got %v", snap.Roots)
	}
	if snap.Sessions[200].IsRoot {
		t.Fatalf("mid-chain 200 must not be root")
	}
}

func TestBuild_NoLabelStillBuildsEdge(t *testing.T) {
	sessions := map[int]Session{
		100: sess(100),
		200: sess(200, 100),
	}
	snap := Build("c1", time.Now(), sessions, nil)
	if len(snap.Edges) != 1 || snap.Edges[0].Relation != "" {
		t.Fatalf("edge should exist with empty label, got %+v", snap.Edges)
	}
}

func TestBuild_SpuriousLabelIgnored(t *testing.T) {
	// A label for a pair not confirmed by BlockedBy must not create an edge.
	sessions := map[int]Session{
		100: sess(100),
		200: sess(200), // not blocked by anyone
	}
	labels := map[EdgeKey]LockLabel{
		{WaiterPID: 200, BlockerPID: 100}: {LockType: "relation", Relation: "t"},
	}
	snap := Build("c1", time.Now(), sessions, labels)
	if len(snap.Edges) != 0 {
		t.Fatalf("spurious label must not create an edge, got %v", snap.Edges)
	}
	if len(snap.Roots) != 0 {
		t.Fatalf("no edges means no roots, got %v", snap.Roots)
	}
}

func TestBuild_TwoBlockers(t *testing.T) {
	// 300 blocked by both 100 and 200; both are roots.
	sessions := map[int]Session{
		100: sess(100),
		200: sess(200),
		300: sess(300, 100, 200),
	}
	snap := Build("c1", time.Now(), sessions, nil)
	if len(snap.Edges) != 2 {
		t.Fatalf("want 2 edges, got %d", len(snap.Edges))
	}
	if len(snap.Roots) != 2 {
		t.Fatalf("want 2 roots, got %v", snap.Roots)
	}
}

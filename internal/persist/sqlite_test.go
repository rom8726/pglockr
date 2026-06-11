package persist

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rom8726/pglockr/internal/graph"
)

func snap(cluster string, takenAt time.Time, roots int) graph.Snapshot {
	sessions := map[int]graph.Session{}
	for i := 0; i < roots; i++ {
		sessions[i] = graph.Session{PID: i, Query: "SELECT 1", IsRoot: true}
	}
	r := make([]int, roots)
	for i := range r {
		r[i] = i
	}
	return graph.Snapshot{Cluster: cluster, TakenAt: takenAt, Sessions: sessions, Roots: r}
}

func open(t *testing.T, retention time.Duration) *SQLite {
	t.Helper()
	p, err := Open(filepath.Join(t.TempDir(), "history.db"), retention, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

func TestSaveHistoryAndAt(t *testing.T) {
	p := open(t, 0)
	base := time.Now().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		if err := p.Save(snap("c1", base.Add(time.Duration(i)*time.Second), i+1)); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	metas, err := p.History(time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("want 3 metas, got %d", len(metas))
	}
	for i := 1; i < len(metas); i++ {
		if metas[i].TakenAt.Before(metas[i-1].TakenAt) {
			t.Fatalf("history not ascending")
		}
	}
	if metas[0].Roots != 1 || metas[2].Roots != 3 {
		t.Fatalf("root summaries wrong: %+v", metas)
	}

	// Nearest snapshot to base+1.1s is the second one (full data round-trips).
	got, ok, err := p.At(base.Add(1100 * time.Millisecond))
	if err != nil || !ok {
		t.Fatalf("At err=%v ok=%v", err, ok)
	}
	if !got.TakenAt.Equal(base.Add(time.Second)) {
		t.Fatalf("At returned %v, want %v", got.TakenAt, base.Add(time.Second))
	}
	if len(got.Sessions) != 2 || !got.Sessions[0].IsRoot || got.Sessions[0].Query != "SELECT 1" {
		t.Fatalf("snapshot did not round-trip: %+v", got.Sessions)
	}
}

func TestHistoryWindow(t *testing.T) {
	p := open(t, 0)
	base := time.Now().Truncate(time.Second)
	for i := 0; i < 4; i++ {
		_ = p.Save(snap("c1", base.Add(time.Duration(i)*time.Second), 1))
	}
	win, err := p.History(base.Add(time.Second), base.Add(2*time.Second))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(win) != 2 {
		t.Fatalf("want 2 metas in window, got %d", len(win))
	}
}

func TestRetentionPrune(t *testing.T) {
	p := open(t, time.Hour)
	// An old snapshot is pruned on the first Save (lastPrune is zero).
	if err := p.Save(snap("c1", time.Now().Add(-2*time.Hour), 1)); err != nil {
		t.Fatalf("save old: %v", err)
	}
	metas, _ := p.History(time.Time{}, time.Time{})
	if len(metas) != 0 {
		t.Fatalf("expired snapshot should have been pruned, got %d", len(metas))
	}

	// A fresh snapshot is retained (prune won't run again within a minute).
	if err := p.Save(snap("c1", time.Now(), 1)); err != nil {
		t.Fatalf("save new: %v", err)
	}
	metas, _ = p.History(time.Time{}, time.Time{})
	if len(metas) != 1 {
		t.Fatalf("recent snapshot should be retained, got %d", len(metas))
	}
}

func TestDurableAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.db")
	at := time.Now().Truncate(time.Second)

	p1, err := Open(path, 0, nil)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	if err := p1.Save(snap("c1", at, 2)); err != nil {
		t.Fatalf("save: %v", err)
	}
	p1.Close()

	// Reopen the same file: the snapshot must still be there (survives restart).
	p2, err := Open(path, 0, nil)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	defer p2.Close()
	got, ok, err := p2.At(at)
	if err != nil || !ok {
		t.Fatalf("At after reopen err=%v ok=%v", err, ok)
	}
	if !got.TakenAt.Equal(at) || len(got.Roots) != 2 {
		t.Fatalf("snapshot not durable: %+v", got)
	}
}

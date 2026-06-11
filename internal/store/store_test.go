package store

import (
	"testing"
	"time"

	"github.com/rom8726/pglockr/internal/graph"
)

func snap(pids ...int) graph.Snapshot {
	s := graph.Snapshot{TakenAt: time.Now(), Sessions: map[int]graph.Session{}}
	for _, p := range pids {
		s.Sessions[p] = graph.Session{PID: p}
	}
	return s
}

func TestRingWraps(t *testing.T) {
	st := New(2)
	st.Put(snap(1))
	st.Put(snap(2))
	st.Put(snap(3)) // evicts snap(1)

	latest, ok := st.Latest()
	if !ok || len(latest.Sessions) != 1 || latest.Sessions[3].PID != 3 {
		t.Fatalf("latest should be snap(3), got %+v ok=%v", latest.Sessions, ok)
	}
}

func TestAtClosest(t *testing.T) {
	st := New(3)
	base := time.Now()
	for i := 0; i < 3; i++ {
		s := snap(i)
		s.TakenAt = base.Add(time.Duration(i) * time.Second)
		st.Put(s)
	}
	got, ok := st.At(base.Add(1100 * time.Millisecond))
	if !ok || got.Sessions[1].PID != 1 {
		t.Fatalf("want snapshot at index 1, got %+v", got.Sessions)
	}
}

func TestSubscribeReceives(t *testing.T) {
	st := New(2)
	ch, unsub := st.Subscribe()
	defer unsub()
	st.Put(snap(7))
	select {
	case got := <-ch:
		if got.Sessions[7].PID != 7 {
			t.Fatalf("got wrong snapshot %+v", got.Sessions)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive snapshot")
	}
}

func TestHistory(t *testing.T) {
	st := New(5)
	base := time.Now().Truncate(time.Second)
	for i := 0; i < 4; i++ {
		s := snap(i) // one session (PID i)
		s.TakenAt = base.Add(time.Duration(i) * time.Second)
		s.Roots = []int{i}
		st.Put(s)
	}

	// Full range, ascending by time.
	all := st.History(time.Time{}, time.Time{})
	if len(all) != 4 {
		t.Fatalf("want 4 metas, got %d", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].TakenAt.Before(all[i-1].TakenAt) {
			t.Fatalf("history not ascending: %v", all)
		}
	}
	if all[0].Roots != 1 || all[0].Sessions != 1 {
		t.Fatalf("meta summary wrong: %+v", all[0])
	}

	// Windowed: [base+1s, base+2s] → two entries.
	win := st.History(base.Add(time.Second), base.Add(2*time.Second))
	if len(win) != 2 {
		t.Fatalf("want 2 metas in window, got %d: %+v", len(win), win)
	}
}

func TestDiffSnapshots(t *testing.T) {
	prev := snap(1, 2)
	cur := snap(2, 3)
	// mutate session 2 so it counts as changed
	s := cur.Sessions[2]
	s.State = "active"
	cur.Sessions[2] = s

	d := DiffSnapshots(prev, cur)
	if len(d.Added) != 1 || d.Added[0] != 3 {
		t.Fatalf("added want [3], got %v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0] != 1 {
		t.Fatalf("removed want [1], got %v", d.Removed)
	}
	if len(d.Changed) != 1 || d.Changed[0] != 2 {
		t.Fatalf("changed want [2], got %v", d.Changed)
	}
}

package persist

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rom8726/pglockr/internal/audit"
)

func TestAuditRecordRecentAndDurability(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.db")

	p1, err := Open(path, 0, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	base := time.Now().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		e := audit.Entry{
			At:          base.Add(time.Duration(i) * time.Second),
			Actor:       "olga",
			Action:      "cancel",
			PID:         100 + i,
			VictimQuery: "UPDATE t SET x = ?",
			Delivered:   true,
		}
		if err := p1.Record(e); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	got, err := p1.Recent(2)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 2 || got[0].PID != 102 || got[1].PID != 101 {
		t.Fatalf("want newest-first [102 101], got %+v", got)
	}
	if got[0].Actor != "olga" || !got[0].Delivered || got[0].VictimQuery != "UPDATE t SET x = ?" {
		t.Fatalf("entry did not round-trip: %+v", got[0])
	}
	p1.Close()

	// The audit trail survives a reopen (restart).
	p2, err := Open(path, 0, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer p2.Close()
	all, err := p2.Recent(100)
	if err != nil {
		t.Fatalf("recent after reopen: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("audit not durable: want 3 entries after reopen, got %d", len(all))
	}
}

func TestAuditNotPrunedByHistoryRetention(t *testing.T) {
	p := open(t, time.Hour) // history retention 1h
	old := audit.Entry{At: time.Now().Add(-2 * time.Hour), Actor: "a", Action: "cancel", PID: 1}
	if err := p.Record(old); err != nil {
		t.Fatal(err)
	}
	// Trigger a history prune via Save (lastPrune is zero on a fresh store).
	if err := p.Save(snap("c1", time.Now(), 1)); err != nil {
		t.Fatal(err)
	}
	got, err := p.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("audit must be immutable (not pruned), got %d entries", len(got))
	}
}

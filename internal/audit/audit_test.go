package audit

import (
	"fmt"
	"testing"
	"time"
)

func entry(i int) Entry {
	return Entry{At: time.Unix(int64(i), 0), Actor: fmt.Sprintf("u%d", i), Action: "cancel", PID: i}
}

func TestMemoryRecordAndRecent(t *testing.T) {
	m := NewMemory(10)
	for i := 1; i <= 3; i++ {
		if err := m.Record(entry(i)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := m.Recent(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].PID != 3 || got[1].PID != 2 {
		t.Fatalf("want newest-first [3 2], got %+v", got)
	}
	// limit 0 → all.
	all, _ := m.Recent(0)
	if len(all) != 3 {
		t.Fatalf("want 3, got %d", len(all))
	}
}

func TestMemoryEvictsOldest(t *testing.T) {
	m := NewMemory(2)
	for i := 1; i <= 5; i++ {
		_ = m.Record(entry(i))
	}
	got, _ := m.Recent(10)
	if len(got) != 2 || got[0].PID != 5 || got[1].PID != 4 {
		t.Fatalf("want [5 4] after eviction, got %+v", got)
	}
}

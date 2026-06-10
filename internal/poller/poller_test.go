package poller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/rom8726/pglockr/internal/graph"
	"github.com/rom8726/pglockr/internal/store"
)

type fakeSource struct {
	mu       sync.Mutex
	sessions map[int]graph.Session
	err      error
	calls    int
}

func (f *fakeSource) Snapshot(context.Context) (map[int]graph.Session, map[graph.EdgeKey]graph.LockLabel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.sessions, nil, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPoller_BuildsAndStores(t *testing.T) {
	src := &fakeSource{sessions: map[int]graph.Session{
		100: {PID: 100},
		200: {PID: 200, BlockedBy: []int{100}},
	}}
	st := store.New(10)
	p := New("c1", src, st, 5*time.Millisecond, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx)

	deadline := time.After(2 * time.Second)
	for {
		if snap, ok := st.Latest(); ok && len(snap.Roots) == 1 && snap.Roots[0] == 100 {
			cancel()
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatal("poller did not produce a forest with root 100 in time")
		case <-time.After(5 * time.Millisecond):
		}
	}

	if !p.Status().Connected {
		t.Fatal("status should be connected after a successful poll")
	}
}

func TestPoller_ErrorSetsDisconnected(t *testing.T) {
	src := &fakeSource{err: errors.New("boom")}
	st := store.New(10)
	p := New("c1", src, st, time.Millisecond, discardLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go p.Run(ctx)

	<-ctx.Done()
	if st0, _ := st.Latest(); st0.Cluster != "" {
		t.Fatal("no snapshot should be stored on error")
	}
	if p.Status().Connected || p.Status().LastError == "" {
		t.Fatalf("status should reflect failure, got %+v", p.Status())
	}
}

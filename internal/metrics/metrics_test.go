package metrics

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rom8726/pglockr/internal/graph"
)

func TestMetricsExposition(t *testing.T) {
	snap := graph.Snapshot{
		Cluster:  "c1",
		Sessions: map[int]graph.Session{1: {PID: 1}, 2: {PID: 2}},
		Roots:    []int{1},
		Edges:    []graph.Edge{{WaiterPID: 2, BlockerPID: 1}},
	}
	m := New("1.2.3",
		func() (string, bool) { return "c1", true },
		func() (graph.Snapshot, bool) { return snap, true },
	)
	m.ObservePoll("c1", 5*time.Millisecond, nil)
	m.ObservePoll("c1", 7*time.Millisecond, errors.New("boom"))
	m.ObserveAction("cancel", true)

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()

	for _, want := range []string{
		`pglockr_build_info{version="1.2.3"} 1`,
		`pglockr_polls_total{cluster="c1"} 2`,
		`pglockr_poll_errors_total{cluster="c1"} 1`,
		`pglockr_actions_total{action="cancel",delivered="true"} 1`,
		`pglockr_connected{cluster="c1"} 1`,
		`pglockr_sessions{cluster="c1"} 2`,
		`pglockr_root_blockers{cluster="c1"} 1`,
		`pglockr_blocked_edges{cluster="c1"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
	// Standard collectors are present.
	if !strings.Contains(body, "go_goroutines") {
		t.Error("expected Go collector metrics")
	}
}

func TestMetricsNoSnapshot(t *testing.T) {
	m := New("dev",
		func() (string, bool) { return "c1", false },
		func() (graph.Snapshot, bool) { return graph.Snapshot{}, false },
	)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `pglockr_connected{cluster="c1"} 0`) {
		t.Error("expected connected=0 when disconnected")
	}
	if strings.Contains(body, "pglockr_sessions") {
		t.Error("session gauges should be absent with no snapshot")
	}
}

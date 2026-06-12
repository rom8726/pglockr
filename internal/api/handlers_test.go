package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rom8726/pglockr/internal/audit"
	"github.com/rom8726/pglockr/internal/auth"
	"github.com/rom8726/pglockr/internal/graph"
	"github.com/rom8726/pglockr/internal/pg"
	"github.com/rom8726/pglockr/internal/poller"
	"github.com/rom8726/pglockr/internal/signal"
	"github.com/rom8726/pglockr/internal/store"
)

const (
	testToken   = "tok"      // admin (back-compat single token)
	viewerToken = "view-tok" // viewer role
	operatorTok = "op-tok"   // operator role
)

func testIdentity() auth.Identity {
	return auth.NewTokenIdentity([]auth.TokenPrincipal{
		{Name: "admin", Role: auth.RoleAdmin, Token: testToken},
		{Name: "vera", Role: auth.RoleViewer, Token: viewerToken},
		{Name: "olga", Role: auth.RoleOperator, Token: operatorTok},
	})
}

// fakeSignaler records calls and returns canned results.
type fakeSignaler struct {
	cancelled  []int
	terminated []int
	delivered  bool
}

func (f *fakeSignaler) Cancel(_ context.Context, pid int) (bool, error) {
	f.cancelled = append(f.cancelled, pid)
	return f.delivered, nil
}
func (f *fakeSignaler) Terminate(_ context.Context, pid int) (bool, error) {
	f.terminated = append(f.terminated, pid)
	return f.delivered, nil
}

// fakeInspector returns canned lock views.
type fakeInspector struct {
	locks []pg.LockRow
	hot   []pg.HotObject
}

func (f *fakeInspector) Locks(context.Context) ([]pg.LockRow, error)        { return f.locks, nil }
func (f *fakeInspector) HotObjects(context.Context) ([]pg.HotObject, error) { return f.hot, nil }

func newTestServer(t *testing.T, st *store.Store, sig signal.Signaler) http.Handler {
	return newTestServerWith(t, st, sig, &fakeInspector{})
}

func newTestServerWith(t *testing.T, st *store.Store, sig signal.Signaler, insp Inspector) http.Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sink := audit.NewMemory(100)
	// A poller that never runs; we only read its Status().
	p := poller.New("default", nil, st, time.Second, log)
	srv := New(Config{
		Cluster:   "default",
		Store:     st,
		Poller:    p,
		Signal:    signal.New(sig, nil, log, sink),
		Inspector: insp,
		Audit:     sink,
		Auth:      auth.New(testIdentity()),
		UI:        fstest.MapFS{"index.html": {Data: []byte("<html>ui</html>")}},
		Log:       log,
	})
	return srv.Handler()
}

// authed builds a request authenticated as the admin token (full access).
func authed(method, target string) *http.Request {
	return authedAs(method, target, testToken)
}

func authedAs(method, target, token string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestSnapshotRequiresAuth(t *testing.T) {
	h := newTestServer(t, store.New(10), &fakeSignaler{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/snapshot", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}

func TestSnapshotEmptyStore(t *testing.T) {
	h := newTestServer(t, store.New(10), &fakeSignaler{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(http.MethodGet, "/api/snapshot"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503 when no snapshot", rec.Code)
	}
}

func TestSnapshotReturnsForest(t *testing.T) {
	st := store.New(10)
	st.Put(graph.Build("default", time.Now(),
		map[int]graph.Session{
			100: {PID: 100},
			200: {PID: 200, BlockedBy: []int{100}},
		}, nil))

	h := newTestServer(t, st, &fakeSignaler{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(http.MethodGet, "/api/snapshot"))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var snap graph.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(snap.Roots) != 1 || snap.Roots[0] != 100 {
		t.Fatalf("roots = %v, want [100]", snap.Roots)
	}
	if len(snap.Edges) != 1 || snap.Edges[0].WaiterPID != 200 {
		t.Fatalf("edges = %v", snap.Edges)
	}
}

func TestSnapshotAt(t *testing.T) {
	st := store.New(10)
	base := time.Now().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		s := graph.Build("default", base.Add(time.Duration(i)*time.Second),
			map[int]graph.Session{100: {PID: 100}, 200: {PID: 200, BlockedBy: []int{100}}}, nil)
		st.Put(s)
	}
	h := newTestServer(t, st, &fakeSignaler{})

	// Ask for the middle snapshot by its exact timestamp.
	want := base.Add(time.Second)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(http.MethodGet, "/api/snapshot?at="+url.QueryEscape(want.Format(time.RFC3339))))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var snap graph.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !snap.TakenAt.Equal(want) {
		t.Fatalf("at= returned %v, want nearest %v", snap.TakenAt, want)
	}
}

func TestSnapshotAtBadTimestamp(t *testing.T) {
	h := newTestServer(t, store.New(10), &fakeSignaler{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(http.MethodGet, "/api/snapshot?at=nope"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestHealthzReflectsStatus(t *testing.T) {
	// Fresh poller is not connected yet → degraded / 503.
	h := newTestServer(t, store.New(10), &fakeSignaler{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503 before first poll", rec.Code)
	}
}

func TestCancelAction(t *testing.T) {
	sig := &fakeSignaler{delivered: true}
	h := newTestServer(t, store.New(10), sig)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(http.MethodPost, "/api/sessions/42/cancel"))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	if len(sig.cancelled) != 1 || sig.cancelled[0] != 42 {
		t.Fatalf("cancelled = %v, want [42]", sig.cancelled)
	}
	var res signal.Result
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Action != signal.ActionCancel || !res.Delivered {
		t.Fatalf("result = %+v", res)
	}
}

func TestMeEndpoint(t *testing.T) {
	h := newTestServer(t, store.New(10), &fakeSignaler{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedAs(http.MethodGet, "/api/me", operatorTok))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var p auth.Principal
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Name != "olga" || p.Role != auth.RoleOperator {
		t.Fatalf("me = %+v", p)
	}
}

func TestViewerCanReadButNotAct(t *testing.T) {
	st := store.New(10)
	st.Put(graph.Build("default", time.Now(), map[int]graph.Session{100: {PID: 100}}, nil))
	sig := &fakeSignaler{delivered: true}
	h := newTestServerWith(t, st, sig, &fakeInspector{})

	// Viewer can read the snapshot.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedAs(http.MethodGet, "/api/snapshot", viewerToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer GET snapshot = %d, want 200", rec.Code)
	}

	// But cannot cancel/terminate.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authedAs(http.MethodPost, "/api/sessions/100/cancel", viewerToken))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer POST cancel = %d, want 403", rec.Code)
	}
	if len(sig.cancelled) != 0 {
		t.Fatal("viewer action must not reach the signaler")
	}
}

func TestOperatorCanAct(t *testing.T) {
	sig := &fakeSignaler{delivered: true}
	h := newTestServer(t, store.New(10), sig)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedAs(http.MethodPost, "/api/sessions/7/terminate", operatorTok))
	if rec.Code != http.StatusOK {
		t.Fatalf("operator POST terminate = %d, want 200", rec.Code)
	}
	if len(sig.terminated) != 1 || sig.terminated[0] != 7 {
		t.Fatalf("terminate not delivered: %v", sig.terminated)
	}
}

func TestTerminateRequiresAuth(t *testing.T) {
	sig := &fakeSignaler{delivered: true}
	h := newTestServer(t, store.New(10), sig)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/sessions/42/terminate", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
	if len(sig.terminated) != 0 {
		t.Fatalf("must not signal on unauthorized request")
	}
}

func TestActionRejectsCrossOrigin(t *testing.T) {
	sig := &fakeSignaler{delivered: true}
	h := newTestServer(t, store.New(10), sig)
	r := authed(http.MethodPost, "/api/sessions/42/cancel")
	r.Host = "localhost:8080"
	r.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403 for cross-origin action", rec.Code)
	}
	if len(sig.cancelled) != 0 {
		t.Fatalf("cross-origin action must not reach the signaler")
	}
}

func TestHistoryEndpoint(t *testing.T) {
	st := store.New(10)
	base := time.Now().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		s := graph.Build("default", base.Add(time.Duration(i)*time.Second),
			map[int]graph.Session{100: {PID: 100}, 200: {PID: 200, BlockedBy: []int{100}}}, nil)
		st.Put(s)
	}
	h := newTestServer(t, st, &fakeSignaler{})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(http.MethodGet, "/api/history"))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var metas []store.Meta
	if err := json.Unmarshal(rec.Body.Bytes(), &metas); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("want 3 metas, got %d", len(metas))
	}
	if metas[0].Roots != 1 || metas[0].Edges != 1 || metas[0].Sessions != 2 {
		t.Fatalf("meta summary wrong: %+v", metas[0])
	}
}

func TestHistoryBadTimestamp(t *testing.T) {
	h := newTestServer(t, store.New(10), &fakeSignaler{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(http.MethodGet, "/api/history?from=not-a-time"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestLocksEndpoint(t *testing.T) {
	insp := &fakeInspector{locks: []pg.LockRow{
		{LockType: "relation", Object: "accounts", Mode: "AccessExclusiveLock", Granted: true, PID: 10},
		{LockType: "relation", Object: "accounts", Mode: "AccessShareLock", Granted: false, PID: 20},
	}}
	h := newTestServerWith(t, store.New(10), &fakeSignaler{}, insp)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(http.MethodGet, "/api/locks"))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var got []pg.LockRow
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 || got[0].Object != "accounts" || got[1].Granted {
		t.Fatalf("unexpected locks payload: %+v", got)
	}
}

func TestLocksRequiresAuth(t *testing.T) {
	h := newTestServer(t, store.New(10), &fakeSignaler{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/locks", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}

func TestHotObjectsEndpoint(t *testing.T) {
	insp := &fakeInspector{hot: []pg.HotObject{
		{Object: "accounts", Waiters: 3, Holders: 1},
	}}
	h := newTestServerWith(t, store.New(10), &fakeSignaler{}, insp)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(http.MethodGet, "/api/hot-objects"))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var got []pg.HotObject
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Waiters != 3 {
		t.Fatalf("unexpected hot-objects payload: %+v", got)
	}
}

func TestAuditAdminOnlyAndRecordsActions(t *testing.T) {
	sig := &fakeSignaler{delivered: true}
	h := newTestServer(t, store.New(10), sig)

	// Operator performs an action; it must land in the audit trail.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedAs(http.MethodPost, "/api/sessions/55/terminate", operatorTok))
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate = %d, want 200", rec.Code)
	}

	// viewer and operator are forbidden from reading the audit.
	for _, tok := range []string{viewerToken, operatorTok} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, authedAs(http.MethodGet, "/api/audit", tok))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("audit with %q = %d, want 403", tok, rec.Code)
		}
	}

	// admin sees the recorded entry, attributed to the operator principal.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authed(http.MethodGet, "/api/audit"))
	if rec.Code != http.StatusOK {
		t.Fatalf("audit as admin = %d, want 200", rec.Code)
	}
	var entries []audit.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 audit entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Actor != "olga" || e.Action != "terminate" || e.PID != 55 || !e.Delivered {
		t.Fatalf("audit entry wrong: %+v", e)
	}
}

func TestAuditBadLimit(t *testing.T) {
	h := newTestServer(t, store.New(10), &fakeSignaler{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(http.MethodGet, "/api/audit?limit=zero"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestSPAFallback(t *testing.T) {
	h := newTestServer(t, store.New(10), &fakeSignaler{})
	// An unknown client-side route falls back to index.html.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/some/spa/route", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "<html>ui</html>" {
		t.Fatalf("SPA fallback failed: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

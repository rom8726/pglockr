//go:build integration

// Integration test for the pg data-access layer against a real PostgreSQL.
//
// It is skipped unless PGLOCKR_TEST_DSN points at a database the test may write
// to (it creates and drops a throwaway table). Run with:
//
//	PGLOCKR_TEST_DSN="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable" \
//	  go test -tags=integration ./internal/pg/
package pg

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rom8726/pglockr/internal/graph"
)

const itTable = "t_pglockr_it"

func dsnOrSkip(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("PGLOCKR_TEST_DSN")
	if dsn == "" {
		t.Skip("PGLOCKR_TEST_DSN not set; skipping integration test")
	}
	return dsn
}

func backendPID(t *testing.T, ctx context.Context, c *pgx.Conn) int {
	t.Helper()
	var pid int
	if err := c.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("pg_backend_pid: %v", err)
	}
	return pid
}

// TestSnapshotBuildsForest manufactures a blocking pair (B blocked by A on an
// ACCESS EXCLUSIVE table lock) and asserts the pg client + graph builder
// reconstruct the wait-for forest correctly.
func TestSnapshotBuildsForest(t *testing.T) {
	dsn := dsnOrSkip(t)
	ctx := context.Background()

	// Admin connection for schema setup.
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "DROP TABLE IF EXISTS "+itTable); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE TABLE "+itTable+" (id int)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP TABLE IF EXISTS "+itTable)
	})

	// Session A: hold ACCESS EXCLUSIVE on the table.
	connA, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect A: %v", err)
	}
	defer connA.Close(ctx)
	pidA := backendPID(t, ctx, connA)
	if _, err := connA.Exec(ctx, "BEGIN"); err != nil {
		t.Fatalf("A begin: %v", err)
	}
	if _, err := connA.Exec(ctx, "LOCK TABLE "+itTable+" IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("A lock: %v", err)
	}
	defer func() { _, _ = connA.Exec(context.Background(), "ROLLBACK") }()

	// Session B: a SELECT that blocks on A's lock. Run it in the background; its
	// own context is cancelled during cleanup to release it.
	connB, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect B: %v", err)
	}
	pidB := backendPID(t, ctx, connB)
	bCtx, cancelB := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Blocks until A releases or the context is cancelled.
		_, _ = connB.Exec(bCtx, "SELECT * FROM "+itTable)
	}()
	// Ordered teardown: stop the query, wait for the goroutine to stop touching
	// connB, then close it (pgx.Conn is not concurrency-safe).
	t.Cleanup(func() {
		cancelB()
		<-done
		_ = connB.Close(context.Background())
	})

	// The client under test.
	client, err := Connect(ctx, dsn, 3*time.Second)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer client.Close()

	// Poll until B is reported as blocked by A.
	var snap graph.Snapshot
	deadline := time.Now().Add(15 * time.Second)
	for {
		sessions, labels, err := client.Snapshot(ctx)
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		snap = graph.Build("it", time.Now(), sessions, labels)
		if blockedBy(snap, pidB, pidA) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("B (%d) never became blocked by A (%d); sessions=%+v", pidB, pidA, snap.Sessions)
		}
		time.Sleep(250 * time.Millisecond)
	}

	// A is a root (holds the lock, waits for nobody).
	if a, ok := snap.Sessions[pidA]; !ok || !a.IsRoot {
		t.Fatalf("A (%d) should be a root, got %+v (ok=%v)", pidA, snap.Sessions[pidA], ok)
	}
	if !contains(snap.Roots, pidA) {
		t.Fatalf("roots %v should contain A (%d)", snap.Roots, pidA)
	}

	// The B->A edge is labelled with the table and the conflicting modes.
	var edge *graph.Edge
	for i := range snap.Edges {
		if snap.Edges[i].WaiterPID == pidB && snap.Edges[i].BlockerPID == pidA {
			edge = &snap.Edges[i]
			break
		}
	}
	if edge == nil {
		t.Fatalf("no B->A edge in %+v", snap.Edges)
	}
	if edge.BlockerMode != "AccessExclusiveLock" {
		t.Errorf("blocker mode = %q, want AccessExclusiveLock", edge.BlockerMode)
	}
	if edge.Relation == "" || edge.LockType != "relation" {
		t.Errorf("edge label missing object: relation=%q lockType=%q", edge.Relation, edge.LockType)
	}

	if client.ServerVersionNum() == 0 {
		t.Error("server version not detected")
	}

	// Lock inspector should report both A's held lock and B's pending lock on
	// the test table.
	locks, err := client.Locks(ctx)
	if err != nil {
		t.Fatalf("Locks: %v", err)
	}
	var sawHeld, sawWaiting bool
	for _, l := range locks {
		if l.Object != itTable {
			continue
		}
		if l.PID == pidA && l.Granted {
			sawHeld = true
		}
		if l.PID == pidB && !l.Granted {
			sawWaiting = true
		}
	}
	if !sawHeld || !sawWaiting {
		t.Errorf("Locks missing rows for %s: held=%v waiting=%v", itTable, sawHeld, sawWaiting)
	}

	// Hot objects should list the contended table with at least one waiter.
	hot, err := client.HotObjects(ctx)
	if err != nil {
		t.Fatalf("HotObjects: %v", err)
	}
	var found bool
	for _, h := range hot {
		if h.Object == itTable {
			found = true
			if h.Waiters < 1 || h.Holders < 1 {
				t.Errorf("hot object %s: waiters=%d holders=%d, want >=1 each", itTable, h.Waiters, h.Holders)
			}
		}
	}
	if !found {
		t.Errorf("hot objects missing %s: %+v", itTable, hot)
	}
}

func blockedBy(snap graph.Snapshot, pid, blocker int) bool {
	s, ok := snap.Sessions[pid]
	if !ok {
		return false
	}
	return contains(s.BlockedBy, blocker)
}

func contains(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

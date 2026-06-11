// Package store keeps the last M snapshots in an in-memory ring buffer and
// fans newly polled snapshots out to live subscribers (WebSocket clients).
package store

import (
	"log/slog"
	"sync"
	"time"

	"github.com/rom8726/pglockr/internal/graph"
)

// Persister is an optional durable backend for snapshot history. When set, At
// and History are served from it (so they can reach beyond the in-memory ring
// and survive restarts); Latest and the live stream always use the ring.
type Persister interface {
	Save(snap graph.Snapshot) error
	At(t time.Time) (graph.Snapshot, bool, error)
	History(from, to time.Time) ([]Meta, error)
}

// Store is a bounded, concurrency-safe ring buffer of snapshots plus a pub/sub
// hub. It holds the last cap snapshots (cap = ringSize from config).
type Store struct {
	mu   sync.RWMutex
	buf  []graph.Snapshot // ring; len grows to cap then wraps
	head int              // index of the next write
	size int              // number of valid entries
	cap  int

	subs   map[int]chan graph.Snapshot
	nextID int

	persist Persister
	log     *slog.Logger
}

// New returns a store retaining the last ringSize snapshots.
func New(ringSize int) *Store {
	if ringSize < 1 {
		ringSize = 1
	}
	return &Store{
		buf:  make([]graph.Snapshot, ringSize),
		cap:  ringSize,
		subs: make(map[int]chan graph.Snapshot),
	}
}

// SetPersister attaches a durable history backend. Call once at startup before
// the poller runs. log may be nil.
func (s *Store) SetPersister(p Persister, log *slog.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persist = p
	s.log = log
}

// Put appends a snapshot to the ring, persists it (if configured), and
// publishes it to all subscribers.
func (s *Store) Put(snap graph.Snapshot) {
	s.mu.Lock()
	s.buf[s.head] = snap
	s.head = (s.head + 1) % s.cap
	if s.size < s.cap {
		s.size++
	}
	// Snapshot the subscriber channels under the lock, send outside it.
	subs := make([]chan graph.Snapshot, 0, len(s.subs))
	for _, ch := range s.subs {
		subs = append(subs, ch)
	}
	persist := s.persist
	log := s.log
	s.mu.Unlock()

	// Persist outside the lock so disk I/O never stalls readers. Errors are
	// logged; the in-memory ring still serves recent history.
	if persist != nil {
		if err := persist.Save(snap); err != nil && log != nil {
			log.Warn("persist snapshot failed", "err", err)
		}
	}

	for _, ch := range subs {
		// Non-blocking: a slow client drops intermediate frames rather than
		// stalling the poller. It will still get the next one.
		select {
		case ch <- snap:
		default:
		}
	}
}

// Latest returns the most recent snapshot and whether one exists.
func (s *Store) Latest() (graph.Snapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.size == 0 {
		return graph.Snapshot{}, false
	}
	idx := (s.head - 1 + s.cap) % s.cap
	return s.buf[idx], true
}

// At returns the retained snapshot whose TakenAt is closest to t. With a
// persister it reaches the full durable history; otherwise it uses the ring.
func (s *Store) At(t time.Time) (graph.Snapshot, bool) {
	s.mu.RLock()
	persist, log := s.persist, s.log
	s.mu.RUnlock()
	if persist != nil {
		snap, ok, err := persist.At(t)
		if err != nil {
			if log != nil {
				log.Warn("persist At failed; falling back to ring", "err", err)
			}
		} else {
			return snap, ok
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.size == 0 {
		return graph.Snapshot{}, false
	}
	var best graph.Snapshot
	var bestDiff time.Duration = 1<<63 - 1
	found := false
	for i := 0; i < s.size; i++ {
		idx := (s.head - 1 - i + s.cap*2) % s.cap
		d := t.Sub(s.buf[idx].TakenAt)
		if d < 0 {
			d = -d
		}
		if d < bestDiff {
			bestDiff, best, found = d, s.buf[idx], true
		}
	}
	return best, found
}

// Meta is a lightweight summary of a retained snapshot, used to build the
// history scrubber timeline without shipping every full snapshot.
type Meta struct {
	TakenAt  time.Time `json:"takenAt"`
	Roots    int       `json:"roots"`
	Edges    int       `json:"edges"`
	Sessions int       `json:"sessions"`
}

// History returns metadata for retained snapshots whose TakenAt falls in
// [from, to], ascending by time. A zero `from`/`to` is treated as unbounded.
// With a persister it returns the full durable history; otherwise the ring.
func (s *Store) History(from, to time.Time) []Meta {
	s.mu.RLock()
	persist, log := s.persist, s.log
	s.mu.RUnlock()
	if persist != nil {
		metas, err := persist.History(from, to)
		if err != nil {
			if log != nil {
				log.Warn("persist History failed; falling back to ring", "err", err)
			}
		} else {
			return metas
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Meta, 0, s.size)
	// Walk oldest -> newest.
	for i := 0; i < s.size; i++ {
		idx := (s.head - s.size + i + s.cap*2) % s.cap
		snap := s.buf[idx]
		if !from.IsZero() && snap.TakenAt.Before(from) {
			continue
		}
		if !to.IsZero() && snap.TakenAt.After(to) {
			continue
		}
		out = append(out, Meta{
			TakenAt:  snap.TakenAt,
			Roots:    len(snap.Roots),
			Edges:    len(snap.Edges),
			Sessions: len(snap.Sessions),
		})
	}
	return out
}

// Subscribe registers a live subscriber. It returns a buffered channel of
// snapshots and an unsubscribe func that must be called to release resources.
func (s *Store) Subscribe() (<-chan graph.Snapshot, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	ch := make(chan graph.Snapshot, 1)
	s.subs[id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if c, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(c)
		}
	}
}

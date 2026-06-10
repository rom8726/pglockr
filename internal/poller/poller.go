// Package poller periodically snapshots the target cluster, builds the wait-for
// forest, and stores/publishes the result. It backs off on errors and exposes
// connection status for health checks and the /clusters endpoint.
package poller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/rom8726/pglockr/internal/graph"
	"github.com/rom8726/pglockr/internal/store"
)

// Source produces a raw snapshot of the target cluster.
type Source interface {
	Snapshot(ctx context.Context) (map[int]graph.Session, map[graph.EdgeKey]graph.LockLabel, error)
}

// Status is the live health of the poll loop, served to the UI.
type Status struct {
	Cluster   string    `json:"cluster"`
	Connected bool      `json:"connected"`
	LastPoll  time.Time `json:"lastPoll"`
	LastError string    `json:"lastError,omitempty"`
}

// Poller drives the snapshot loop for one cluster.
type Poller struct {
	cluster  string
	src      Source
	store    *store.Store
	interval time.Duration
	log      *slog.Logger

	mu     sync.RWMutex
	status Status
}

// New constructs a poller. interval is the base cadence; on errors the loop
// backs off up to a cap before retrying.
func New(cluster string, src Source, st *store.Store, interval time.Duration, log *slog.Logger) *Poller {
	return &Poller{
		cluster:  cluster,
		src:      src,
		store:    st,
		interval: interval,
		log:      log,
		status:   Status{Cluster: cluster},
	}
}

// Status returns a copy of the current poll status.
func (p *Poller) Status() Status {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status
}

// Run polls until ctx is cancelled. It blocks; run it in its own goroutine.
func (p *Poller) Run(ctx context.Context) {
	const maxBackoff = 30 * time.Second
	backoff := p.interval

	for {
		start := time.Now()
		if err := p.pollOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			p.setError(err)
			p.log.Warn("poll failed", "cluster", p.cluster, "err", err, "backoff", backoff)
			if !sleep(ctx, backoff) {
				return
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}

		backoff = p.interval // recovered
		// Sleep the remainder of the interval since the start of this poll.
		if wait := p.interval - time.Since(start); wait > 0 {
			if !sleep(ctx, wait) {
				return
			}
		} else if !sleep(ctx, 0) {
			return
		}
	}
}

func (p *Poller) pollOnce(ctx context.Context) error {
	sessions, labels, err := p.src.Snapshot(ctx)
	if err != nil {
		return err
	}
	snap := graph.Build(p.cluster, time.Now(), sessions, labels)
	p.store.Put(snap)
	p.setOK()
	return nil
}

func (p *Poller) setOK() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.Connected = true
	p.status.LastPoll = time.Now()
	p.status.LastError = ""
}

func (p *Poller) setError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.Connected = false
	p.status.LastError = err.Error()
}

// sleep waits for d or until ctx is done. It returns false if ctx was cancelled.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

package redact

import (
	"context"

	"github.com/rom8726/pglockr/internal/graph"
	"github.com/rom8726/pglockr/internal/poller"
)

// Source wraps a poller.Source, masking every session's query text before it
// enters the pipeline. Because redaction happens at ingestion, the raw texts
// never reach the ring buffer, persistent history, the live stream, or the
// audit log.
type Source struct {
	inner poller.Source
}

// NewSource wraps src with ingestion-time query redaction.
func NewSource(src poller.Source) *Source { return &Source{inner: src} }

// Snapshot polls the inner source and masks query texts in place.
func (s *Source) Snapshot(ctx context.Context) (map[int]graph.Session, map[graph.EdgeKey]graph.LockLabel, error) {
	sessions, labels, err := s.inner.Snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	for pid, sess := range sessions {
		sess.Query = Mask(sess.Query)
		sessions[pid] = sess
	}
	return sessions, labels, nil
}

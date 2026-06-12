// Package signal executes cancel/terminate actions against the target cluster
// and writes an audit record for every attempt (spec 5.10). On MVP the audit
// sink is the structured log; the interface allows a durable sink later.
package signal

import (
	"context"
	"log/slog"
	"time"

	"github.com/rom8726/pglockr/internal/audit"
)

// Action names the kind of signal sent to a backend.
type Action string

const (
	ActionCancel    Action = "cancel"
	ActionTerminate Action = "terminate"
)

// Signaler is the subset of the pg client used to send signals.
type Signaler interface {
	Cancel(ctx context.Context, pid int) (bool, error)
	Terminate(ctx context.Context, pid int) (bool, error)
}

// QueryLookup returns the current query text of a backend (for the audit
// record), if known. It is satisfied by reading the latest snapshot.
type QueryLookup func(pid int) (query string, ok bool)

// Observer counts cancel/terminate actions (for metrics). Optional.
type Observer interface {
	ObserveAction(action string, delivered bool)
}

// Service performs audited cancel/terminate operations.
type Service struct {
	sig    Signaler
	lookup QueryLookup
	log    *slog.Logger
	sink   audit.Sink
	obs    Observer
}

// New constructs the signal service. lookup and sink may be nil; with a nil
// sink the audit trail exists only in the structured log.
func New(sig Signaler, lookup QueryLookup, log *slog.Logger, sink audit.Sink) *Service {
	return &Service{sig: sig, lookup: lookup, log: log, sink: sink}
}

// SetMetrics attaches an optional action observer.
func (s *Service) SetMetrics(o Observer) { s.obs = o }

// Result reports the outcome of a signal attempt.
type Result struct {
	Action    Action    `json:"action"`
	PID       int       `json:"pid"`
	Delivered bool      `json:"delivered"` // PostgreSQL accepted the signal
	At        time.Time `json:"at"`
}

// Do sends the action to pid, attributing it to actor for the audit log.
func (s *Service) Do(ctx context.Context, action Action, pid int, actor string) (Result, error) {
	var (
		delivered bool
		err       error
	)
	switch action {
	case ActionCancel:
		delivered, err = s.sig.Cancel(ctx, pid)
	case ActionTerminate:
		delivered, err = s.sig.Terminate(ctx, pid)
	}

	var victimQuery string
	if s.lookup != nil {
		victimQuery, _ = s.lookup(pid)
	}

	at := time.Now()
	// Immutable audit trail: who, when, which PID, the victim's query, result.
	entry := audit.Entry{
		At:          at,
		Actor:       actor,
		Action:      string(action),
		PID:         pid,
		VictimQuery: victimQuery,
		Delivered:   delivered && err == nil,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	if s.sink != nil {
		if rerr := s.sink.Record(entry); rerr != nil {
			s.log.Error("audit sink write failed", "err", rerr)
		}
	}
	if s.obs != nil {
		s.obs.ObserveAction(string(action), entry.Delivered)
	}

	attrs := []any{
		"audit", true,
		"action", string(action),
		"pid", pid,
		"actor", actor,
		"delivered", delivered,
		"victim_query", victimQuery,
		"at", at.Format(time.RFC3339),
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
		s.log.Error("signal action failed", attrs...)
		return Result{Action: action, PID: pid, Delivered: false, At: at}, err
	}
	s.log.Info("signal action", attrs...)
	return Result{Action: action, PID: pid, Delivered: delivered, At: at}, nil
}

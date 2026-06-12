// Package metrics exposes Prometheus metrics for pglockr's own health and
// activity (poll cadence/errors, current forest size, actions taken). It serves
// no query texts or other sensitive data, so /metrics is unauthenticated like
// /healthz — scrape it from a trusted network or behind the same proxy.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/rom8726/pglockr/internal/graph"
)

// StatusFunc reports the current cluster name and whether the poller is
// connected (evaluated at scrape time).
type StatusFunc func() (cluster string, connected bool)

// SnapshotFunc returns the latest snapshot, if any (evaluated at scrape time).
type SnapshotFunc func() (graph.Snapshot, bool)

// Metrics holds the registry and instruments.
type Metrics struct {
	reg          *prometheus.Registry
	pollsTotal   *prometheus.CounterVec
	pollErrors   *prometheus.CounterVec
	pollDuration *prometheus.HistogramVec
	actions      *prometheus.CounterVec
}

// New builds the metric set, registering process/Go collectors plus a
// scrape-time collector for the live gauges. version labels build info.
func New(version string, statusFn StatusFunc, snapFn SnapshotFunc) *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		reg: reg,
		pollsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pglockr_polls_total",
			Help: "Total target-cluster polls.",
		}, []string{"cluster"}),
		pollErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pglockr_poll_errors_total",
			Help: "Total failed target-cluster polls.",
		}, []string{"cluster"}),
		pollDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "pglockr_poll_duration_seconds",
			Help:    "Duration of a target-cluster poll.",
			Buckets: prometheus.DefBuckets,
		}, []string{"cluster"}),
		actions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pglockr_actions_total",
			Help: "cancel/terminate actions taken.",
		}, []string{"action", "delivered"}),
	}

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pglockr_build_info",
		Help: "Build information; constant 1.",
	}, []string{"version"})
	buildInfo.WithLabelValues(version).Set(1)

	reg.MustRegister(
		m.pollsTotal, m.pollErrors, m.pollDuration, m.actions, buildInfo,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		&liveCollector{statusFn: statusFn, snapFn: snapFn},
	)
	return m
}

// Handler serves the metrics in the Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// ObservePoll records one poll's duration and whether it failed. Satisfies
// poller.Observer.
func (m *Metrics) ObservePoll(cluster string, d time.Duration, err error) {
	m.pollsTotal.WithLabelValues(cluster).Inc()
	if err != nil {
		m.pollErrors.WithLabelValues(cluster).Inc()
	}
	m.pollDuration.WithLabelValues(cluster).Observe(d.Seconds())
}

// ObserveAction records one cancel/terminate. Satisfies signal.Observer.
func (m *Metrics) ObserveAction(action string, delivered bool) {
	m.actions.WithLabelValues(action, boolLabel(delivered)).Inc()
}

func boolLabel(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// liveCollector emits gauges sampled from the current poller status and latest
// snapshot at scrape time, so they need no event wiring.
type liveCollector struct {
	statusFn StatusFunc
	snapFn   SnapshotFunc
}

var (
	descConnected = prometheus.NewDesc("pglockr_connected", "1 if the poller is connected to the target.", []string{"cluster"}, nil)
	descSessions  = prometheus.NewDesc("pglockr_sessions", "Client backends in the latest snapshot.", []string{"cluster"}, nil)
	descRoots     = prometheus.NewDesc("pglockr_root_blockers", "Head blockers in the latest snapshot.", []string{"cluster"}, nil)
	descEdges     = prometheus.NewDesc("pglockr_blocked_edges", "Blocking edges in the latest snapshot.", []string{"cluster"}, nil)
)

func (c *liveCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descConnected
	ch <- descSessions
	ch <- descRoots
	ch <- descEdges
}

func (c *liveCollector) Collect(ch chan<- prometheus.Metric) {
	cluster, connected := c.statusFn()
	ch <- prometheus.MustNewConstMetric(descConnected, prometheus.GaugeValue, b2f(connected), cluster)

	if snap, ok := c.snapFn(); ok {
		ch <- prometheus.MustNewConstMetric(descSessions, prometheus.GaugeValue, float64(len(snap.Sessions)), cluster)
		ch <- prometheus.MustNewConstMetric(descRoots, prometheus.GaugeValue, float64(len(snap.Roots)), cluster)
		ch <- prometheus.MustNewConstMetric(descEdges, prometheus.GaugeValue, float64(len(snap.Edges)), cluster)
	}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

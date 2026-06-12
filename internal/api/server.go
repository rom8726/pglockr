// Package api wires the REST + WebSocket HTTP surface and serves the embedded
// UI. The route table is the MVP subset of the spec 5.15 contract, left open
// for later additions (history, locks, hot-objects, audit).
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/rom8726/pglockr/internal/audit"
	"github.com/rom8726/pglockr/internal/auth"
	"github.com/rom8726/pglockr/internal/pg"
	"github.com/rom8726/pglockr/internal/poller"
	"github.com/rom8726/pglockr/internal/signal"
	"github.com/rom8726/pglockr/internal/store"
)

// Inspector serves on-demand lock views (lock inspector, hot objects). It is
// satisfied by *pg.Client.
type Inspector interface {
	Locks(ctx context.Context) ([]pg.LockRow, error)
	HotObjects(ctx context.Context) ([]pg.HotObject, error)
}

// Server holds the dependencies for the HTTP handlers.
type Server struct {
	cluster   string
	store     *store.Store
	poller    *poller.Poller
	signal    *signal.Service
	inspector Inspector
	audit     audit.Sink
	auth      *auth.Middleware
	ui        fs.FS
	log       *slog.Logger
}

// Config bundles the Server's dependencies.
type Config struct {
	Cluster   string
	Store     *store.Store
	Poller    *poller.Poller
	Signal    *signal.Service
	Inspector Inspector
	Audit     audit.Sink
	Auth      *auth.Middleware
	UI        fs.FS
	Log       *slog.Logger
}

// New builds a Server.
func New(c Config) *Server {
	return &Server{
		cluster:   c.Cluster,
		store:     c.Store,
		poller:    c.Poller,
		signal:    c.Signal,
		inspector: c.Inspector,
		audit:     c.Audit,
		auth:      c.Auth,
		ui:        c.UI,
		log:       c.Log,
	}
}

// Handler returns the root http.Handler with all routes mounted.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	// Liveness needs no auth.
	r.Get("/healthz", s.handleHealthz)

	// Authenticated API surface. Any authenticated principal is at least a
	// viewer; actions require the operator role.
	r.Route("/api", func(api chi.Router) {
		api.Use(s.auth.Authenticate)

		api.Get("/me", s.handleMe)
		api.Get("/clusters", s.handleClusters)
		api.Get("/snapshot", s.handleSnapshot)
		api.Get("/history", s.handleHistory)
		api.Get("/stream", s.handleStream) // WebSocket
		api.Get("/locks", s.handleLocks)
		api.Get("/hot-objects", s.handleHotObjects)

		// State-changing actions: require operator + an Origin check.
		api.Group(func(act chi.Router) {
			act.Use(auth.RequireRole(auth.RoleOperator))
			act.Use(requireSameOrigin)
			act.Post("/sessions/{pid}/cancel", s.handleCancel)
			act.Post("/sessions/{pid}/terminate", s.handleTerminate)
		})

		// Audit trail: admin only.
		api.Group(func(adm chi.Router) {
			adm.Use(auth.RequireRole(auth.RoleAdmin))
			adm.Get("/audit", s.handleAudit)
		})
	})

	// Everything else: the embedded SPA.
	r.Handle("/*", s.spaHandler())
	return r
}

// requireSameOrigin rejects cross-site state-changing requests (MVP anti-CSRF).
func requireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.CheckOrigin(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Package api wires the REST + WebSocket HTTP surface and serves the embedded
// UI. The route table is the MVP subset of the spec 5.15 contract, left open
// for later additions (history, locks, hot-objects, audit).
package api

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/rom8726/pglockr/internal/auth"
	"github.com/rom8726/pglockr/internal/poller"
	"github.com/rom8726/pglockr/internal/signal"
	"github.com/rom8726/pglockr/internal/store"
)

// Server holds the dependencies for the HTTP handlers.
type Server struct {
	cluster string
	store   *store.Store
	poller  *poller.Poller
	signal  *signal.Service
	auth    *auth.Authenticator
	ui      fs.FS
	log     *slog.Logger
}

// Config bundles the Server's dependencies.
type Config struct {
	Cluster string
	Store   *store.Store
	Poller  *poller.Poller
	Signal  *signal.Service
	Auth    *auth.Authenticator
	UI      fs.FS
	Log     *slog.Logger
}

// New builds a Server.
func New(c Config) *Server {
	return &Server{
		cluster: c.Cluster,
		store:   c.Store,
		poller:  c.Poller,
		signal:  c.Signal,
		auth:    c.Auth,
		ui:      c.UI,
		log:     c.Log,
	}
}

// Handler returns the root http.Handler with all routes mounted.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	// Liveness needs no auth.
	r.Get("/healthz", s.handleHealthz)

	// Authenticated API surface.
	r.Route("/api", func(api chi.Router) {
		api.Use(s.auth.Require)

		api.Get("/clusters", s.handleClusters)
		api.Get("/snapshot", s.handleSnapshot)
		api.Get("/stream", s.handleStream) // WebSocket

		// State-changing actions: add an Origin check on top of auth.
		api.Group(func(act chi.Router) {
			act.Use(requireSameOrigin)
			act.Post("/sessions/{pid}/cancel", s.handleCancel)
			act.Post("/sessions/{pid}/terminate", s.handleTerminate)
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

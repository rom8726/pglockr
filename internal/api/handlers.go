package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rom8726/pglockr/internal/auth"
	"github.com/rom8726/pglockr/internal/signal"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	st := s.poller.Status()
	code := http.StatusOK
	if !st.Connected {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"status":    map[bool]string{true: "ok", false: "degraded"}[st.Connected],
		"connected": st.Connected,
		"lastPoll":  st.LastPoll,
	})
}

// handleMe returns the authenticated principal so the UI can show identity and
// gate role-restricted controls (e.g. hide cancel/terminate for viewers).
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleClusters(w http.ResponseWriter, _ *http.Request) {
	// MVP: a single cluster, reported with its live poll status.
	writeJSON(w, http.StatusOK, []any{s.poller.Status()})
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if at := r.URL.Query().Get("at"); at != "" {
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			http.Error(w, "invalid 'at' timestamp (want RFC3339)", http.StatusBadRequest)
			return
		}
		snap, ok := s.store.At(t)
		if !ok {
			http.Error(w, "no snapshot available", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, snap)
		return
	}

	snap, ok := s.store.Latest()
	if !ok {
		http.Error(w, "no snapshot available yet", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// handleHistory returns metadata of retained snapshots in an optional [from,to]
// window (RFC3339), oldest first — the data the UI scrubber timeline is built
// from.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err := parseOptTime(q.Get("from"))
	if err != nil {
		http.Error(w, "invalid 'from' timestamp (want RFC3339)", http.StatusBadRequest)
		return
	}
	to, err := parseOptTime(q.Get("to"))
	if err != nil {
		http.Error(w, "invalid 'to' timestamp (want RFC3339)", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, s.store.History(from, to))
}

func parseOptTime(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, v)
}

func (s *Server) handleLocks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.inspector.Locks(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleHotObjects(w http.ResponseWriter, r *http.Request) {
	objs, err := s.inspector.HotObjects(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, objs)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	s.doSignal(w, r, signal.ActionCancel)
}

func (s *Server) handleTerminate(w http.ResponseWriter, r *http.Request) {
	s.doSignal(w, r, signal.ActionTerminate)
}

func (s *Server) doSignal(w http.ResponseWriter, r *http.Request, action signal.Action) {
	pid, err := strconv.Atoi(chi.URLParam(r, "pid"))
	if err != nil {
		http.Error(w, "invalid pid", http.StatusBadRequest)
		return
	}
	// Attribute the action to the authenticated principal for the audit log.
	actor := "unknown"
	if p, ok := auth.PrincipalFrom(r.Context()); ok {
		actor = p.Name
	}
	res, err := s.signal.Do(r.Context(), action, pid, actor)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error":  err.Error(),
			"result": res,
		})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// spaHandler serves the embedded UI, falling back to index.html for client-side
// routes (single-page app).
func (s *Server) spaHandler() http.Handler {
	fileServer := http.FileServer(http.FS(s.ui))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If the requested asset exists, serve it; otherwise serve index.html.
		p := r.URL.Path
		if p != "/" {
			if _, err := fs.Stat(s.ui, trimLeadingSlash(p)); errors.Is(err, fs.ErrNotExist) {
				r2 := new(http.Request)
				*r2 = *r
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}

func trimLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		return p[1:]
	}
	return p
}

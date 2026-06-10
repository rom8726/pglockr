package api

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/rom8726/pglockr/internal/auth"
)

// handleStream upgrades to a WebSocket and streams snapshots. The current
// snapshot is sent immediately, then every newly polled snapshot is pushed.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	// Accept's default behaviour rejects cross-origin upgrades (Origin must
	// match Host), which is exactly our anti-CSRF policy.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.log.Warn("ws accept failed", "err", err)
		return
	}
	defer c.CloseNow()

	// websocket.Accept already enforces same-origin by default; double-check
	// with our shared policy for non-browser edge cases.
	if !auth.CheckOrigin(r) {
		_ = c.Close(websocket.StatusPolicyViolation, "cross-origin")
		return
	}

	ctx := r.Context()
	sub, unsub := s.store.Subscribe()
	defer unsub()

	// Send the latest snapshot right away so the client has initial state.
	if snap, ok := s.store.Latest(); ok {
		if err := writeWS(ctx, c, snap); err != nil {
			return
		}
	}

	// Keepalive ping so dead connections are detected and cleaned up.
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case snap, ok := <-sub:
			if !ok {
				return
			}
			if err := writeWS(ctx, c, snap); err != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func writeWS(ctx context.Context, c *websocket.Conn, v any) error {
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return wsjson.Write(wctx, c, v)
}

// Package auth provides MVP authentication: a single static bearer token.
// The middleware is structured so role-based access (viewer/operator/admin)
// can be layered on later without changing call sites.
package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Authenticator validates the single configured token.
type Authenticator struct {
	token string
}

// New returns an Authenticator for the given static token.
func New(token string) *Authenticator { return &Authenticator{token: token} }

// validToken reports whether presented matches the configured token, using a
// constant-time comparison to avoid leaking length/content via timing.
func (a *Authenticator) validToken(presented string) bool {
	if presented == "" || a.token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(a.token)) == 1
}

// tokenFromRequest extracts the bearer token from the Authorization header or
// the pglockr_token cookie (the latter lets the browser open the WebSocket,
// which cannot set custom headers cross-origin).
func tokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if t, ok := strings.CutPrefix(h, "Bearer "); ok {
			return t
		}
	}
	if c, err := r.Cookie("pglockr_token"); err == nil {
		return c.Value
	}
	return ""
}

// Require wraps next, rejecting requests without a valid token.
func (a *Authenticator) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.validToken(tokenFromRequest(r)) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CheckOrigin guards state-changing requests and WebSocket upgrades against
// cross-site abuse. With no Origin header (non-browser clients like curl) it
// allows the request; with one, it must match the Host. This is the MVP
// anti-CSRF measure (spec 5.15).
func CheckOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	host := r.Host
	return strings.HasSuffix(origin, "://"+host) || strings.HasSuffix(origin, host)
}

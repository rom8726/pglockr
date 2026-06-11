// Package auth provides role-based access control. A request is resolved to a
// Principal{Name, Role} by an Identity (pluggable: tokens now, a trusted SSO
// proxy later); middleware enforces authentication and a minimum role.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

// Role is an access level. Ordered: viewer < operator < admin.
type Role string

const (
	RoleViewer   Role = "viewer"   // read-only: forest, history, locks, hot objects
	RoleOperator Role = "operator" // + cancel/terminate
	RoleAdmin    Role = "admin"    // + audit/config (reserved)
)

func (r Role) rank() int {
	switch r {
	case RoleViewer:
		return 1
	case RoleOperator:
		return 2
	case RoleAdmin:
		return 3
	default:
		return 0
	}
}

// AtLeast reports whether r is a valid role at least as privileged as min.
func (r Role) AtLeast(min Role) bool { return r.rank() > 0 && r.rank() >= min.rank() }

// ParseRole validates and parses a role string.
func ParseRole(s string) (Role, error) {
	switch Role(s) {
	case RoleViewer, RoleOperator, RoleAdmin:
		return Role(s), nil
	default:
		return "", fmt.Errorf("invalid role %q (want viewer|operator|admin)", s)
	}
}

// Principal is the authenticated identity behind a request.
type Principal struct {
	Name string `json:"name"`
	Role Role   `json:"role"`
}

// Identity resolves an HTTP request to a Principal. ok is false when the request
// carries no valid credentials.
type Identity interface {
	Authenticate(r *http.Request) (Principal, bool)
}

// --- token identity ---

// TokenPrincipal binds a bearer token to a named principal and role.
type TokenPrincipal struct {
	Name  string
	Role  Role
	Token string
}

// TokenIdentity authenticates by bearer token, looked up by SHA-256 hash.
type TokenIdentity struct {
	byHash map[string]Principal
}

// NewTokenIdentity indexes the given token principals by token hash.
func NewTokenIdentity(entries []TokenPrincipal) *TokenIdentity {
	m := make(map[string]Principal, len(entries))
	for _, e := range entries {
		if e.Token == "" {
			continue
		}
		m[hashToken(e.Token)] = Principal{Name: e.Name, Role: e.Role}
	}
	return &TokenIdentity{byHash: m}
}

// Authenticate resolves the request's bearer token to a principal.
func (t *TokenIdentity) Authenticate(r *http.Request) (Principal, bool) {
	tok := tokenFromRequest(r)
	if tok == "" {
		return Principal{}, false
	}
	p, ok := t.byHash[hashToken(tok)]
	return p, ok
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
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

// --- middleware ---

type ctxKey int

const principalKey ctxKey = 0

// PrincipalFrom returns the authenticated principal stored in the context.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)
	return p, ok
}

// Middleware enforces authentication using an Identity.
type Middleware struct {
	id Identity
}

// New returns a Middleware backed by the given identity source.
func New(id Identity) *Middleware { return &Middleware{id: id} }

// Authenticate rejects requests without valid credentials and stores the
// resolved principal in the request context for downstream handlers.
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := m.id.Authenticate(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), principalKey, p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole rejects (403) authenticated requests whose principal is below the
// minimum role. It must be mounted after Authenticate.
func RequireRole(min Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFrom(r.Context())
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if !p.Role.AtLeast(min) {
				http.Error(w, fmt.Sprintf("forbidden: requires %s role", min), http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CheckOrigin guards state-changing requests and WebSocket upgrades against
// cross-site abuse. With no Origin header (non-browser clients like curl) it
// allows the request; with one, it must match the Host.
func CheckOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	host := r.Host
	return strings.HasSuffix(origin, "://"+host) || strings.HasSuffix(origin, host)
}

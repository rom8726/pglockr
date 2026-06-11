package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoleAtLeast(t *testing.T) {
	cases := []struct {
		role Role
		min  Role
		want bool
	}{
		{RoleAdmin, RoleOperator, true},
		{RoleOperator, RoleOperator, true},
		{RoleViewer, RoleOperator, false},
		{RoleOperator, RoleViewer, true},
		{RoleAdmin, RoleAdmin, true},
		{Role("bogus"), RoleViewer, false},
	}
	for _, c := range cases {
		if got := c.role.AtLeast(c.min); got != c.want {
			t.Errorf("%s.AtLeast(%s) = %v, want %v", c.role, c.min, got, c.want)
		}
	}
}

func TestParseRole(t *testing.T) {
	for _, s := range []string{"viewer", "operator", "admin"} {
		if _, err := ParseRole(s); err != nil {
			t.Errorf("ParseRole(%q) errored: %v", s, err)
		}
	}
	if _, err := ParseRole("root"); err == nil {
		t.Error("ParseRole(root) should error")
	}
}

func testIdentity() *TokenIdentity {
	return NewTokenIdentity([]TokenPrincipal{
		{Name: "ada", Role: RoleViewer, Token: "view-tok"},
		{Name: "ben", Role: RoleOperator, Token: "op-tok"},
		{Name: "cleo", Role: RoleAdmin, Token: "admin-tok"},
	})
}

func TestTokenIdentityAuthenticate(t *testing.T) {
	id := testIdentity()

	t.Run("bearer header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer op-tok")
		p, ok := id.Authenticate(r)
		if !ok || p.Name != "ben" || p.Role != RoleOperator {
			t.Fatalf("got %+v ok=%v", p, ok)
		}
	})
	t.Run("cookie", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: "pglockr_token", Value: "view-tok"})
		p, ok := id.Authenticate(r)
		if !ok || p.Role != RoleViewer {
			t.Fatalf("got %+v ok=%v", p, ok)
		}
	})
	t.Run("unknown token", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer nope")
		if _, ok := id.Authenticate(r); ok {
			t.Fatal("unknown token must not authenticate")
		}
	})
	t.Run("no credentials", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if _, ok := id.Authenticate(r); ok {
			t.Fatal("missing token must not authenticate")
		}
	})
}

func TestAuthenticateMiddleware(t *testing.T) {
	mw := New(testIdentity())
	var seen Principal
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = PrincipalFrom(r.Context())
		w.WriteHeader(http.StatusTeapot)
	})
	h := mw.Authenticate(next)

	t.Run("rejects anonymous", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rec.Code)
		}
	})
	t.Run("passes and sets principal", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		r.Header.Set("Authorization", "Bearer admin-tok")
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusTeapot || seen.Name != "cleo" || seen.Role != RoleAdmin {
			t.Fatalf("code=%d principal=%+v", rec.Code, seen)
		}
	})
}

func TestRequireRole(t *testing.T) {
	mw := New(testIdentity())
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	// Authenticate -> RequireRole(operator) -> final.
	h := mw.Authenticate(RequireRole(RoleOperator)(final))

	req := func(tok string) int {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/x", nil)
		r.Header.Set("Authorization", "Bearer "+tok)
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	if code := req("view-tok"); code != http.StatusForbidden {
		t.Errorf("viewer on operator route = %d, want 403", code)
	}
	if code := req("op-tok"); code != http.StatusOK {
		t.Errorf("operator on operator route = %d, want 200", code)
	}
	if code := req("admin-tok"); code != http.StatusOK {
		t.Errorf("admin on operator route = %d, want 200", code)
	}
}

func TestCheckOrigin(t *testing.T) {
	mk := func(origin, host string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/x", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	cases := []struct {
		name         string
		origin, host string
		want         bool
	}{
		{"no origin (curl)", "", "localhost:8080", true},
		{"same origin", "http://localhost:8080", "localhost:8080", true},
		{"cross origin", "http://evil.example", "localhost:8080", false},
		{"https same host", "https://app.local", "app.local", true},
	}
	for _, c := range cases {
		if got := CheckOrigin(mk(c.origin, c.host)); got != c.want {
			t.Errorf("%s: CheckOrigin = %v, want %v", c.name, got, c.want)
		}
	}
}

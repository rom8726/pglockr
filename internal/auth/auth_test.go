package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidToken(t *testing.T) {
	a := New("secret")
	cases := map[string]bool{
		"secret":  true,
		"":        false,
		"wrong":   false,
		"secret ": false,
	}
	for tok, want := range cases {
		if got := a.validToken(tok); got != want {
			t.Errorf("validToken(%q) = %v, want %v", tok, got, want)
		}
	}
	// An empty configured token rejects everything.
	if New("").validToken("anything") {
		t.Error("empty configured token must reject all")
	}
}

func TestTokenFromRequest(t *testing.T) {
	t.Run("bearer header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer abc")
		if got := tokenFromRequest(r); got != "abc" {
			t.Errorf("got %q, want abc", got)
		}
	})
	t.Run("cookie", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: "pglockr_token", Value: "xyz"})
		if got := tokenFromRequest(r); got != "xyz" {
			t.Errorf("got %q, want xyz", got)
		}
	})
	t.Run("none", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if got := tokenFromRequest(r); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestRequireMiddleware(t *testing.T) {
	a := New("secret")
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := a.Require(next)

	t.Run("rejects without token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/x", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("got %d, want 401", rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Error("missing WWW-Authenticate header")
		}
	})
	t.Run("passes with token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		r.Header.Set("Authorization", "Bearer secret")
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusTeapot {
			t.Errorf("got %d, want 418 (next handler)", rec.Code)
		}
	})
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
		name   string
		origin string
		host   string
		want   bool
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

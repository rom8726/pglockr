package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func proxyCfg(trust string) ProxyConfig {
	return ProxyConfig{
		UserHeader:   "X-Auth-Request-Email",
		GroupsHeader: "X-Auth-Request-Groups",
		GroupsSep:    ",",
		TrustMode:    trust,
		SecretHeader: "X-Pglockr-Proxy-Secret",
		Secret:       "s3cret",
		GroupToRole: map[string]Role{
			"pglockr-admins":  RoleAdmin,
			"pglockr-ops":     RoleOperator,
			"pglockr-viewers": RoleViewer,
		},
	}
}

func reqWith(secret, user, groups string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	if secret != "" {
		r.Header.Set("X-Pglockr-Proxy-Secret", secret)
	}
	if user != "" {
		r.Header.Set("X-Auth-Request-Email", user)
	}
	if groups != "" {
		r.Header.Set("X-Auth-Request-Groups", groups)
	}
	return r
}

func TestNewProxyIdentityValidation(t *testing.T) {
	if _, err := NewProxyIdentity(ProxyConfig{UserHeader: "U", TrustMode: "secret", SecretHeader: "S", GroupToRole: map[string]Role{"g": RoleViewer}}); err == nil {
		t.Error("secret mode without a secret value should error")
	}
	if _, err := NewProxyIdentity(ProxyConfig{UserHeader: "U", TrustMode: "bogus", GroupToRole: map[string]Role{"g": RoleViewer}}); err == nil {
		t.Error("invalid trustMode should error")
	}
	if _, err := NewProxyIdentity(ProxyConfig{UserHeader: "U", TrustMode: "network"}); err == nil {
		t.Error("no mappings and no defaultRole should error")
	}
}

func TestProxySecretMode(t *testing.T) {
	id, err := NewProxyIdentity(proxyCfg("secret"))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("valid secret + groups", func(t *testing.T) {
		p, ok := id.Authenticate(reqWith("s3cret", "ada@x.io", "other,pglockr-ops"))
		if !ok || p.Name != "ada@x.io" || p.Role != RoleOperator {
			t.Fatalf("got %+v ok=%v", p, ok)
		}
	})

	t.Run("FORGED: identity headers without secret are ignored", func(t *testing.T) {
		p, ok := id.Authenticate(reqWith("", "evil@x.io", "pglockr-admins"))
		if ok {
			t.Fatalf("forged headers must not authenticate, got %+v", p)
		}
	})

	t.Run("wrong secret is rejected", func(t *testing.T) {
		if _, ok := id.Authenticate(reqWith("nope", "evil@x.io", "pglockr-admins")); ok {
			t.Fatal("wrong secret must not authenticate")
		}
	})
}

func TestProxyNetworkMode(t *testing.T) {
	id, err := NewProxyIdentity(proxyCfg("network"))
	if err != nil {
		t.Fatal(err)
	}
	// No secret needed in network mode.
	p, ok := id.Authenticate(reqWith("", "ben@x.io", "pglockr-admins"))
	if !ok || p.Role != RoleAdmin {
		t.Fatalf("network mode: got %+v ok=%v", p, ok)
	}
}

func TestProxyHighestRoleWins(t *testing.T) {
	id, _ := NewProxyIdentity(proxyCfg("network"))
	// Member of both viewer and admin groups → admin.
	p, ok := id.Authenticate(reqWith("", "cleo@x.io", "pglockr-viewers,pglockr-admins"))
	if !ok || p.Role != RoleAdmin {
		t.Fatalf("highest role should win: got %+v", p)
	}
}

func TestProxyNoUserOrNoGroup(t *testing.T) {
	id, _ := NewProxyIdentity(proxyCfg("network"))

	if _, ok := id.Authenticate(reqWith("", "", "pglockr-admins")); ok {
		t.Error("missing user header must not authenticate")
	}
	if _, ok := id.Authenticate(reqWith("", "x@x.io", "unmapped-group")); ok {
		t.Error("user with no mapped group (and no defaultRole) must not authenticate")
	}
}

func TestProxyDefaultRole(t *testing.T) {
	cfg := proxyCfg("network")
	cfg.DefaultRole = RoleViewer
	id, _ := NewProxyIdentity(cfg)
	// Authenticated by proxy but no mapped group → falls back to viewer.
	p, ok := id.Authenticate(reqWith("", "guest@x.io", "some-unrelated-group"))
	if !ok || p.Role != RoleViewer {
		t.Fatalf("defaultRole fallback failed: got %+v ok=%v", p, ok)
	}
}

package auth

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
)

// ProxyConfig configures the trusted-reverse-proxy identity source: pglockr
// reads the user and groups from headers injected by an upstream authenticating
// proxy (oauth2-proxy, Istio, Pomerium) and maps groups to roles.
//
// Trust is established one of two ways (TrustMode):
//   - "secret": the proxy must present a shared secret header; without it the
//     identity headers are ignored. For proxies that can inject a static header.
//   - "network": headers are trusted unconditionally; the operator must ensure
//     pglockr is only reachable through the proxy (e.g. oauth2-proxy as the sole
//     ingress, pglockr bound to an internal network).
type ProxyConfig struct {
	UserHeader   string
	GroupsHeader string
	GroupsSep    string
	TrustMode    string // "secret" | "network"
	SecretHeader string
	Secret       string // resolved value the proxy must present (TrustMode=secret)
	DefaultRole  Role   // role when authenticated but no group matched ("" = no access)
	GroupToRole  map[string]Role
}

// ProxyIdentity authenticates requests using trusted proxy headers.
type ProxyIdentity struct {
	cfg ProxyConfig
}

// NewProxyIdentity validates cfg and returns a ProxyIdentity.
func NewProxyIdentity(cfg ProxyConfig) (*ProxyIdentity, error) {
	if cfg.UserHeader == "" {
		return nil, fmt.Errorf("proxy: userHeader is required")
	}
	switch cfg.TrustMode {
	case "secret":
		if cfg.SecretHeader == "" || cfg.Secret == "" {
			return nil, fmt.Errorf("proxy: trustMode 'secret' requires secretHeader and a secret value")
		}
	case "network":
		// no secret needed
	default:
		return nil, fmt.Errorf("proxy: invalid trustMode %q (want secret|network)", cfg.TrustMode)
	}
	if len(cfg.GroupToRole) == 0 && cfg.DefaultRole == "" {
		return nil, fmt.Errorf("proxy: configure at least one group→role mapping or a defaultRole")
	}
	if cfg.GroupsSep == "" {
		cfg.GroupsSep = ","
	}
	return &ProxyIdentity{cfg: cfg}, nil
}

// Authenticate resolves the proxy-provided identity to a Principal. It returns
// false (which surfaces as 401) when the trust check fails, the user header is
// absent, or the user maps to no role.
func (p *ProxyIdentity) Authenticate(r *http.Request) (Principal, bool) {
	// Trust boundary: without the shared secret, ignore the identity headers
	// entirely so a client that reaches pglockr directly cannot forge identity.
	if p.cfg.TrustMode == "secret" {
		presented := r.Header.Get(p.cfg.SecretHeader)
		if subtle.ConstantTimeCompare([]byte(presented), []byte(p.cfg.Secret)) != 1 {
			return Principal{}, false
		}
	}

	user := strings.TrimSpace(r.Header.Get(p.cfg.UserHeader))
	if user == "" {
		return Principal{}, false
	}

	role, ok := p.highestRole(r.Header.Get(p.cfg.GroupsHeader))
	if !ok {
		if p.cfg.DefaultRole == "" {
			return Principal{}, false // authenticated by proxy, but no pglockr access
		}
		role = p.cfg.DefaultRole
	}
	return Principal{Name: user, Role: role}, true
}

// highestRole maps the comma-separated groups header to the most privileged
// configured role.
func (p *ProxyIdentity) highestRole(groups string) (Role, bool) {
	best := Role("")
	found := false
	for _, g := range strings.Split(groups, p.cfg.GroupsSep) {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if role, ok := p.cfg.GroupToRole[g]; ok {
			if !found || role.rank() > best.rank() {
				best, found = role, true
			}
		}
	}
	return best, found
}

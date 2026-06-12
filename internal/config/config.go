// Package config loads pglockr configuration from a YAML file with environment
// overrides. Secrets (target DSN, auth token) are read from the environment and
// never from the open config file, and are never logged.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the fully resolved runtime configuration.
type Config struct {
	Cluster   ClusterConfig   `yaml:"cluster"`
	Poll      PollConfig      `yaml:"poll"`
	HTTP      HTTPConfig      `yaml:"http"`
	Auth      AuthConfig      `yaml:"auth"`
	Persist   PersistConfig   `yaml:"persist"`
	Redaction RedactionConfig `yaml:"redaction"`
}

// RedactionConfig controls masking of query texts at ingestion (spec §7).
// When enabled, literal values in queries are replaced with `?` before a
// snapshot reaches the ring buffer, persistent history, stream, or audit log.
type RedactionConfig struct {
	Enabled bool `yaml:"enabled"`
}

// PersistConfig controls durable snapshot history (SQLite). Disabled by
// default; the in-memory ring buffer is always present regardless.
type PersistConfig struct {
	Enabled   bool          `yaml:"enabled"`
	Path      string        `yaml:"-"` // from PGLOCKR_DB_PATH (a host/volume path)
	Retention time.Duration `yaml:"retention"`
}

// ClusterConfig describes the single target cluster (MVP: one cluster).
// DSN is intentionally not a YAML field — it must come from the environment.
type ClusterConfig struct {
	Name string `yaml:"name"`
	DSN  string `yaml:"-"`
}

// PollConfig controls the poller cadence and history retention.
type PollConfig struct {
	Interval         time.Duration `yaml:"interval"`
	RingSize         int           `yaml:"ringSize"`
	StatementTimeout time.Duration `yaml:"statementTimeout"`
	// WaitersOnly calls pg_blocking_pids only for backends already in a lock
	// wait, reducing overhead on busy clusters (spec 5.8).
	WaitersOnly bool `yaml:"waitersOnly"`
}

// HTTPConfig controls the embedded HTTP server.
type HTTPConfig struct {
	Addr string `yaml:"addr"`
}

// AuthConfig holds tool authentication. Tokens/secrets come from the
// environment.
//
// Mode selects the identity source: "token" (default) uses PGLOCKR_TOKEN and/or
// auth.principals; "proxy" trusts identity headers from an upstream SSO proxy.
type AuthConfig struct {
	Mode       string            `yaml:"mode"`
	Token      string            `yaml:"-"` // legacy single token (env PGLOCKR_TOKEN) → admin
	Principals []PrincipalConfig `yaml:"principals"`
	Proxy      ProxyAuthConfig   `yaml:"proxy"`
}

// ProxyAuthConfig configures the trusted-reverse-proxy identity source.
type ProxyAuthConfig struct {
	UserHeader   string              `yaml:"userHeader"`
	GroupsHeader string              `yaml:"groupsHeader"`
	GroupsSep    string              `yaml:"groupsDelimiter"`
	TrustMode    string              `yaml:"trustMode"`    // "secret" | "network"
	SecretHeader string              `yaml:"secretHeader"` // header the proxy presents
	SecretEnv    string              `yaml:"secretEnv"`    // env var holding the expected secret
	Secret       string              `yaml:"-"`            // resolved from SecretEnv
	DefaultRole  string              `yaml:"defaultRole"`  // role when no group matched
	RoleMappings []RoleMappingConfig `yaml:"roleMappings"`
}

// RoleMappingConfig maps an upstream group/claim value to a pglockr role.
type RoleMappingConfig struct {
	Group string `yaml:"group"`
	Role  string `yaml:"role"`
}

// PrincipalConfig is one named access principal. Its token is read from the
// environment variable named by TokenEnv (never stored in the YAML).
type PrincipalConfig struct {
	Name     string `yaml:"name"`
	Role     string `yaml:"role"`
	TokenEnv string `yaml:"tokenEnv"`
	Token    string `yaml:"-"` // resolved from TokenEnv
}

// Defaults returns a config populated with the MVP defaults from the spec.
func Defaults() Config {
	return Config{
		Cluster: ClusterConfig{Name: "default"},
		Poll: PollConfig{
			Interval:         time.Second,
			RingSize:         300,
			StatementTimeout: 3 * time.Second,
			WaitersOnly:      false,
		},
		HTTP:    HTTPConfig{Addr: ":8080"},
		Persist: PersistConfig{Enabled: false, Retention: 24 * time.Hour},
	}
}

// Environment variable names for secrets and common overrides.
const (
	EnvDSN      = "PGLOCKR_DSN"
	EnvToken    = "PGLOCKR_TOKEN"
	EnvHTTPAddr = "PGLOCKR_HTTP_ADDR"
	EnvCluster  = "PGLOCKR_CLUSTER"
	// EnvDBPath points at the SQLite history file (a host/volume path); setting
	// it also enables persistence.
	EnvDBPath = "PGLOCKR_DB_PATH"
	// EnvRedact ("1"/"true") enables query-text redaction, overriding YAML.
	EnvRedact = "PGLOCKR_REDACT"
)

// Load reads the YAML config at path (if non-empty), applies environment
// overrides, fills defaults, and validates. Secrets always come from the
// environment.
func Load(path string) (Config, error) {
	cfg := Defaults()

	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config %q: %w", path, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %q: %w", path, err)
		}
	}

	// Secrets and overrides from the environment.
	cfg.Cluster.DSN = os.Getenv(EnvDSN)
	cfg.Auth.Token = os.Getenv(EnvToken)
	// Resolve each principal's token from its named env var.
	for i := range cfg.Auth.Principals {
		if env := cfg.Auth.Principals[i].TokenEnv; env != "" {
			cfg.Auth.Principals[i].Token = os.Getenv(env)
		}
	}
	if v := os.Getenv(EnvHTTPAddr); v != "" {
		cfg.HTTP.Addr = v
	}
	if v := os.Getenv(EnvCluster); v != "" {
		cfg.Cluster.Name = v
	}
	// The DB path is a host/volume location, so it comes from the environment.
	// Providing it also enables persistence.
	if v := os.Getenv(EnvDBPath); v != "" {
		cfg.Persist.Path = v
		cfg.Persist.Enabled = true
	}
	if v := os.Getenv(EnvRedact); v == "1" || strings.EqualFold(v, "true") {
		cfg.Redaction.Enabled = true
	}
	// Resolve the proxy shared secret from its named env var.
	if env := cfg.Auth.Proxy.SecretEnv; env != "" {
		cfg.Auth.Proxy.Secret = os.Getenv(env)
	}

	// Auth + proxy defaults.
	if cfg.Auth.Mode == "" {
		cfg.Auth.Mode = "token"
	}
	if cfg.Auth.Mode == "proxy" {
		px := &cfg.Auth.Proxy
		if px.UserHeader == "" {
			px.UserHeader = "X-Auth-Request-Email"
		}
		if px.GroupsHeader == "" {
			px.GroupsHeader = "X-Auth-Request-Groups"
		}
		if px.GroupsSep == "" {
			px.GroupsSep = ","
		}
		if px.TrustMode == "" {
			px.TrustMode = "secret"
		}
		if px.SecretHeader == "" {
			px.SecretHeader = "X-Pglockr-Proxy-Secret"
		}
	}

	// Re-apply defaults for any zeroed durations/sizes after YAML parse.
	d := Defaults()
	if cfg.Persist.Retention <= 0 {
		cfg.Persist.Retention = d.Persist.Retention
	}
	if cfg.Poll.Interval <= 0 {
		cfg.Poll.Interval = d.Poll.Interval
	}
	if cfg.Poll.RingSize <= 0 {
		cfg.Poll.RingSize = d.Poll.RingSize
	}
	if cfg.Poll.StatementTimeout <= 0 {
		cfg.Poll.StatementTimeout = d.Poll.StatementTimeout
	}
	if cfg.HTTP.Addr == "" {
		cfg.HTTP.Addr = d.HTTP.Addr
	}
	if cfg.Cluster.Name == "" {
		cfg.Cluster.Name = d.Cluster.Name
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

var validRoles = map[string]bool{"viewer": true, "operator": true, "admin": true}

func (c Config) validate() error {
	if c.Cluster.DSN == "" {
		return fmt.Errorf("%s must be set (target database DSN)", EnvDSN)
	}
	switch c.Auth.Mode {
	case "token":
		if err := c.validateTokenAuth(); err != nil {
			return err
		}
	case "proxy":
		if err := c.validateProxyAuth(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("auth.mode %q invalid (want token|proxy)", c.Auth.Mode)
	}
	if c.Persist.Enabled && c.Persist.Path == "" {
		return fmt.Errorf("persistence enabled but no path set (%s)", EnvDBPath)
	}
	return nil
}

func (c Config) validateTokenAuth() error {
	if c.Auth.Token == "" && len(c.Auth.Principals) == 0 {
		return fmt.Errorf("no auth configured: set %s or define auth.principals", EnvToken)
	}
	for i, p := range c.Auth.Principals {
		if p.Name == "" {
			return fmt.Errorf("auth.principals[%d]: name is required", i)
		}
		if !validRoles[p.Role] {
			return fmt.Errorf("auth.principals[%d] (%s): invalid role %q (want viewer|operator|admin)", i, p.Name, p.Role)
		}
		if p.TokenEnv == "" {
			return fmt.Errorf("auth.principals[%d] (%s): tokenEnv is required", i, p.Name)
		}
		if p.Token == "" {
			return fmt.Errorf("auth.principals[%d] (%s): env %s is empty", i, p.Name, p.TokenEnv)
		}
	}
	return nil
}

func (c Config) validateProxyAuth() error {
	px := c.Auth.Proxy
	switch px.TrustMode {
	case "secret":
		if px.SecretEnv == "" {
			return fmt.Errorf("auth.proxy: trustMode 'secret' requires secretEnv")
		}
		if px.Secret == "" {
			return fmt.Errorf("auth.proxy: env %s (secret) is empty", px.SecretEnv)
		}
	case "network":
		// trusted by network isolation
	default:
		return fmt.Errorf("auth.proxy.trustMode %q invalid (want secret|network)", px.TrustMode)
	}
	if len(px.RoleMappings) == 0 && px.DefaultRole == "" {
		return fmt.Errorf("auth.proxy: define roleMappings or a defaultRole")
	}
	for i, m := range px.RoleMappings {
		if m.Group == "" || !validRoles[m.Role] {
			return fmt.Errorf("auth.proxy.roleMappings[%d]: need group and a valid role, got group=%q role=%q", i, m.Group, m.Role)
		}
	}
	if px.DefaultRole != "" && !validRoles[px.DefaultRole] {
		return fmt.Errorf("auth.proxy.defaultRole %q invalid", px.DefaultRole)
	}
	return nil
}

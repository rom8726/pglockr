// Command pglockr is a live visualizer of PostgreSQL locks and blocking trees.
// It polls a target cluster, builds a wait-for forest, streams it to an
// embedded web UI, and can cancel/terminate offending backends.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rom8726/pglockr/internal/api"
	"github.com/rom8726/pglockr/internal/audit"
	"github.com/rom8726/pglockr/internal/auth"
	"github.com/rom8726/pglockr/internal/config"
	"github.com/rom8726/pglockr/internal/persist"
	"github.com/rom8726/pglockr/internal/pg"
	"github.com/rom8726/pglockr/internal/poller"
	"github.com/rom8726/pglockr/internal/redact"
	"github.com/rom8726/pglockr/internal/setup"
	sig "github.com/rom8726/pglockr/internal/signal"
	"github.com/rom8726/pglockr/internal/store"
	"github.com/rom8726/pglockr/web"
)

func main() {
	// `pglockr grants ...` generates the provisioning SQL and exits; it needs no
	// DSN/token, so it is handled before the server flags/config.
	if len(os.Args) > 1 && os.Args[1] == "grants" {
		if err := runGrants(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	configPath := flag.String("config", "", "path to YAML config file (optional)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(*configPath, log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// runGrants implements `pglockr grants`: print a provisioning SQL script to
// stdout (pipe it to psql) and human guidance to stderr. The operator reviews
// and applies it; pglockr never runs it itself.
func runGrants(args []string) error {
	fs := flag.NewFlagSet("grants", flag.ExitOnError)
	role := fs.String("role", "pglockr_ro", "role name to create")
	password := fs.String("password", "", "login password (default: generate a strong random one)")
	noSignal := fs.Bool("no-signal", false, "omit pg_signal_backend (read-only viewer; no cancel/terminate)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pw := *password
	generated := false
	if pw == "" {
		var err error
		if pw, err = setup.RandomPassword(); err != nil {
			return err
		}
		generated = true
	}

	// SQL to stdout so it can be piped straight into psql.
	fmt.Print(setup.Script(setup.GrantOptions{Role: *role, Password: pw, Signal: !*noSignal}))

	// Guidance and the (possibly generated) password to stderr.
	fmt.Fprintln(os.Stderr, "\n# Review the SQL above, then apply it as a superuser, e.g.:")
	fmt.Fprintf(os.Stderr, "#   pglockr grants --role %s | psql \"postgres://postgres@HOST:5432/DBNAME\"\n", *role)
	if generated {
		fmt.Fprintf(os.Stderr, "#\n# Generated password for %q (save it — shown once):\n#   %s\n", *role, pw)
	}
	fmt.Fprintf(os.Stderr, "#\n# Then point pglockr at it:\n#   PGLOCKR_DSN=\"postgres://%s:PASSWORD@HOST:5432/DBNAME\"\n", *role)
	return nil
}

func run(configPath string, log *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	// Connect to the target cluster.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	client, err := pg.Connect(connectCtx, cfg.Cluster.DSN, cfg.Poll.StatementTimeout)
	cancel()
	if err != nil {
		return err
	}
	defer client.Close()
	log.Info("connected to target",
		"cluster", cfg.Cluster.Name,
		"server_version_num", client.ServerVersionNum())

	// Preflight: check the role has the privileges pglockr needs, and tell the
	// operator exactly how to fix it if not. Non-fatal — the tool still runs
	// (degraded) without them.
	preflight(ctx, client, log)

	storage := store.New(cfg.Poll.RingSize)

	// Audit sink: durable (SQLite, shared with history) when persistence is on,
	// otherwise a bounded in-memory trail.
	var auditSink audit.Sink = audit.NewMemory(1000)

	// Optional durable history so the scrubber survives restarts (spec 5.9).
	if cfg.Persist.Enabled {
		hist, err := persist.Open(cfg.Persist.Path, cfg.Persist.Retention, log)
		if err != nil {
			return err
		}
		defer hist.Close()
		storage.SetPersister(hist, log)
		auditSink = hist
		log.Info("history persistence enabled",
			"path", cfg.Persist.Path, "retention", cfg.Persist.Retention)
	}

	// Redaction: mask literal values in query texts at ingestion, so raw texts
	// never reach the ring, history, stream, or audit (spec §7).
	var source poller.Source = client
	if cfg.Redaction.Enabled {
		source = redact.NewSource(client)
		log.Info("query-text redaction enabled")
	}

	pollerSvc := poller.New(cfg.Cluster.Name, source, storage, cfg.Poll.Interval, log)
	go pollerSvc.Run(ctx)

	// Audit lookup: the victim's current query from the latest snapshot (already
	// redacted when redaction is enabled).
	lookup := func(pid int) (string, bool) {
		snap, ok := storage.Latest()
		if !ok {
			return "", false
		}
		sess, ok := snap.Sessions[pid]
		return sess.Query, ok
	}
	signalSvc := sig.New(client, lookup, log, auditSink)

	ui, err := web.DistFS()
	if err != nil {
		return err
	}

	identity, err := buildIdentity(cfg.Auth)
	if err != nil {
		return err
	}

	srv := api.New(api.Config{
		Cluster:   cfg.Cluster.Name,
		Store:     storage,
		Poller:    pollerSvc,
		Signal:    signalSvc,
		Inspector: client,
		Audit:     auditSink,
		Auth:      auth.New(identity),
		UI:        ui,
		Log:       log,
	})

	httpSrv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Shut the HTTP server down gracefully when the context is cancelled.
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	log.Info("listening", "addr", cfg.HTTP.Addr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

// buildIdentity assembles the token identity from config: the legacy single
// token (admin) plus any named principals.
func buildIdentity(cfg config.AuthConfig) (auth.Identity, error) {
	var entries []auth.TokenPrincipal
	if cfg.Token != "" {
		entries = append(entries, auth.TokenPrincipal{Name: "default", Role: auth.RoleAdmin, Token: cfg.Token})
	}
	for _, p := range cfg.Principals {
		role, err := auth.ParseRole(p.Role)
		if err != nil {
			return nil, fmt.Errorf("principal %q: %w", p.Name, err)
		}
		entries = append(entries, auth.TokenPrincipal{Name: p.Name, Role: role, Token: p.Token})
	}
	return auth.NewTokenIdentity(entries), nil
}

// preflight checks the connected role's privileges and, if any are missing,
// prints the exact GRANT statements to fix it. It never fails startup — pglockr
// degrades gracefully (e.g. hidden query texts without pg_monitor).
func preflight(ctx context.Context, client *pg.Client, log *slog.Logger) {
	caps, err := client.Capabilities(ctx)
	if err != nil {
		log.Warn("could not determine role capabilities", "err", err)
		return
	}
	log.Info("role capabilities",
		"role", caps.Role,
		"superuser", caps.IsSuperuser,
		"can_read_stats", caps.CanReadStats,
		"can_signal", caps.CanSignal)

	needStats, needSignal := !caps.CanReadStats, !caps.CanSignal
	if !needStats && !needSignal {
		return
	}
	if needStats {
		log.Warn("missing pg_monitor — query texts of other backends will be hidden", "role", caps.Role)
	}
	if needSignal {
		log.Warn("missing pg_signal_backend — cancel/terminate will not work for other backends", "role", caps.Role)
	}
	fmt.Fprintf(os.Stderr,
		"\n# pglockr: role %q is missing privileges. Apply these as a superuser:\n%s\n\n",
		caps.Role, setup.Remediation(caps.Role, needStats, needSignal))
}

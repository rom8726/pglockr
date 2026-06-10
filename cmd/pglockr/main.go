// Command pglockr is a live visualizer of PostgreSQL locks and blocking trees.
// It polls a target cluster, builds a wait-for forest, streams it to an
// embedded web UI, and can cancel/terminate offending backends.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rom8726/pglockr/internal/api"
	"github.com/rom8726/pglockr/internal/auth"
	"github.com/rom8726/pglockr/internal/config"
	"github.com/rom8726/pglockr/internal/pg"
	"github.com/rom8726/pglockr/internal/poller"
	sig "github.com/rom8726/pglockr/internal/signal"
	"github.com/rom8726/pglockr/internal/store"
	"github.com/rom8726/pglockr/web"
)

func main() {
	configPath := flag.String("config", "", "path to YAML config file (optional)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(*configPath, log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
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

	st := store.New(cfg.Poll.RingSize)

	p := poller.New(cfg.Cluster.Name, client, st, cfg.Poll.Interval, log)
	go p.Run(ctx)

	// Audit lookup: the victim's current query from the latest snapshot.
	lookup := func(pid int) (string, bool) {
		snap, ok := st.Latest()
		if !ok {
			return "", false
		}
		s, ok := snap.Sessions[pid]
		return s.Query, ok
	}
	signalSvc := sig.New(client, lookup, log)

	ui, err := web.DistFS()
	if err != nil {
		return err
	}

	srv := api.New(api.Config{
		Cluster: cfg.Cluster.Name,
		Store:   st,
		Poller:  p,
		Signal:  signalSvc,
		Auth:    auth.New(cfg.Auth.Token),
		UI:      ui,
		Log:     log,
	})

	httpSrv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Shut the HTTP server down gracefully when the context is cancelled.
	go func() {
		<-ctx.Done()
		shutdownCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	log.Info("listening", "addr", cfg.HTTP.Addr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

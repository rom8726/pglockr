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
	"github.com/rom8726/pglockr/internal/persist"
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

	storage := store.New(cfg.Poll.RingSize)

	// Optional durable history so the scrubber survives restarts (spec 5.9).
	if cfg.Persist.Enabled {
		hist, err := persist.Open(cfg.Persist.Path, cfg.Persist.Retention, log)
		if err != nil {
			return err
		}
		defer hist.Close()
		storage.SetPersister(hist, log)
		log.Info("history persistence enabled",
			"path", cfg.Persist.Path, "retention", cfg.Persist.Retention)
	}

	pollerSvc := poller.New(cfg.Cluster.Name, client, storage, cfg.Poll.Interval, log)
	go pollerSvc.Run(ctx)

	// Audit lookup: the victim's current query from the latest snapshot.
	lookup := func(pid int) (string, bool) {
		snap, ok := storage.Latest()
		if !ok {
			return "", false
		}
		sess, ok := snap.Sessions[pid]
		return sess.Query, ok
	}
	signalSvc := sig.New(client, lookup, log)

	ui, err := web.DistFS()
	if err != nil {
		return err
	}

	srv := api.New(api.Config{
		Cluster:   cfg.Cluster.Name,
		Store:     storage,
		Poller:    pollerSvc,
		Signal:    signalSvc,
		Inspector: client,
		Auth:      auth.New(cfg.Auth.Token),
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

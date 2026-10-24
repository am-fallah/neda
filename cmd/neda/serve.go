package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/am-fallah/neda/internal/config"
	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/httpapi"
	"github.com/am-fallah/neda/internal/logging"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/scheduler"
	"github.com/am-fallah/neda/internal/store"
)

func runServe(stdout, stderr io.Writer) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "neda: configuration error: %v\n", err)
		return 1
	}
	logger := logging.NewWithWriter(stdout, cfg.LogLevel, cfg.LogFormat)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		logger.Error("database open failed", "error", err)
		return 1
	}
	defer func() {
		if err := database.Close(); err != nil {
			logger.Error("database close failed", "error", err)
		}
	}()

	applied, err := db.Migrate(ctx, database.DB, migrations.FS)
	if err != nil {
		logger.Error("database migration failed", "error", err)
		return 1
	}
	if err := database.Ready(ctx, applied); err != nil {
		logger.Error("database readiness failed", "error", err)
		return 1
	}
	logger.Info("database ready", "migrations", applied)

	appStore := store.New(database.DB)
	if err := appStore.EnsurePollInterval(ctx, time.Now().UTC()); err != nil {
		logger.Error("poll interval initialization failed", "error", err)
		return 1
	}
	pollInterval, err := appStore.PollInterval(ctx)
	if err != nil {
		logger.Error("poll interval configuration is invalid", "error", err)
		return 1
	}

	// PRV-009 (production provider configuration) remains deferred. SCH-010
	// will compose the unwired SCH-004 check stage into a production runner;
	// until then the scheduler is intentionally idle, so this empty registry is
	// never consulted for dispatch in this slice.
	providerRegistry, err := provider.NewRegistry()
	if err != nil {
		logger.Error("provider registry initialization failed", "error", err)
		return 1
	}
	// The waker is the scheduler's wake source and the intended post-commit
	// hook for MonitorService when the API layer composes it. SCH-004 provides
	// the unwired check stage; SCH-010 will compose it into the production
	// runner, so the scheduler remains idle in this slice.
	waker := scheduler.NewWaker()
	sched, err := scheduler.New(scheduler.Config{
		Repository: appStore,
		Providers:  providerRegistry,
		Interval:   appStore,
		Runner:     nil,
		Workers:    cfg.Workers,
		Wake:       waker.C(),
		Logger:     logger,
	})
	if err != nil {
		logger.Error("scheduler initialization failed", "error", err)
		return 1
	}
	logger.Info("scheduler configured",
		"workers", cfg.Workers,
		"poll_interval", pollInterval.String(),
		"provider_spacing", scheduler.DefaultProviderSpacing.String(),
		"mode", "idle-until-check-runner",
	)

	server := httpapi.New(httpapi.Config{
		Addr:       cfg.HTTPAddr,
		Production: cfg.IsProduction(),
		PublicURL:  cfg.PublicURL,
	}, logger, database, applied)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	serverErr := make(chan error, 1)
	go func() {
		err := server.Run(runCtx)
		cancel()
		serverErr <- err
	}()

	schedErr := sched.Run(runCtx)
	unexpectedStop := schedErr == nil && runCtx.Err() == nil
	cancel()
	serverRunErr := <-serverErr

	if schedErr != nil {
		logger.Error("scheduler stopped with error", "error", schedErr)
	}
	if unexpectedStop {
		logger.Error("scheduler stopped unexpectedly")
	}
	if serverRunErr != nil {
		logger.Error("server stopped with error", "error", serverRunErr)
	}
	if schedErr != nil || unexpectedStop || serverRunErr != nil {
		return 1
	}
	logger.Info("service stopped")
	return 0
}

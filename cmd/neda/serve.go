package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/am-fallah/neda/internal/config"
	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/httpapi"
	"github.com/am-fallah/neda/internal/logging"
	"github.com/am-fallah/neda/internal/migrations"
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

	server := httpapi.New(httpapi.Config{
		Addr:       cfg.HTTPAddr,
		Production: cfg.IsProduction(),
		PublicURL:  cfg.PublicURL,
	}, logger, database, applied)

	if err := server.Run(ctx); err != nil {
		logger.Error("server stopped with error", "error", err)
		return 1
	}
	logger.Info("service stopped")
	return 0
}

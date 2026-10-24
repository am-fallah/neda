package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/am-fallah/neda/internal/auth"
	"github.com/am-fallah/neda/internal/config"
	"github.com/am-fallah/neda/internal/crypto"
	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/httpapi"
	"github.com/am-fallah/neda/internal/logging"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/notify"
	notifydelivery "github.com/am-fallah/neda/internal/notify/delivery"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
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

	authManager, err := auth.NewManager(appStore, auth.Config{
		SessionTTL:  cfg.SessionTTL,
		SessionIdle: cfg.SessionIdle,
		Secure:      cfg.IsProduction(),
		PublicURL:   cfg.PublicURL,
		Production:  cfg.IsProduction(),
		Now:         time.Now,
	})
	if err != nil {
		logger.Error("auth manager initialization failed", "error", err)
		return 1
	}
	loginLimiter, err := auth.NewRateLimiter(auth.DefaultRateLimitConfig(), time.Now)
	if err != nil {
		logger.Error("login limiter initialization failed", "error", err)
		return 1
	}
	loginGuard := auth.NewLoginGuard(auth.OriginPolicy{
		PublicURL:  cfg.PublicURL,
		Production: cfg.IsProduction(),
	}, loginLimiter, cfg.TrustProxy)

	// P2/P3: load the external key only when configured. Development startup
	// without a key remains usable; Telegram delivery is simply unavailable.
	var telegramSettings *telegramsettings.Settings
	var deliverySource scheduler.DeliverySource
	if cfg.EncryptionKeyFile != "" {
		key, err := crypto.LoadKeyFile(cfg.EncryptionKeyFile)
		if err != nil {
			logger.Error("encryption key load failed", "error", err)
			return 1
		}
		defer key.Zero()
		telegramSettings, err = telegramsettings.New(appStore, key, time.Now)
		if err != nil {
			logger.Error("telegram settings initialization failed", "error", err)
			return 1
		}
		deliverySource = telegramSettings.DeliveryDestinations
	}

	// PRV-009 (production provider configuration) remains deferred, so this
	// registry is empty and no target can be dispatched until provider
	// integration is authorized. SCH-010 still composes the production check
	// stage and outcome runner so the scheduler path is complete and tested;
	// the empty registry keeps it inert.
	providerRegistry, err := provider.NewRegistry()
	if err != nil {
		logger.Error("provider registry initialization failed", "error", err)
		return 1
	}
	checkStage, err := scheduler.NewCheckStage(scheduler.CheckStageConfig{
		Repository: appStore,
		Providers:  providerRegistry,
		Logger:     logger,
	})
	if err != nil {
		logger.Error("check stage initialization failed", "error", err)
		return 1
	}
	// P3 supplies the Telegram destination when encrypted settings exist; Push
	// destinations remain owned by PSH-002/PSH-004. The runner still commits
	// events atomically even when no delivery destination is configured.
	outcomeRunner, err := scheduler.NewOutcomeRunner(scheduler.OutcomeRunnerConfig{
		Stage:      checkStage,
		Store:      appStore,
		Deliveries: deliverySource,
		Logger:     logger,
	})
	if err != nil {
		logger.Error("outcome runner initialization failed", "error", err)
		return 1
	}
	// The waker is the scheduler's wake source and the intended post-commit
	// hook for MonitorService when the API layer composes it.
	waker := scheduler.NewWaker()
	sched, err := scheduler.New(scheduler.Config{
		Repository: appStore,
		Providers:  providerRegistry,
		Interval:   appStore,
		Runner:     outcomeRunner,
		Workers:    cfg.Workers,
		Wake:       waker.C(),
		Logger:     logger,
	})
	if err != nil {
		logger.Error("scheduler initialization failed", "error", err)
		return 1
	}
	var deliveryWorker *notifydelivery.Worker
	if telegramSettings != nil {
		telegramClient, err := notify.NewTelegramClient(notify.TelegramClientConfig{})
		if err != nil {
			logger.Error("telegram client initialization failed", "error", err)
			return 1
		}
		telegramSender, err := notifydelivery.NewTelegramSender(telegramSettings, telegramClient)
		if err != nil {
			logger.Error("telegram sender initialization failed", "error", err)
			return 1
		}
		deliveryWorker, err = notifydelivery.New(notifydelivery.Config{
			DB:      database.DB,
			Senders: map[notify.Channel]notifydelivery.Sender{notify.ChannelTelegram: telegramSender},
			Logger:  logger,
		})
		if err != nil {
			logger.Error("delivery worker initialization failed", "error", err)
			return 1
		}
	}

	logger.Info("scheduler configured",
		"workers", cfg.Workers,
		"poll_interval", pollInterval.String(),
		"provider_spacing", scheduler.DefaultProviderSpacing.String(),
		"delivery_worker", deliveryWorker != nil,
		"mode", "runner-composed-empty-provider-registry",
	)

	server := httpapi.New(httpapi.Config{
		Addr:       cfg.HTTPAddr,
		Production: cfg.IsProduction(),
		PublicURL:  cfg.PublicURL,
		Auth:       authManager,
		LoginGuard: loginGuard,
	}, logger, database, applied)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var deliveryErr <-chan error
	if deliveryWorker != nil {
		ch := make(chan error, 1)
		deliveryErr = ch
		go func() { ch <- deliveryWorker.Run(runCtx) }()
	}

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
	var deliveryRunErr error
	if deliveryErr != nil {
		deliveryRunErr = <-deliveryErr
	}

	if schedErr != nil {
		logger.Error("scheduler stopped with error", "error", schedErr)
	}
	if unexpectedStop {
		logger.Error("scheduler stopped unexpectedly")
	}
	if serverRunErr != nil {
		logger.Error("server stopped with error", "error", serverRunErr)
	}
	if deliveryRunErr != nil {
		logger.Error("delivery worker stopped with error", "error", deliveryRunErr)
	}
	if schedErr != nil || unexpectedStop || serverRunErr != nil || deliveryRunErr != nil {
		return 1
	}
	logger.Info("service stopped")
	return 0
}

package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/am-fallah/neda/internal/auth"
	"github.com/am-fallah/neda/internal/config"
	"github.com/am-fallah/neda/internal/crypto"
	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/httpapi"
	"github.com/am-fallah/neda/internal/logging"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/notify"
	notifydelivery "github.com/am-fallah/neda/internal/notify/delivery"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/provider/fake"
	pushservice "github.com/am-fallah/neda/internal/push"
	"github.com/am-fallah/neda/internal/retention"
	"github.com/am-fallah/neda/internal/scheduler"
	appsettings "github.com/am-fallah/neda/internal/settings"
	"github.com/am-fallah/neda/internal/store"
	"github.com/am-fallah/neda/internal/webui"
	webassets "github.com/am-fallah/neda/web"
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
	if err := appStore.EnsurePushEnabled(ctx, time.Now().UTC()); err != nil {
		logger.Error("push setting initialization failed", "error", err)
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

	// P2/P3/PSH: load the external key only when configured. Development
	// startup without a key remains usable; encrypted channels are simply
	// unavailable.
	var telegramSettings *telegramsettings.Settings
	var telegramClient *notify.TelegramClient
	var pushService *pushservice.Service
	var pushSender *pushservice.Sender
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
		telegramClient, err = notify.NewTelegramClient(notify.TelegramClientConfig{})
		if err != nil {
			logger.Error("telegram client initialization failed", "error", err)
			return 1
		}
		pushService, err = pushservice.New(appStore, database.DB, key, cfg.VAPIDSubject, time.Now)
		if err != nil {
			logger.Error("web push initialization failed", "error", err)
			return 1
		}
		if err := pushService.Ensure(ctx); err != nil {
			logger.Error("VAPID key initialization failed", "error", err)
			return 1
		}
		pushSender, err = pushservice.NewSender(pushService, pushservice.SenderConfig{
			PublicURL: publicURLString(cfg.PublicURL),
			Now:       time.Now,
		})
		if err != nil {
			logger.Error("web push sender initialization failed", "error", err)
			return 1
		}
		deliverySource = composeDeliverySources(
			telegramSettings.DeliveryDestinations,
			pushService.DeliveryDestinations,
		)
	}
	if pushService != nil && !pushService.Available() {
		logger.Warn("web push keys exist but NEDA_VAPID_SUBJECT is not configured; push stays unavailable")
	}
	settingsService, err := appsettings.New(appStore, telegramSettings, telegramClient, time.Now)
	if err != nil {
		logger.Error("settings service initialization failed", "error", err)
		return 1
	}

	// PRV-009 (production provider configuration) remains deferred, so the
	// production registry is empty. Development/test may explicitly enable the
	// deterministic fake provider; it is never registered in production.
	providerRegistry, fakeProviderEnabled, err := providerRegistryForConfig(cfg)
	if err != nil {
		logger.Error("provider registry initialization failed", "error", err)
		return 1
	}
	// The waker is the scheduler's wake source and the post-commit hook for
	// monitor CRUD changes.
	waker := scheduler.NewWaker()
	monitorService, err := domain.NewMonitorService(appStore, providerRegistry, domain.MonitorServiceConfig{
		Now: time.Now,
		NearTermSchedule: func(now time.Time) time.Time {
			interval := pollInterval
			if current, err := appStore.PollInterval(context.Background()); err == nil && current > 0 {
				interval = current
			}
			return now.Add(scheduler.NearTermDelay(interval, nil))
		},
		Wake: waker.Wake,
	})
	if err != nil {
		logger.Error("monitor service initialization failed", "error", err)
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
	senders := map[notify.Channel]notifydelivery.Sender{}
	if telegramSettings != nil {
		telegramSender, err := notifydelivery.NewTelegramSender(telegramSettings, telegramClient)
		if err != nil {
			logger.Error("telegram sender initialization failed", "error", err)
			return 1
		}
		senders[notify.ChannelTelegram] = telegramSender
	}
	if pushSender != nil {
		senders[notify.ChannelPush] = pushSender
	}
	if len(senders) > 0 {
		deliveryWorker, err = notifydelivery.New(notifydelivery.Config{
			DB:      database.DB,
			Senders: senders,
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
		"fake_provider", fakeProviderEnabled,
		"mode", "runner-composed",
	)

	webHandler := webui.New(webassets.Dist())
	server := httpapi.New(httpapi.Config{
		Addr:       cfg.HTTPAddr,
		Production: cfg.IsProduction(),
		PublicURL:  cfg.PublicURL,
		Auth:       authManager,
		LoginGuard: loginGuard,
		Monitors:   monitorService,
		Settings:   settingsService,
		Store:      appStore,
		Providers:  providerRegistry,
		Push:       pushService,
		PushTester: pushSender,
		WebUI:      webHandler,
	}, logger, database, applied)
	if !webHandler.Built() {
		logger.Warn("frontend assets are not embedded; serving API only (run npm run build in web/)")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	retentionWorker, err := retention.New(database.DB, retention.Config{Logger: logger})
	if err != nil {
		logger.Error("retention worker initialization failed", "error", err)
		return 1
	}
	retentionDone := make(chan struct{})
	go func() {
		defer close(retentionDone)
		_ = retentionWorker.Run(runCtx)
	}()

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
	<-retentionDone
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

// composeDeliverySources runs every configured channel source in the caller's
// outcome transaction. An error from any source fails the transaction so an
// event is never committed with a partially resolved outbox.
func composeDeliverySources(sources ...scheduler.DeliverySource) scheduler.DeliverySource {
	active := make([]scheduler.DeliverySource, 0, len(sources))
	for _, source := range sources {
		if source != nil {
			active = append(active, source)
		}
	}
	if len(active) == 0 {
		return nil
	}
	return func(ctx context.Context, tx *sql.Tx) ([]notify.Delivery, error) {
		deliveries := make([]notify.Delivery, 0)
		for _, source := range active {
			next, err := source(ctx, tx)
			if err != nil {
				return nil, err
			}
			deliveries = append(deliveries, next...)
		}
		return deliveries, nil
	}
}

func publicURLString(publicURL *url.URL) string {
	if publicURL == nil {
		return ""
	}
	return publicURL.String()
}

func providerRegistryForConfig(cfg config.Config) (*provider.Registry, bool, error) {
	providers := make([]provider.Provider, 0, 1)
	fakeEnabled := cfg.FakeProviderEnabled && !cfg.IsProduction()
	if fakeEnabled {
		providers = append(providers, fake.New())
	}
	registry, err := provider.NewRegistry(providers...)
	if err != nil {
		return nil, false, err
	}
	return registry, fakeEnabled, nil
}

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
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
	"github.com/am-fallah/neda/internal/provider/mrbilit"
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
	if cfg.AllowInsecureHTTP && cfg.PublicURL != nil && strings.EqualFold(cfg.PublicURL.Scheme, "http") {
		logger.Warn("plaintext HTTP access enabled; traffic is not encrypted and browser cookies will not use the Secure flag")
	}

	if err := validateRuntimeUser(os.Geteuid(), cfg.IsProduction()); err != nil {
		logger.Error("runtime user validation failed", "error", err)
		return 1
	}

	// Construct the provider registry before opening the database so a
	// production misconfiguration fails before any persistent state changes.
	providerRegistry, providerFlags, err := providerRegistryForConfig(cfg, logger)
	if err != nil {
		logger.Error("provider registry initialization failed", "error", err)
		return 1
	}

	// Load the key before opening the database so the persisted-secret check can
	// run before migrations or settings seeding.
	var encryptionKey *crypto.Key
	if cfg.EncryptionKeyFile != "" {
		encryptionKey, err = crypto.LoadKeyFile(cfg.EncryptionKeyFile)
		if err != nil {
			logger.Error("encryption key load failed", "error", err)
			return 1
		}
		defer encryptionKey.Zero()
	}

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

	if encryptionKey != nil {
		if err := validateKeyDatabaseConsistency(ctx, database.DB, encryptionKey); err != nil {
			logger.Error("encryption key/database consistency check failed", "error", err)
			return 1
		}
	}

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

	// Check persisted provider references before seeding any settings so a
	// production coverage failure does not write defaults to the database.
	if err := validateProviderCoverage(ctx, appStore, providerRegistry, time.Now().UTC(), cfg.IsProduction(), logger); err != nil {
		logger.Error("provider coverage check failed", "error", err)
		return 1
	}
	logger.Info("provider registry ready", "providers", registryProviderIDs(providerRegistry))

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
		Secure:      cfg.SecureCookies(),
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

	// P2/P3/PSH: encrypted channels are available only when a key is configured.
	// Development startup without a key remains usable.
	var telegramSettings *telegramsettings.Settings
	var telegramClient *notify.TelegramClient
	var pushService *pushservice.Service
	var pushSender *pushservice.Sender
	var deliverySource scheduler.DeliverySource
	if encryptionKey != nil {
		telegramSettings, err = telegramsettings.New(appStore, encryptionKey, time.Now)
		if err != nil {
			logger.Error("telegram settings initialization failed", "error", err)
			return 1
		}
		telegramClient, err = notify.NewTelegramClient(notify.TelegramClientConfig{})
		if err != nil {
			logger.Error("telegram client initialization failed", "error", err)
			return 1
		}
		pushService, err = pushservice.New(appStore, database.DB, encryptionKey, cfg.VAPIDSubject, time.Now)
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
		"fake_provider", providerFlags.FakeEnabled,
		"mrbilit_provider", providerFlags.MrBilitEnabled,
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

// validateRuntimeUser makes the non-root requirement explicit for production
// even if a Compose override or host launcher supplies UID 0.
func validateRuntimeUser(uid int, production bool) error {
	if production && uid == 0 {
		return errors.New("production refuses to run as root (uid 0); set a non-root runtime UID")
	}
	return nil
}

// validateKeyDatabaseConsistency verifies that the configured key can decrypt
// every persisted secret before migrations or settings seeding. A fresh database
// has no settings table or secret rows and passes.
func validateKeyDatabaseConsistency(ctx context.Context, database *sql.DB, key *crypto.Key) error {
	if database == nil || key == nil {
		return errors.New("key/database consistency requires a database and key")
	}
	var tableCount int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'settings'`,
	).Scan(&tableCount); err != nil {
		return fmt.Errorf("inspect settings table: %w", err)
	}
	if tableCount == 0 {
		return nil
	}
	rows, err := database.QueryContext(ctx,
		`SELECT key, value FROM settings WHERE is_secret = 1`,
	)
	if err != nil {
		return fmt.Errorf("read encrypted settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var settingKey, ciphertext string
		if err := rows.Scan(&settingKey, &ciphertext); err != nil {
			return fmt.Errorf("scan encrypted setting: %w", err)
		}
		if _, err := key.Decrypt(settingKey, ciphertext); err != nil {
			return fmt.Errorf("encrypted setting %q cannot be decrypted with the configured key", settingKey)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read encrypted settings: %w", err)
	}
	return nil
}

type scheduleLister interface {
	ListScheduleTargets(ctx context.Context, now time.Time) ([]domain.ScheduleTarget, error)
}

// validateProviderCoverage prevents active monitors from being silently
// skipped when their persisted provider is not registered. Production must
// fail closed; development may continue with a visible warning.
func validateProviderCoverage(
	ctx context.Context,
	schedule scheduleLister,
	registry *provider.Registry,
	now time.Time,
	production bool,
	logger *slog.Logger,
) error {
	if schedule == nil || registry == nil {
		return errors.New("provider coverage requires a schedule repository and registry")
	}
	targets, err := schedule.ListScheduleTargets(ctx, now)
	if err != nil {
		return fmt.Errorf("list active targets: %w", err)
	}
	missing := make(map[provider.ID]struct{})
	for _, target := range targets {
		if _, ok := registry.Lookup(target.ProviderID); !ok {
			missing[target.ProviderID] = struct{}{}
		}
	}
	if len(missing) == 0 {
		if logger != nil {
			logger.Info("provider coverage ready", "active_targets", len(targets))
		}
		return nil
	}
	ids := make([]string, 0, len(missing))
	for id := range missing {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	if production {
		return fmt.Errorf("active monitors require unregistered providers: %s", strings.Join(ids, ", "))
	}
	if logger != nil {
		logger.Warn("active monitors require unregistered providers", "providers", ids)
	}
	return nil
}

func registryProviderIDs(registry *provider.Registry) []string {
	if registry == nil {
		return nil
	}
	metadata := registry.List()
	ids := make([]string, 0, len(metadata))
	for _, item := range metadata {
		ids = append(ids, string(item.ID))
	}
	return ids
}

type providerFlags struct {
	FakeEnabled    bool
	MrBilitEnabled bool
}

func providerRegistryForConfig(cfg config.Config, logger *slog.Logger) (*provider.Registry, providerFlags, error) {
	if cfg.IsProduction() {
		if !cfg.MrBilitEnabled {
			return nil, providerFlags{}, errors.New("NEDA_MRBILIT_ENABLED must be true in production; the real provider is required")
		}
		if cfg.FakeProviderEnabled {
			return nil, providerFlags{}, errors.New("NEDA_FAKE_PROVIDER_ENABLED is not allowed in production")
		}
	}

	providers := make([]provider.Provider, 0, 2)
	flags := providerFlags{}
	if !cfg.IsProduction() || cfg.MrBilitEnabled {
		adapter, err := mrbilit.New(mrbilit.Config{NoRetries: true, Logger: logger})
		if err != nil {
			return nil, providerFlags{}, err
		}
		providers = append(providers, adapter)
		flags.MrBilitEnabled = true
	}
	if cfg.FakeProviderEnabled && !cfg.IsProduction() {
		providers = append(providers, fake.New())
		flags.FakeEnabled = true
	}
	if cfg.IsProduction() && len(providers) == 0 {
		return nil, providerFlags{}, errors.New("production provider registry is empty")
	}
	registry, err := provider.NewRegistry(providers...)
	if err != nil {
		return nil, providerFlags{}, err
	}
	return registry, flags, nil
}

package report

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
	"github.com/am-fallah/neda/internal/store"
)

// Sender delivers one fully rendered report. It must not retry internally and
// must classify missing configuration through the telegramsettings errors.
type Sender interface {
	Send(ctx context.Context, text string) (messageID int64, err error)
}

// Config controls the daily report worker.
type Config struct {
	DB           *sql.DB
	Store        *store.Store
	Sender       Sender
	Location     *time.Location
	Now          func() time.Time
	PollInterval time.Duration
	Lease        time.Duration
	MaxAttempts  int
	RetryDelays  []time.Duration
	Logger       *slog.Logger
}

// Worker runs the report scheduler and delivery lifecycle.
type Worker struct {
	db           *sql.DB
	store        *store.Store
	sender       Sender
	loc          *time.Location
	now          func() time.Time
	pollInterval time.Duration
	lease        time.Duration
	maxAttempts  int
	retryDelays  []time.Duration
	logger       *slog.Logger
}

// New validates dependencies and applies safe defaults.
func New(cfg Config) (*Worker, error) {
	if cfg.DB == nil {
		return nil, errors.New("report: nil database")
	}
	if cfg.Store == nil {
		return nil, errors.New("report: nil store")
	}
	if cfg.Sender == nil {
		return nil, errors.New("report: nil sender")
	}
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	if cfg.PollInterval < 10*time.Second || cfg.PollInterval > time.Hour {
		return nil, errors.New("report: poll interval must be between 10s and 1h")
	}
	if cfg.Lease == 0 {
		cfg.Lease = DefaultLease
	}
	if cfg.Lease < time.Minute || cfg.Lease > time.Hour {
		return nil, errors.New("report: lease must be between 1m and 1h")
	}
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}
	if cfg.MaxAttempts < 1 || cfg.MaxAttempts > 10 {
		return nil, errors.New("report: max attempts must be between 1 and 10")
	}
	retryDelays := cfg.RetryDelays
	if retryDelays == nil {
		retryDelays = append([]time.Duration(nil), defaultRetryDelays...)
	}
	if len(retryDelays) == 0 {
		return nil, errors.New("report: retry delays must not be empty")
	}
	for _, delay := range retryDelays {
		if delay <= 0 || delay > 24*time.Hour {
			return nil, errors.New("report: retry delay is out of range")
		}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Worker{
		db:           cfg.DB,
		store:        cfg.Store,
		sender:       cfg.Sender,
		loc:          cfg.Location,
		now:          cfg.Now,
		pollInterval: cfg.PollInterval,
		lease:        cfg.Lease,
		maxAttempts:  cfg.MaxAttempts,
		retryDelays:  append([]time.Duration(nil), retryDelays...),
		logger:       cfg.Logger,
	}, nil
}

// Run processes due reports until ctx is canceled. Database and delivery
// failures are recorded and retried according to the persisted report row.
func (w *Worker) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("report: nil context")
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		if _, err := w.RunOnce(ctx, w.now()); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.logger.Error("daily report run failed", "code", "report_failed")
		}
		timer := time.NewTimer(w.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// RunOnce evaluates the current Tehran-local day and, when due, processes at
// most the previous complete local day's report. It returns true when a send
// attempt was claimed.
func (w *Worker) RunOnce(ctx context.Context, now time.Time) (bool, error) {
	if ctx == nil {
		return false, errors.New("report: nil context")
	}
	if now.IsZero() {
		return false, errors.New("report: timestamp is required")
	}
	local := now.In(w.loc)
	reportTime, err := w.store.DailyReportTime(ctx)
	if err != nil {
		return false, fmt.Errorf("report: read report time: %w", err)
	}
	hour, minute, err := parseReportTime(reportTime)
	if err != nil {
		return false, err
	}
	scheduledToday := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, w.loc)
	if local.Before(scheduledToday) {
		// The current day's 00:05 report has not been reached. A missed older
		// date is intentionally not backfilled; on restart after the scheduled
		// time the most recent complete day is reported once.
		return false, nil
	}

	reportDate := local.AddDate(0, 0, -1)
	dateKey := reportDate.Format("2006-01-02")
	record, _, err := w.store.EnsureDailyReport(ctx, dateKey, now.UTC())
	if err != nil {
		return false, err
	}
	record, claimed, err := w.store.ClaimDailyReport(ctx, dateKey, now.UTC(), w.lease)
	if err != nil {
		return false, err
	}
	if !claimed {
		return false, nil
	}

	dayStart := time.Date(reportDate.Year(), reportDate.Month(), reportDate.Day(), 0, 0, 0, 0, w.loc)
	summary, err := BuildSummary(ctx, w.db, w.store, dayStart, now)
	if err != nil {
		return true, w.finishFailure(ctx, record, now, "summary_failed")
	}
	text := FormatTelegram(summary)
	messageID, sendErr := w.sender.Send(ctx, text)
	if sendErr != nil {
		return true, w.finishFailure(ctx, record, now, classifyError(sendErr))
	}
	if err := w.store.CompleteDailyReport(ctx, dateKey, now.UTC(), store.DailyReportCompletion{
		Status:    store.DailyReportSent,
		SentAt:    timePtr(now.UTC()),
		MessageID: int64Ptr(messageID),
	}); err != nil {
		return true, err
	}
	w.logger.Info("daily report sent", "report_date", dateKey, "attempt", record.Attempts, "message_id", messageID)
	return true, nil
}

func (w *Worker) finishFailure(ctx context.Context, record store.DailyReportRecord, now time.Time, kind string) error {
	safeKind := safeErrorKind(kind)
	if isPermanent(safeKind) || isSkipped(safeKind) || record.Attempts >= w.maxAttempts {
		status := store.DailyReportFailed
		if isSkipped(safeKind) {
			status = store.DailyReportSkipped
		}
		if err := w.store.CompleteDailyReport(ctx, record.ReportDate, now.UTC(), store.DailyReportCompletion{
			Status:        status,
			LastErrorKind: safeKind,
		}); err != nil {
			return err
		}
		w.logger.Warn("daily report not sent", "report_date", record.ReportDate, "status", status, "error_kind", safeKind, "attempt", record.Attempts)
		return nil
	}
	delay := w.retryDelay(record.Attempts)
	next := now.UTC().Add(delay)
	if err := w.store.CompleteDailyReport(ctx, record.ReportDate, now.UTC(), store.DailyReportCompletion{
		Status:        store.DailyReportPending,
		LastErrorKind: safeKind,
		NextAttemptAt: &next,
	}); err != nil {
		return err
	}
	w.logger.Warn("daily report retry scheduled", "report_date", record.ReportDate, "error_kind", safeKind, "attempt", record.Attempts, "retry_at", next.Format(time.RFC3339))
	return nil
}

func (w *Worker) retryDelay(attempt int) time.Duration {
	index := attempt - 1
	if index < 0 {
		index = 0
	}
	if index >= len(w.retryDelays) {
		index = len(w.retryDelays) - 1
	}
	return w.retryDelays[index]
}

func parseReportTime(value string) (int, int, error) {
	if err := store.ValidateDailyReportTime(value); err != nil {
		return 0, 0, fmt.Errorf("report: %w", err)
	}
	parsed, _ := time.Parse("15:04", value)
	return parsed.Hour(), parsed.Minute(), nil
}

func classifyError(err error) string {
	switch {
	case errors.Is(err, telegramsettings.ErrTelegramNotConfigured):
		return "telegram_not_configured"
	case errors.Is(err, telegramsettings.ErrTelegramSettingsUnreadable):
		return "telegram_unreadable"
	case errors.Is(err, ErrTelegramUnavailable):
		return "telegram_unavailable"
	}
	var telegramErr *notify.TelegramError
	if errors.As(err, &telegramErr) && telegramErr != nil {
		switch telegramErr.Kind {
		case notify.TelegramErrorRateLimited:
			return "telegram_rate_limited"
		case notify.TelegramErrorTransient:
			return "telegram_transient"
		case notify.TelegramErrorCanceled:
			return "telegram_canceled"
		default:
			return "telegram_permanent"
		}
	}
	return "delivery_failed"
}

func isPermanent(kind string) bool {
	return kind == "telegram_permanent"
}

func isSkipped(kind string) bool {
	switch kind {
	case "telegram_not_configured", "telegram_unreadable", "telegram_unavailable":
		return true
	default:
		return false
	}
}

func safeErrorKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "unknown"
	}
	return kind
}

func timePtr(value time.Time) *time.Time { return &value }

func int64Ptr(value int64) *int64 { return &value }

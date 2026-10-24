// Package delivery owns the notification delivery worker. It claims committed
// outbox rows with a lease, sends outside every database transaction, records
// attempts, applies bounded retry policy, and supports crash reclaim. Event
// creation and outbox enqueue remain owned by NTF-000/SCH-005/SCH-006.
package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/am-fallah/neda/internal/notify"
)

const (
	DefaultConcurrency   = 2
	DefaultLease         = time.Minute
	DefaultPollInterval  = 2 * time.Second
	DefaultSendTimeout   = 30 * time.Second
	DefaultMaxAttempts   = 5
	DefaultMaxRetryAfter = 2 * time.Hour
)

var defaultRetryDelays = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
}

// Outcome is a sender's classified result.
type Outcome string

const (
	OutcomeSent    Outcome = "sent"
	OutcomeRetry   Outcome = "retry"
	OutcomeFailed  Outcome = "failed"
	OutcomeSkipped Outcome = "skipped"
)

// Result is one channel send result. ErrorKind and HTTPStatus are safe
// diagnostic categories; they must never contain payload or credentials.
type Result struct {
	Outcome    Outcome
	ErrorKind  string
	HTTPStatus int
	RetryAfter time.Duration
}

// Sent reports a successful delivery.
func Sent() Result { return Result{Outcome: OutcomeSent} }

// Retry reports a transient failure.
func Retry(errorKind string, retryAfter time.Duration) Result {
	return Result{Outcome: OutcomeRetry, ErrorKind: errorKind, RetryAfter: retryAfter}
}

// Failed reports a permanent failure.
func Failed(errorKind string) Result {
	return Result{Outcome: OutcomeFailed, ErrorKind: errorKind}
}

// Skipped reports a non-retryable skip, such as an unconfigured channel.
func Skipped(errorKind string) Result {
	return Result{Outcome: OutcomeSkipped, ErrorKind: errorKind}
}

// Sender delivers one record. It must not access the database and must not
// retry internally; the worker owns retries and attempt accounting.
type Sender interface {
	Send(ctx context.Context, record Record) Result
}

// Record is the event/delivery snapshot passed to a channel sender.
type Record struct {
	ID        string
	EventID   string
	Channel   notify.Channel
	TargetKey string
	AttemptNo int
	Event     notify.Event
	// MonitorExpired is true when the event belongs to a monitor that is
	// already expired; availability deliveries are skipped in that case.
	MonitorExpired bool
}

// Config controls the worker. DB and at least one sender are required.
type Config struct {
	DB            *sql.DB
	Senders       map[notify.Channel]Sender
	Now           func() time.Time
	Lease         time.Duration
	Concurrency   int
	PollInterval  time.Duration
	SendTimeout   time.Duration
	MaxAttempts   int
	RetryDelays   []time.Duration
	MaxRetryAfter time.Duration
	Jitter        func(time.Duration) time.Duration
	Wake          <-chan struct{}
	Logger        *slog.Logger
}

// Worker is a single-process claim/lease/send/retry loop.
type Worker struct {
	db            *sql.DB
	senders       map[notify.Channel]Sender
	now           func() time.Time
	lease         time.Duration
	concurrency   int
	pollInterval  time.Duration
	sendTimeout   time.Duration
	maxAttempts   int
	retryDelays   []time.Duration
	maxRetryAfter time.Duration
	jitter        func(time.Duration) time.Duration
	wake          <-chan struct{}
	logger        *slog.Logger
}

// New validates configuration and applies safe defaults.
func New(cfg Config) (*Worker, error) {
	if cfg.DB == nil {
		return nil, errors.New("delivery: nil database")
	}
	if len(cfg.Senders) == 0 {
		return nil, errors.New("delivery: at least one sender is required")
	}
	for channel, sender := range cfg.Senders {
		if channel != notify.ChannelTelegram && channel != notify.ChannelPush {
			return nil, fmt.Errorf("delivery: invalid sender channel %q", channel)
		}
		if sender == nil {
			return nil, fmt.Errorf("delivery: nil sender for %q", channel)
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Lease == 0 {
		cfg.Lease = DefaultLease
	}
	if cfg.Lease < time.Second || cfg.Lease > 30*time.Minute {
		return nil, errors.New("delivery: lease must be between 1s and 30m")
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = DefaultConcurrency
	}
	if cfg.Concurrency < 1 || cfg.Concurrency > 16 {
		return nil, errors.New("delivery: concurrency must be between 1 and 16")
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	if cfg.PollInterval < 100*time.Millisecond || cfg.PollInterval > time.Minute {
		return nil, errors.New("delivery: poll interval must be between 100ms and 1m")
	}
	if cfg.SendTimeout == 0 {
		cfg.SendTimeout = DefaultSendTimeout
	}
	if cfg.SendTimeout < time.Second || cfg.SendTimeout > 2*time.Minute {
		return nil, errors.New("delivery: send timeout must be between 1s and 2m")
	}
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}
	if cfg.MaxAttempts < 1 || cfg.MaxAttempts > 20 {
		return nil, errors.New("delivery: max attempts must be between 1 and 20")
	}
	delays := cfg.RetryDelays
	if delays == nil {
		delays = append([]time.Duration(nil), defaultRetryDelays...)
	}
	if len(delays) == 0 {
		return nil, errors.New("delivery: retry delays must not be empty")
	}
	for _, delay := range delays {
		if delay < 0 || delay > 24*time.Hour {
			return nil, errors.New("delivery: retry delay is out of range")
		}
	}
	if cfg.MaxRetryAfter == 0 {
		cfg.MaxRetryAfter = DefaultMaxRetryAfter
	}
	if cfg.MaxRetryAfter < 0 || cfg.MaxRetryAfter > 24*time.Hour {
		return nil, errors.New("delivery: max retry-after is out of range")
	}
	jitter := cfg.Jitter
	if jitter == nil {
		jitter = defaultJitter
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		db:            cfg.DB,
		senders:       cfg.Senders,
		now:           cfg.Now,
		lease:         cfg.Lease,
		concurrency:   cfg.Concurrency,
		pollInterval:  cfg.PollInterval,
		sendTimeout:   cfg.SendTimeout,
		maxAttempts:   cfg.MaxAttempts,
		retryDelays:   append([]time.Duration(nil), delays...),
		maxRetryAfter: cfg.MaxRetryAfter,
		jitter:        jitter,
		wake:          cfg.Wake,
		logger:        logger,
	}, nil
}

// Run processes due deliveries until ctx is canceled. Database errors are
// logged and retried after the poll interval; they never crash the service.
func (w *Worker) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("delivery: nil context")
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		processed, err := w.ProcessOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.logger.Error("delivery worker poll failed", "code", "poll_failed")
		}
		if processed > 0 && err == nil {
			continue
		}
		if err := w.wait(ctx); err != nil {
			return nil
		}
	}
}

func (w *Worker) wait(ctx context.Context) error {
	timer := time.NewTimer(w.pollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.wake:
		return nil
	case <-timer.C:
		return nil
	}
}

// ProcessOnce claims and processes at most Concurrency due deliveries. It is
// exported for deterministic tests and operational one-shot use.
func (w *Worker) ProcessOnce(ctx context.Context) (int, error) {
	if ctx == nil {
		return 0, errors.New("delivery: nil context")
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	now := w.now().UTC()
	if err := w.markExhausted(ctx, now); err != nil {
		return 0, err
	}
	records, err := w.claim(ctx, now, w.concurrency)
	if err != nil {
		return 0, err
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, w.concurrency)
	for _, record := range records {
		record := record
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			w.sendOne(ctx, record)
		}()
	}
	wg.Wait()
	return len(records), nil
}

type claimedRow struct {
	record   Record
	attempts int
}

func (w *Worker) claim(ctx context.Context, now time.Time, limit int) ([]Record, error) {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("delivery: begin claim: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, claimQuery, formatTime(now), formatTime(now), w.maxAttempts, limit)
	if err != nil {
		return nil, fmt.Errorf("delivery: select due deliveries: %w", err)
	}
	var candidates []claimedRow
	for rows.Next() {
		claim, err := scanClaim(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, claim)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("delivery: iterate due deliveries: %w", err)
	}
	rows.Close()

	claimed := make([]Record, 0, len(candidates))
	for _, candidate := range candidates {
		record := candidate.record
		attemptNo := candidate.attempts + 1
		record.AttemptNo = attemptNo
		if err := expireOldAttempt(ctx, tx, record.ID, now); err != nil {
			return nil, err
		}
		result, err := tx.ExecContext(ctx, claimUpdate,
			formatTime(now.Add(w.lease)),
			formatTime(now),
			formatTime(now),
			record.ID,
			candidate.attempts,
			formatTime(now),
		)
		if err != nil {
			return nil, fmt.Errorf("delivery: claim delivery: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("delivery: claim rows affected: %w", err)
		}
		if affected == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, insertAttempt,
			record.ID, attemptNo, formatTime(now),
		); err != nil {
			return nil, fmt.Errorf("delivery: insert attempt: %w", err)
		}
		claimed = append(claimed, record)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("delivery: commit claim: %w", err)
	}
	return claimed, nil
}

func (w *Worker) markExhausted(ctx context.Context, now time.Time) error {
	if _, err := w.db.ExecContext(ctx, exhaustedUpdate, formatTime(now), w.maxAttempts); err != nil {
		return fmt.Errorf("delivery: mark exhausted: %w", err)
	}
	return nil
}

func expireOldAttempt(ctx context.Context, tx *sql.Tx, deliveryID string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE notification_attempts
		SET status='failed', error_kind='lease_expired', finished_at=?
		WHERE delivery_id=? AND status='sending'`, formatTime(now), deliveryID)
	if err != nil {
		return fmt.Errorf("delivery: expire old attempt: %w", err)
	}
	return nil
}

func (w *Worker) sendOne(ctx context.Context, record Record) {
	started := w.now().UTC()
	if record.MonitorExpired {
		w.finalize(record, failedAttempt(started, w.now().UTC(), "skipped", "monitor_expired", 0), statusSkipped, "monitor_expired", 0, nil)
		return
	}
	sender := w.senders[record.Channel]
	if sender == nil {
		w.finalize(record, failedAttempt(started, w.now().UTC(), "skipped", "channel_unavailable", 0), statusSkipped, "channel_unavailable", 0, nil)
		return
	}

	sendCtx, cancel := context.WithTimeout(ctx, w.sendTimeout)
	result := sender.Send(sendCtx, record)
	cancel()

	finished := w.now().UTC()
	if ctx.Err() != nil {
		w.finalize(record, failedAttempt(started, finished, "failed", "canceled", 0), statusRetry, "canceled", 0, timePtr(finished))
		return
	}

	switch result.Outcome {
	case OutcomeSent:
		w.finalize(record, attemptResult(started, finished, "sent", "", 0), statusSent, "", 0, nil)
	case OutcomeSkipped:
		kind := safeKind(result.ErrorKind, "skipped")
		w.finalize(record, failedAttempt(started, finished, "skipped", kind, 0), statusSkipped, kind, 0, nil)
	case OutcomeRetry:
		kind := safeKind(result.ErrorKind, "transient")
		if record.AttemptNo >= w.maxAttempts {
			w.finalize(record, failedAttempt(started, finished, "failed", kind, result.HTTPStatus), statusFailed, kind, result.HTTPStatus, nil)
			return
		}
		next := finished.Add(w.retryDelay(record.AttemptNo, result.RetryAfter))
		w.finalize(record, failedAttempt(started, finished, "failed", kind, result.HTTPStatus), statusRetry, kind, result.HTTPStatus, &next)
	default:
		kind := safeKind(result.ErrorKind, "permanent")
		w.finalize(record, failedAttempt(started, finished, "failed", kind, result.HTTPStatus), statusFailed, kind, result.HTTPStatus, nil)
	}
}

func (w *Worker) retryDelay(attemptNo int, retryAfter time.Duration) time.Duration {
	index := attemptNo - 1
	if index < 0 {
		index = 0
	}
	if index >= len(w.retryDelays) {
		index = len(w.retryDelays) - 1
	}
	delay := w.retryDelays[index]
	if retryAfter > delay {
		delay = retryAfter
	}
	if delay > w.maxRetryAfter {
		delay = w.maxRetryAfter
	}
	delay = w.jitter(delay)
	if delay < 0 {
		return 0
	}
	return delay
}

func (w *Worker) finalize(record Record, attempt attemptUpdate, status string, errorKind string, httpStatus int, next *time.Time) {
	tx, err := w.db.BeginTx(context.Background(), nil)
	if err != nil {
		w.logger.Error("delivery finalize failed", "delivery_id", record.ID, "code", "begin_failed")
		return
	}
	defer tx.Rollback()
	now := w.now().UTC()
	var nextValue any
	if next != nil {
		nextValue = formatTime(next.UTC())
	}
	var sentAt any
	if status == statusSent {
		sentAt = formatTime(now)
	}
	var errorValue any
	if strings.TrimSpace(errorKind) != "" {
		errorValue = errorKind
	}
	if _, err := tx.Exec(`UPDATE notification_deliveries
		SET status=?, next_attempt_at=?, last_error_kind=?, sent_at=?, lease_until=NULL, updated_at=?
		WHERE id=? AND status='sending'`,
		status, nextValue, errorValue, sentAt, formatTime(now), record.ID); err != nil {
		w.logger.Error("delivery finalize failed", "delivery_id", record.ID, "code", "delivery_update_failed")
		return
	}
	if _, err := tx.Exec(`UPDATE notification_attempts
		SET finished_at=?, status=?, error_kind=?, http_status=?, duration_ms=?
		WHERE delivery_id=? AND attempt_no=?`,
		formatTime(attempt.finishedAt), attempt.status, nullableError(attempt.errorKind),
		nullableHTTP(attempt.httpStatus), attempt.durationMS, record.ID, record.AttemptNo); err != nil {
		w.logger.Error("delivery finalize failed", "delivery_id", record.ID, "code", "attempt_update_failed")
		return
	}
	if err := tx.Commit(); err != nil {
		w.logger.Error("delivery finalize failed", "delivery_id", record.ID, "code", "commit_failed")
		return
	}
	w.logger.Info("delivery processed",
		"delivery_id", record.ID,
		"event_id", record.EventID,
		"channel", string(record.Channel),
		"attempt", record.AttemptNo,
		"status", status,
		"error_kind", errorKind,
		"duration_ms", attempt.durationMS,
	)
}

type attemptUpdate struct {
	startedAt  time.Time
	finishedAt time.Time
	status     string
	errorKind  string
	httpStatus int
	durationMS int64
}

func attemptResult(started, finished time.Time, status, errorKind string, httpStatus int) attemptUpdate {
	return attemptUpdate{
		startedAt: started, finishedAt: finished, status: status,
		errorKind: errorKind, httpStatus: httpStatus,
		durationMS: durationMillis(started, finished),
	}
}

func failedAttempt(started, finished time.Time, status, errorKind string, httpStatus int) attemptUpdate {
	return attemptResult(started, finished, status, errorKind, httpStatus)
}

func durationMillis(started, finished time.Time) int64 {
	delta := finished.Sub(started)
	if delta < 0 {
		return 0
	}
	return delta.Milliseconds()
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseTime(raw string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func nullableError(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableHTTP(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func timePtr(value time.Time) *time.Time { return &value }

func safeKind(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if len(value) > 64 {
		value = value[:64]
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r == ' ' || r == '"' || r == '\\' {
			return fallback
		}
	}
	return value
}

func defaultJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}
	span := int64(delay / 5)
	if span <= 0 {
		return delay
	}
	delta := time.Duration(rand.Int63n(span*2+1)) - time.Duration(span)
	return delay + delta
}

const (
	statusSent    = "sent"
	statusRetry   = "retry_scheduled"
	statusFailed  = "failed"
	statusSkipped = "skipped"

	claimQuery = `SELECT d.id, d.event_id, d.channel, d.target_key, d.attempt_count,
		e.kind, e.monitor_id, e.provider_id, e.episode_id, e.occurred_at,
		e.train_number, e.operator, e.origin_name, e.destination_name,
		e.departure_at, e.arrival_at, e.wagon_name, e.class_name,
		e.price_amount, e.price_currency, e.booking_url, e.error_kind,
		e.failure_count, e.created_at,
		CASE WHEN m.expired_at IS NOT NULL THEN 1 ELSE 0 END
		FROM notification_deliveries d
		JOIN events e ON e.id = d.event_id
		LEFT JOIN monitors m ON m.id = e.monitor_id
		WHERE (
			(d.status IN ('pending','retry_scheduled') AND (d.next_attempt_at IS NULL OR d.next_attempt_at <= ?))
			OR (d.status = 'sending' AND d.lease_until IS NOT NULL AND d.lease_until <= ?)
		)
		AND d.attempt_count < ?
		ORDER BY COALESCE(d.next_attempt_at, d.created_at), d.created_at
		LIMIT ?`

	claimUpdate = `UPDATE notification_deliveries
		SET status='sending', attempt_count=attempt_count+1, lease_until=?,
		    last_attempt_at=?, next_attempt_at=NULL, updated_at=?
		WHERE id=? AND attempt_count=?
		  AND ((status IN ('pending','retry_scheduled'))
		       OR (status='sending' AND lease_until IS NOT NULL AND lease_until <= ?))`

	insertAttempt = `INSERT INTO notification_attempts
		(delivery_id, attempt_no, started_at, status)
		VALUES (?, ?, ?, 'sending')`

	exhaustedUpdate = `UPDATE notification_deliveries
		SET status='failed', last_error_kind=COALESCE(last_error_kind, 'max_attempts'),
		    lease_until=NULL, updated_at=?
		WHERE status IN ('pending','retry_scheduled','sending') AND attempt_count >= ?`
)

func scanClaim(scanner interface{ Scan(...any) error }) (claimedRow, error) {
	var (
		record                                      Record
		attempts                                    int
		channel, kind                               string
		monitorID, train, operator                  sql.NullString
		episodeID                                   sql.NullInt64
		origin, destination, departure, arrival     sql.NullString
		wagon, class, currency, booking, eventError sql.NullString
		occurred, created                           string
		price                                       sql.NullInt64
		failureCount                                sql.NullInt64
		monitorExpired                              int
	)
	err := scanner.Scan(
		&record.ID, &record.EventID, &channel, &record.TargetKey, &attempts,
		&kind, &monitorID, &record.Event.ProviderID, &episodeID, &occurred,
		&train, &operator, &origin, &destination, &departure, &arrival, &wagon, &class,
		&price, &currency, &booking, &eventError, &failureCount, &created, &monitorExpired,
	)
	if err != nil {
		return claimedRow{}, fmt.Errorf("delivery: scan due delivery: %w", err)
	}
	record.Channel = notify.Channel(channel)
	record.Event.Kind = notify.EventKind(kind)
	record.Event.MonitorID = monitorID.String
	if episodeID.Valid {
		value := episodeID.Int64
		record.Event.EpisodeID = &value
	}
	record.Event.OccurredAt = parseTime(occurred)
	record.Event.CreatedAt = parseTime(created)
	record.Event.TrainNumber = train.String
	record.Event.Operator = operator.String
	record.Event.OriginName = origin.String
	record.Event.DestinationName = destination.String
	record.Event.DepartureAt = nullableTime(departure)
	record.Event.ArrivalAt = nullableTime(arrival)
	record.Event.WagonName = wagon.String
	record.Event.ClassName = class.String
	record.Event.PriceCurrency = currency.String
	record.Event.BookingURL = booking.String
	record.Event.ErrorKind = eventError.String
	if price.Valid {
		record.Event.PriceAmount = &price.Int64
	}
	if failureCount.Valid {
		value := int(failureCount.Int64)
		record.Event.FailureCount = &value
	}
	record.MonitorExpired = monitorExpired != 0
	return claimedRow{record: record, attempts: attempts}, nil
}

func nullableTime(value sql.NullString) *time.Time {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	parsed := parseTime(value.String)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}

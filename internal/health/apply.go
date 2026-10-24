// Package health owns provider-health counters, degraded episodes, recovery,
// and health diagnostics. It is transaction-scoped: callers pass an existing
// *sql.Tx and SCH-010 owns commit/rollback and check-run finalization.
package health

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/provider"
)

// DegradedFailureThreshold is the number of consecutive applicable
// infrastructure failures that opens one provider-degraded episode.
const DegradedFailureThreshold = 3

// TargetOutcome is the target-level outcome recorded on an applicable success.
type TargetOutcome string

const (
	OutcomeSuccess TargetOutcome = "success"
	OutcomeEmpty   TargetOutcome = "empty"
)

func (o TargetOutcome) valid() bool {
	return o == OutcomeSuccess || o == OutcomeEmpty
}

// SuccessInput is one applicable successful or valid-empty outcome.
type SuccessInput struct {
	ProviderID provider.ID
	TargetID   int64
	Outcome    TargetOutcome
	Now        time.Time
	Duration   time.Duration
	Deliveries []notify.Delivery
}

// FailureInput is one applicable provider error outcome. Business outcomes,
// discarded results, and caller cancellation/abandonment must not be passed.
type FailureInput struct {
	ProviderID     provider.ID
	TargetID       int64
	Classification provider.Classification
	Now            time.Time
	Duration       time.Duration
	Deliveries     []notify.Delivery
}

// Result reports provider-health changes made by an applier.
type Result struct {
	ProviderFailureCount int
	EpisodeOpened        *int64
	EpisodeClosed        *int64
	EventsCreated        []string
}

type providerHealth struct {
	consecutiveFailures int
}

type healthEpisode struct {
	id                 int64
	failureCountAtOpen int
	lastErrorKind      string
}

const selectProviderHealthQuery = `SELECT consecutive_failures FROM provider_health WHERE provider_id = ?`

const upsertProviderHealthSuccessQuery = `INSERT INTO provider_health
	(provider_id, last_attempt_at, last_success_at, last_failure_at, last_error_kind,
	 last_duration_ms, consecutive_failures, open_episode_id, updated_at)
VALUES (?, ?, ?, NULL, NULL, ?, 0, NULL, ?)
ON CONFLICT(provider_id) DO UPDATE SET
	last_attempt_at = excluded.last_attempt_at,
	last_success_at = excluded.last_success_at,
	last_duration_ms = excluded.last_duration_ms,
	consecutive_failures = 0,
	open_episode_id = NULL,
	updated_at = excluded.updated_at`

const upsertProviderHealthFailureQuery = `INSERT INTO provider_health
	(provider_id, last_attempt_at, last_success_at, last_failure_at, last_error_kind,
	 last_duration_ms, consecutive_failures, open_episode_id, updated_at)
VALUES (?, ?, NULL, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider_id) DO UPDATE SET
	last_attempt_at = excluded.last_attempt_at,
	last_failure_at = excluded.last_failure_at,
	last_error_kind = excluded.last_error_kind,
	last_duration_ms = excluded.last_duration_ms,
	consecutive_failures = excluded.consecutive_failures,
	open_episode_id = excluded.open_episode_id,
	updated_at = excluded.updated_at`

const selectOpenEpisodeQuery = `SELECT id, failure_count_at_open, last_error_kind
FROM provider_health_episodes WHERE provider_id = ? AND closed_at IS NULL`

const insertEpisodeQuery = `INSERT INTO provider_health_episodes
	(provider_id, opened_at, failure_count_at_open, last_error_kind)
VALUES (?, ?, ?, ?)`

const closeEpisodeQuery = `UPDATE provider_health_episodes
	SET closed_at = ? WHERE id = ? AND closed_at IS NULL`

const updateEpisodeErrorQuery = `UPDATE provider_health_episodes
	SET last_error_kind = ? WHERE id = ? AND closed_at IS NULL`

const updateTargetSuccessQuery = `UPDATE monitor_providers
	SET last_attempt_at = ?, last_success_at = ?, last_outcome = ?
	WHERE id = ?`

const updateTargetFailureQuery = `UPDATE monitor_providers
	SET last_attempt_at = ?, last_outcome = 'error', last_error_kind = ?, last_error_at = ?
	WHERE id = ?`

// ApplySuccess applies one applicable success/valid-empty outcome. It resets
// the provider counter, closes an open degraded episode with one recovery
// event/outbox, and updates provider/target diagnostics. It never begins,
// commits, or rolls back the caller's transaction.
func ApplySuccess(ctx context.Context, tx *sql.Tx, input SuccessInput) (Result, error) {
	if tx == nil {
		return Result{}, errors.New("health: nil transaction")
	}
	if err := input.validate(); err != nil {
		return Result{}, err
	}
	input.Now = input.Now.UTC()

	episode, open, err := findOpenEpisode(ctx, tx, input.ProviderID)
	if err != nil {
		return Result{}, err
	}
	var result Result
	if open {
		if err := closeEpisode(ctx, tx, episode.id, input.Now); err != nil {
			return Result{}, err
		}
		event, err := enqueueHealthEvent(ctx, tx, input.ProviderID, input.Deliveries,
			notify.KindProviderRecovered, episode.id, episode.failureCountAtOpen,
			episodeErrorKind(episode), input.Now)
		if err != nil {
			return Result{}, err
		}
		result.EpisodeClosed = &episode.id
		if event.Created {
			result.EventsCreated = append(result.EventsCreated, event.EventID)
		}
	}

	if err := writeProviderHealthSuccess(ctx, tx, input); err != nil {
		return Result{}, err
	}
	if err := updateTargetSuccess(ctx, tx, input); err != nil {
		return Result{}, err
	}
	return result, nil
}

// ApplyFailure applies one applicable provider error. Under the approved A-21
// rule, only infrastructure failures with CountsTowardOutage increment the
// provider counter. Other provider errors update diagnostics only. Caller
// cancellation/abandonment and business outcomes are rejected.
func ApplyFailure(ctx context.Context, tx *sql.Tx, input FailureInput) (Result, error) {
	if tx == nil {
		return Result{}, errors.New("health: nil transaction")
	}
	if err := input.validate(); err != nil {
		return Result{}, err
	}
	input.Now = input.Now.UTC()

	health, found, err := loadProviderHealth(ctx, tx, input.ProviderID)
	if err != nil {
		return Result{}, err
	}
	failures := 0
	if found {
		failures = health.consecutiveFailures
	}
	if failures < 0 {
		return Result{}, errors.New("health: stored consecutive failures must not be negative")
	}

	episode, open, err := findOpenEpisode(ctx, tx, input.ProviderID)
	if err != nil {
		return Result{}, err
	}

	counts := input.Classification.IsInfrastructure() && input.Classification.CountsTowardOutage
	var result Result
	openEpisodeID := any(nil)
	if open {
		openEpisodeID = episode.id
	}

	if counts {
		if failures == math.MaxInt {
			return Result{}, errors.New("health: consecutive failures overflow")
		}
		failures++
		if !open && failures >= DegradedFailureThreshold {
			episodeID, err := openEpisode(ctx, tx, input.ProviderID, failures, input.Classification.Kind, input.Now)
			if err != nil {
				return Result{}, err
			}
			episode.id = episodeID
			episode.failureCountAtOpen = failures
			episode.lastErrorKind = string(input.Classification.Kind)
			open = true
			openEpisodeID = episodeID
			result.EpisodeOpened = &episodeID
			event, err := enqueueHealthEvent(ctx, tx, input.ProviderID, input.Deliveries,
				notify.KindProviderDegraded, episodeID, failures, string(input.Classification.Kind), input.Now)
			if err != nil {
				return Result{}, err
			}
			if event.Created {
				result.EventsCreated = append(result.EventsCreated, event.EventID)
			}
		} else if open {
			if err := updateEpisodeError(ctx, tx, episode.id, input.Classification.Kind); err != nil {
				return Result{}, err
			}
		}
	}

	if err := writeProviderHealthFailure(ctx, tx, input, failures, openEpisodeID); err != nil {
		return Result{}, err
	}
	if err := updateTargetFailure(ctx, tx, input); err != nil {
		return Result{}, err
	}
	result.ProviderFailureCount = failures
	return result, nil
}

func (in SuccessInput) validate() error {
	if in.TargetID <= 0 {
		return errors.New("health: target id must be positive")
	}
	if strings.TrimSpace(string(in.ProviderID)) == "" {
		return errors.New("health: provider id is required")
	}
	if !in.Outcome.valid() {
		return fmt.Errorf("health: invalid target outcome %q", in.Outcome)
	}
	if in.Now.IsZero() {
		return errors.New("health: now is required")
	}
	if in.Duration < 0 {
		return errors.New("health: duration must not be negative")
	}
	for _, delivery := range in.Deliveries {
		if err := delivery.Validate(); err != nil {
			return fmt.Errorf("health: %w", err)
		}
	}
	return nil
}

func (in FailureInput) validate() error {
	if in.TargetID <= 0 {
		return errors.New("health: target id must be positive")
	}
	if strings.TrimSpace(string(in.ProviderID)) == "" {
		return errors.New("health: provider id is required")
	}
	if in.Now.IsZero() {
		return errors.New("health: now is required")
	}
	if in.Duration < 0 {
		return errors.New("health: duration must not be negative")
	}
	switch in.Classification.Category {
	case provider.CategoryInfrastructure, provider.CategoryConfiguration,
		provider.CategoryRoute, provider.CategoryRequest:
	default:
		return fmt.Errorf("health: unsupported failure category %q", in.Classification.Category)
	}
	if !in.Classification.Kind.Valid() {
		return fmt.Errorf("health: invalid provider error kind %q", in.Classification.Kind)
	}
	for _, delivery := range in.Deliveries {
		if err := delivery.Validate(); err != nil {
			return fmt.Errorf("health: %w", err)
		}
	}
	return nil
}

func loadProviderHealth(ctx context.Context, tx *sql.Tx, providerID provider.ID) (providerHealth, bool, error) {
	var health providerHealth
	err := tx.QueryRowContext(ctx, selectProviderHealthQuery, string(providerID)).
		Scan(&health.consecutiveFailures)
	if errors.Is(err, sql.ErrNoRows) {
		return providerHealth{}, false, nil
	}
	if err != nil {
		return providerHealth{}, false, fmt.Errorf("health: load provider health: %w", err)
	}
	return health, true, nil
}

func findOpenEpisode(ctx context.Context, tx *sql.Tx, providerID provider.ID) (healthEpisode, bool, error) {
	var episode healthEpisode
	var lastErrorKind sql.NullString
	err := tx.QueryRowContext(ctx, selectOpenEpisodeQuery, string(providerID)).Scan(
		&episode.id, &episode.failureCountAtOpen, &lastErrorKind,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return healthEpisode{}, false, nil
	}
	if err != nil {
		return healthEpisode{}, false, fmt.Errorf("health: find open episode: %w", err)
	}
	if lastErrorKind.Valid {
		episode.lastErrorKind = lastErrorKind.String
	}
	return episode, true, nil
}

func openEpisode(ctx context.Context, tx *sql.Tx, providerID provider.ID, failureCount int, errorKind provider.ErrorKind, now time.Time) (int64, error) {
	result, err := tx.ExecContext(ctx, insertEpisodeQuery,
		string(providerID), formatTime(now), failureCount, string(errorKind),
	)
	if err != nil {
		return 0, fmt.Errorf("health: open episode: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("health: open episode id: %w", err)
	}
	return id, nil
}

func closeEpisode(ctx context.Context, tx *sql.Tx, episodeID int64, now time.Time) error {
	result, err := tx.ExecContext(ctx, closeEpisodeQuery, formatTime(now), episodeID)
	if err != nil {
		return fmt.Errorf("health: close episode: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("health: close episode rows: %w", err)
	}
	if updated != 1 {
		return errors.New("health: open episode disappeared during close")
	}
	return nil
}

func updateEpisodeError(ctx context.Context, tx *sql.Tx, episodeID int64, errorKind provider.ErrorKind) error {
	result, err := tx.ExecContext(ctx, updateEpisodeErrorQuery, string(errorKind), episodeID)
	if err != nil {
		return fmt.Errorf("health: update episode error: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("health: update episode error rows: %w", err)
	}
	if updated != 1 {
		return errors.New("health: open episode disappeared during error update")
	}
	return nil
}

func writeProviderHealthSuccess(ctx context.Context, tx *sql.Tx, input SuccessInput) error {
	duration := input.Duration.Milliseconds()
	if _, err := tx.ExecContext(ctx, upsertProviderHealthSuccessQuery,
		string(input.ProviderID), formatTime(input.Now), formatTime(input.Now),
		duration, formatTime(input.Now),
	); err != nil {
		return fmt.Errorf("health: update provider success: %w", err)
	}
	return nil
}

func writeProviderHealthFailure(ctx context.Context, tx *sql.Tx, input FailureInput, failures int, openEpisodeID any) error {
	duration := input.Duration.Milliseconds()
	if _, err := tx.ExecContext(ctx, upsertProviderHealthFailureQuery,
		string(input.ProviderID), formatTime(input.Now), formatTime(input.Now),
		string(input.Classification.Kind), duration, failures, openEpisodeID, formatTime(input.Now),
	); err != nil {
		return fmt.Errorf("health: update provider failure: %w", err)
	}
	return nil
}

func updateTargetSuccess(ctx context.Context, tx *sql.Tx, input SuccessInput) error {
	result, err := tx.ExecContext(ctx, updateTargetSuccessQuery,
		formatTime(input.Now), formatTime(input.Now), string(input.Outcome), input.TargetID,
	)
	if err != nil {
		return fmt.Errorf("health: update target success: %w", err)
	}
	return requireTargetUpdate(result)
}

func updateTargetFailure(ctx context.Context, tx *sql.Tx, input FailureInput) error {
	result, err := tx.ExecContext(ctx, updateTargetFailureQuery,
		formatTime(input.Now), string(input.Classification.Kind), formatTime(input.Now), input.TargetID,
	)
	if err != nil {
		return fmt.Errorf("health: update target failure: %w", err)
	}
	return requireTargetUpdate(result)
}

func requireTargetUpdate(result sql.Result) error {
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("health: target rows affected: %w", err)
	}
	if updated != 1 {
		return errors.New("health: target not found during diagnostics update")
	}
	return nil
}

func enqueueHealthEvent(ctx context.Context, tx *sql.Tx, providerID provider.ID, deliveries []notify.Delivery, kind notify.EventKind, episodeID int64, failureCount int, errorKind string, now time.Time) (notify.EnqueueResult, error) {
	failure := failureCount
	event := notify.Event{
		ID:           healthEventID(kind, episodeID),
		Kind:         kind,
		DedupeKey:    healthDedupeKey(kind, providerID, episodeID),
		ProviderID:   string(providerID),
		OccurredAt:   now,
		ErrorKind:    errorKind,
		FailureCount: &failure,
		CreatedAt:    now,
	}
	enqueued, err := notify.Enqueue(ctx, tx, event, deliveries)
	if err != nil {
		return notify.EnqueueResult{}, fmt.Errorf("health: enqueue event: %w", err)
	}
	return enqueued, nil
}

func healthEventID(kind notify.EventKind, episodeID int64) string {
	state := "degraded"
	if kind == notify.KindProviderRecovered {
		state = "recovered"
	}
	return fmt.Sprintf("health-episode-%d-%s", episodeID, state)
}

func healthDedupeKey(kind notify.EventKind, providerID provider.ID, episodeID int64) string {
	state := "degraded"
	if kind == notify.KindProviderRecovered {
		state = "recovered"
	}
	return fmt.Sprintf("health:%s:%d:%s", providerID, episodeID, state)
}

func episodeErrorKind(episode healthEpisode) string {
	if strings.TrimSpace(episode.lastErrorKind) != "" {
		return episode.lastErrorKind
	}
	return "unknown"
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

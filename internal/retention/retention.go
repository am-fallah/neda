// Package retention owns bounded, deterministic cleanup of diagnostic and
// notification history. It never prunes open episodes or events whose
// deliveries are still pending/retrying, and it never touches monitors,
// provider configuration, active sessions, or active subscriptions.
package retention

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	defaultCheckRuns             = 14 * 24 * time.Hour
	defaultNotificationAttempts  = 30 * 24 * time.Hour
	defaultEvents                = 180 * 24 * time.Hour
	defaultHealthEpisodes        = 180 * 24 * time.Hour
	defaultInventoryStates       = 180 * 24 * time.Hour
	defaultRevokedSessions       = 30 * 24 * time.Hour
	defaultDisabledSubscriptions = 30 * 24 * time.Hour
	defaultInterval              = 24 * time.Hour
	defaultInitialDelay          = 30 * time.Second
)

// Config controls retention windows. Zero values receive documented defaults.
type Config struct {
	CheckRuns             time.Duration
	NotificationAttempts  time.Duration
	Events                time.Duration
	HealthEpisodes        time.Duration
	InventoryStates       time.Duration
	RevokedSessions       time.Duration
	DisabledSubscriptions time.Duration
	Interval              time.Duration
	InitialDelay          time.Duration
	Now                   func() time.Time
	Logger                *slog.Logger
}

// Summary reports rows removed by one run.
type Summary struct {
	Attempts             int64
	CheckRuns            int64
	Events               int64
	HealthEpisodes       int64
	AvailabilityEpisodes int64
	InventoryStates      int64
	Sessions             int64
	PushSubscriptions    int64
}

// Worker executes retention on a daily cadence.
type Worker struct {
	db     *sql.DB
	cfg    Config
	logger *slog.Logger
}

// New validates dependencies and applies safe defaults.
func New(db *sql.DB, cfg Config) (*Worker, error) {
	if db == nil {
		return nil, errors.New("retention: nil database")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.CheckRuns == 0 {
		cfg.CheckRuns = defaultCheckRuns
	}
	if cfg.NotificationAttempts == 0 {
		cfg.NotificationAttempts = defaultNotificationAttempts
	}
	if cfg.Events == 0 {
		cfg.Events = defaultEvents
	}
	if cfg.HealthEpisodes == 0 {
		cfg.HealthEpisodes = defaultHealthEpisodes
	}
	if cfg.InventoryStates == 0 {
		cfg.InventoryStates = defaultInventoryStates
	}
	if cfg.RevokedSessions == 0 {
		cfg.RevokedSessions = defaultRevokedSessions
	}
	if cfg.DisabledSubscriptions == 0 {
		cfg.DisabledSubscriptions = defaultDisabledSubscriptions
	}
	if cfg.Interval == 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.InitialDelay == 0 {
		cfg.InitialDelay = defaultInitialDelay
	}
	if cfg.Interval < time.Hour {
		return nil, errors.New("retention: interval must be at least 1h")
	}
	if cfg.InitialDelay < 0 || cfg.InitialDelay > cfg.Interval {
		return nil, errors.New("retention: initial delay must be between 0 and the interval")
	}
	for name, value := range map[string]time.Duration{
		"check runs": cfg.CheckRuns, "notification attempts": cfg.NotificationAttempts,
		"events": cfg.Events, "health episodes": cfg.HealthEpisodes,
		"inventory states": cfg.InventoryStates, "revoked sessions": cfg.RevokedSessions,
		"disabled subscriptions": cfg.DisabledSubscriptions,
	} {
		if value <= 0 {
			return nil, fmt.Errorf("retention: %s window must be positive", name)
		}
	}
	return &Worker{db: db, cfg: cfg, logger: cfg.Logger}, nil
}

// Run executes one cleanup after the initial delay and then daily until the
// context is cancelled. Errors are logged and retried; they do not crash the
// service.
func (w *Worker) Run(ctx context.Context) error {
	if w == nil || w.db == nil {
		return errors.New("retention: not initialized")
	}
	if ctx == nil {
		return errors.New("retention: nil context")
	}
	if w.cfg.InitialDelay > 0 {
		timer := time.NewTimer(w.cfg.InitialDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		summary, err := w.RunOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.logger.Error("retention run failed", "code", "retention_failed")
		} else {
			w.logger.Info("retention completed",
				"attempts", summary.Attempts,
				"check_runs", summary.CheckRuns,
				"events", summary.Events,
				"health_episodes", summary.HealthEpisodes,
				"availability_episodes", summary.AvailabilityEpisodes,
				"inventory_states", summary.InventoryStates,
				"sessions", summary.Sessions,
				"push_subscriptions", summary.PushSubscriptions,
			)
		}
		timer := time.NewTimer(w.cfg.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// RunOnce executes the deterministic cleanup order in one transaction. It
// commits only if the foreign-key check passes; on any error the transaction is
// rolled back and no rows are removed.
func (w *Worker) RunOnce(ctx context.Context) (Summary, error) {
	if w == nil || w.db == nil {
		return Summary{}, errors.New("retention: not initialized")
	}
	if ctx == nil {
		return Summary{}, errors.New("retention: nil context")
	}
	now := w.cfg.Now().UTC()
	if now.IsZero() {
		return Summary{}, errors.New("retention: current timestamp is required")
	}
	checkCutoff := now.Add(-w.cfg.CheckRuns)
	attemptCutoff := now.Add(-w.cfg.NotificationAttempts)
	eventCutoff := now.Add(-w.cfg.Events)
	healthCutoff := now.Add(-w.cfg.HealthEpisodes)
	stateCutoff := now.Add(-w.cfg.InventoryStates)
	revokedCutoff := now.Add(-w.cfg.RevokedSessions)
	disabledCutoff := now.Add(-w.cfg.DisabledSubscriptions)

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, fmt.Errorf("retention: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	summary := Summary{}
	steps := []struct {
		name string
		dest *int64
		run  func() (sql.Result, error)
	}{
		{
			name: "notification attempts",
			dest: &summary.Attempts,
			run: func() (sql.Result, error) {
				return tx.ExecContext(ctx, `DELETE FROM notification_attempts WHERE started_at < ?`, format(attemptCutoff))
			},
		},
		{
			name: "check runs",
			dest: &summary.CheckRuns,
			run: func() (sql.Result, error) {
				return tx.ExecContext(ctx, `
DELETE FROM check_runs
WHERE outcome <> 'running' AND started_at < ?`, format(checkCutoff))
			},
		},
		{
			name: "events",
			dest: &summary.Events,
			run: func() (sql.Result, error) {
				return tx.ExecContext(ctx, `
DELETE FROM events
WHERE created_at < ?
  AND NOT EXISTS (
    SELECT 1 FROM notification_deliveries d
    WHERE d.event_id = events.id
      AND d.status IN ('pending', 'sending', 'retry_scheduled')
  )`, format(eventCutoff))
			},
		},
		{
			name: "reconcile provider health open episode",
			dest: nil,
			run: func() (sql.Result, error) {
				return tx.ExecContext(ctx, `
UPDATE provider_health
SET open_episode_id = NULL
WHERE open_episode_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM provider_health_episodes e
    WHERE e.id = provider_health.open_episode_id AND e.closed_at IS NULL
  )`)
			},
		},
		{
			name: "closed health episodes",
			dest: &summary.HealthEpisodes,
			run: func() (sql.Result, error) {
				return tx.ExecContext(ctx, `
DELETE FROM provider_health_episodes
WHERE closed_at IS NOT NULL
  AND closed_at < ?
  AND NOT EXISTS (
    SELECT 1 FROM provider_health h WHERE h.open_episode_id = provider_health_episodes.id
  )`, format(healthCutoff))
			},
		},
		{
			name: "closed availability episodes",
			dest: &summary.AvailabilityEpisodes,
			run: func() (sql.Result, error) {
				return tx.ExecContext(ctx, `
DELETE FROM availability_episodes
WHERE closed_at IS NOT NULL
  AND closed_at < ?
  AND NOT EXISTS (
    SELECT 1 FROM events e WHERE e.episode_id = availability_episodes.id
  )`, format(eventCutoff))
			},
		},
		{
			name: "inventory states",
			dest: &summary.InventoryStates,
			run: func() (sql.Result, error) {
				return tx.ExecContext(ctx, `
DELETE FROM inventory_states
WHERE updated_at < ?
  AND NOT EXISTS (
    SELECT 1 FROM availability_episodes e WHERE e.state_id = inventory_states.id
  )`, format(stateCutoff))
			},
		},
		{
			name: "sessions",
			dest: &summary.Sessions,
			run: func() (sql.Result, error) {
				return tx.ExecContext(ctx, `
DELETE FROM sessions
WHERE expires_at < ?
   OR (revoked_at IS NOT NULL AND revoked_at < ?)`, format(now), format(revokedCutoff))
			},
		},
		{
			name: "disabled push subscriptions",
			dest: &summary.PushSubscriptions,
			run: func() (sql.Result, error) {
				return tx.ExecContext(ctx, `
DELETE FROM push_subscriptions
WHERE disabled_at IS NOT NULL AND disabled_at < ?`, format(disabledCutoff))
			},
		},
	}
	for _, step := range steps {
		result, err := step.run()
		if err != nil {
			return Summary{}, fmt.Errorf("retention: delete %s: %w", step.name, err)
		}
		if step.dest != nil {
			affected, err := result.RowsAffected()
			if err != nil {
				return Summary{}, fmt.Errorf("retention: rows affected for %s: %w", step.name, err)
			}
			*step.dest = affected
		}
	}

	if err := foreignKeyCheck(ctx, tx); err != nil {
		return Summary{}, err
	}
	if err := tx.Commit(); err != nil {
		return Summary{}, fmt.Errorf("retention: commit: %w", err)
	}
	committed = true
	if _, err := w.db.ExecContext(ctx, `PRAGMA optimize`); err != nil {
		w.logger.Warn("retention optimize failed", "code", "optimize_failed")
	}
	return summary, nil
}

// foreignKeyCheck verifies the schema before the destructive transaction
// commits. Any violation returns a sanitized error and rolls the run back.
func foreignKeyCheck(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("retention: foreign key check: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table, rowID, parent, fkID any
		if err := rows.Scan(&table, &rowID, &parent, &fkID); err != nil {
			return fmt.Errorf("retention: foreign key check scan: %w", err)
		}
		return errors.New("retention: foreign key violation detected")
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("retention: foreign key check: %w", err)
	}
	return nil
}

func format(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

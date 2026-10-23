package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
)

const monitorSelectQuery = `SELECT id, label, origin_name, destination_name, departure_date,
	adults, children, infants, enabled, expires_at, expired_at, created_at, updated_at
FROM monitors WHERE id = ?`

const monitorListIDsQuery = `SELECT id FROM monitors ORDER BY created_at, id`

const monitorProviderSelectQuery = `SELECT id, monitor_id, provider_id, active, origin_id, destination_id,
	booking_slug, gender_code, config_version, next_due_at, last_attempt_at, last_success_at,
	last_outcome, last_error_kind, last_error_at, consecutive_failures, created_at, updated_at
FROM monitor_providers WHERE monitor_id = ? ORDER BY id`

const insertMonitorQuery = `INSERT INTO monitors
	(id, label, origin_name, destination_name, departure_date, adults, children, infants,
	 enabled, expires_at, expired_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const updateMonitorQuery = `UPDATE monitors SET
	label = ?, origin_name = ?, destination_name = ?, departure_date = ?, adults = ?, children = ?, infants = ?,
	enabled = ?, expires_at = ?, expired_at = ?, updated_at = ?
WHERE id = ?`

const insertMonitorProviderQuery = `INSERT INTO monitor_providers
	(monitor_id, provider_id, active, origin_id, destination_id, booking_slug, gender_code,
	 config_version, next_due_at, last_attempt_at, last_success_at, last_outcome,
	 last_error_kind, last_error_at, consecutive_failures, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const updateMonitorProviderQuery = `UPDATE monitor_providers SET
	provider_id = ?, active = ?, origin_id = ?, destination_id = ?, booking_slug = ?, gender_code = ?,
	config_version = ?, next_due_at = ?, last_attempt_at = ?, last_success_at = ?, last_outcome = ?,
	last_error_kind = ?, last_error_at = ?, consecutive_failures = ?, updated_at = ?
WHERE id = ? AND monitor_id = ?`

const closeConfigEpisodesQuery = `UPDATE availability_episodes
SET closed_at = ?, close_reason = 'config_changed'
WHERE closed_at IS NULL AND state_id IN (
	SELECT id FROM inventory_states WHERE monitor_provider_id = ? AND config_version = ?
)`

const scheduleActiveTargetsQuery = `UPDATE monitor_providers
SET next_due_at = ?, updated_at = ?
WHERE monitor_id = ? AND active = 1
  AND EXISTS (
	SELECT 1 FROM monitors
	WHERE id = ? AND enabled = 1 AND expires_at > ? AND expired_at IS NULL
  )`

var _ domain.MonitorRepository = (*Store)(nil)

// CreateMonitor inserts a monitor and its active provider targets.
func (s *Store) CreateMonitor(ctx context.Context, monitor domain.Monitor) (domain.Monitor, error) {
	if strings.TrimSpace(monitor.ID) == "" {
		return domain.Monitor{}, errors.New("store: monitor id must not be empty")
	}
	err := s.WithTx(ctx, func(tx *Store) error {
		if _, err := tx.db.ExecContext(ctx, insertMonitorQuery,
			monitor.ID, monitor.Label, monitor.OriginName, monitor.DestinationName, monitor.DepartureDate,
			monitor.Adults, monitor.Children, monitor.Infants, boolToInt(monitor.Enabled),
			formatTime(monitor.ExpiresAt), formatOptionalTime(monitor.ExpiredAt),
			formatTime(monitor.CreatedAt), formatTime(monitor.UpdatedAt),
		); err != nil {
			return wrap("create monitor", err)
		}
		for _, target := range monitor.Providers {
			if err := insertMonitorProvider(ctx, tx.db, monitor.ID, target); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return domain.Monitor{}, err
	}
	return s.GetMonitor(ctx, monitor.ID)
}

// GetMonitor returns a monitor and all its provider rows, including inactive
// historical targets.
func (s *Store) GetMonitor(ctx context.Context, id string) (domain.Monitor, error) {
	db, err := s.executor()
	if err != nil {
		return domain.Monitor{}, err
	}
	return loadMonitor(ctx, db, id)
}

// ListMonitors returns all monitors with provider rows.
func (s *Store) ListMonitors(ctx context.Context) ([]domain.Monitor, error) {
	db, err := s.executor()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, monitorListIDsQuery)
	if err != nil {
		return nil, wrap("list monitors", err)
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, wrap("scan monitor id", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, wrap("iterate monitors", err)
	}
	if err := rows.Close(); err != nil {
		return nil, wrap("close monitor list", err)
	}

	monitors := make([]domain.Monitor, 0, len(ids))
	for _, id := range ids {
		monitor, err := loadMonitor(ctx, db, id)
		if err != nil {
			return nil, err
		}
		monitors = append(monitors, monitor)
	}
	return monitors, nil
}

// UpdateMonitor applies a validated edit and the config-version/episode rules
// in one transaction. Near-term scheduling is applied after that transaction
// commits, then the caller may wake the scheduler.
func (s *Store) UpdateMonitor(ctx context.Context, id string, input domain.MonitorInput, now, scheduleAt time.Time, loc *time.Location) (domain.MonitorUpdateResult, error) {
	var plan domain.UpdatePlan
	err := s.WithTx(ctx, func(tx *Store) error {
		current, err := loadMonitor(ctx, tx.db, id)
		if err != nil {
			return err
		}
		plan, err = domain.PlanUpdate(current, input, now, loc)
		if err != nil {
			return err
		}
		if err := updateMonitor(ctx, tx.db, plan.Monitor); err != nil {
			return err
		}
		for _, target := range plan.Monitor.Providers {
			if target.ID == 0 {
				if err := insertMonitorProvider(ctx, tx.db, id, target); err != nil {
					return err
				}
				continue
			}
			if err := updateMonitorProvider(ctx, tx.db, id, target); err != nil {
				return err
			}
		}
		for _, close := range plan.CloseEpisodes {
			if _, err := tx.db.ExecContext(ctx, closeConfigEpisodesQuery, formatTime(now), close.TargetID, close.ConfigVersion); err != nil {
				return wrap("close config episodes", err)
			}
		}
		return nil
	})
	if err != nil {
		return domain.MonitorUpdateResult{}, err
	}
	if plan.Material {
		if scheduleAt.IsZero() {
			scheduleAt = now
		}
		db, err := s.executor()
		if err != nil {
			return domain.MonitorUpdateResult{}, err
		}
		if _, err := db.ExecContext(ctx, scheduleActiveTargetsQuery,
			formatTime(scheduleAt), formatTime(now), id, id, formatTime(now),
		); err != nil {
			return domain.MonitorUpdateResult{}, wrap("schedule edited targets", err)
		}
	}
	updated, err := s.GetMonitor(ctx, id)
	if err != nil {
		return domain.MonitorUpdateResult{}, err
	}
	return domain.MonitorUpdateResult{Monitor: updated, Material: plan.Material}, nil
}

// PauseMonitor disables polling and clears target due times.
func (s *Store) PauseMonitor(ctx context.Context, id string, now time.Time) (domain.Monitor, error) {
	err := s.WithTx(ctx, func(tx *Store) error {
		result, err := tx.db.ExecContext(ctx,
			`UPDATE monitors SET enabled = 0, updated_at = ? WHERE id = ?`,
			formatTime(now), id,
		)
		if err != nil {
			return wrap("pause monitor", err)
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: pause monitor rows: %w", err)
		}
		if updated == 0 {
			return domain.ErrMonitorNotFound
		}
		if _, err := tx.db.ExecContext(ctx,
			`UPDATE monitor_providers SET next_due_at = NULL, updated_at = ? WHERE monitor_id = ?`,
			formatTime(now), id,
		); err != nil {
			return wrap("clear paused target due times", err)
		}
		return nil
	})
	if err != nil {
		return domain.Monitor{}, err
	}
	return s.GetMonitor(ctx, id)
}

// ResumeMonitor enables a non-expired monitor and schedules active targets.
// Resuming an already-enabled monitor is a no-op.
func (s *Store) ResumeMonitor(ctx context.Context, id string, now, scheduleAt time.Time) (domain.Monitor, error) {
	err := s.WithTx(ctx, func(tx *Store) error {
		current, err := loadMonitor(ctx, tx.db, id)
		if err != nil {
			return err
		}
		if current.IsExpired(now) {
			return domain.ErrMonitorExpired
		}
		if current.Enabled {
			return nil
		}
		if scheduleAt.IsZero() {
			scheduleAt = now
		}
		if _, err := tx.db.ExecContext(ctx,
			`UPDATE monitors SET enabled = 1, updated_at = ? WHERE id = ?`,
			formatTime(now), id,
		); err != nil {
			return wrap("resume monitor", err)
		}
		if _, err := tx.db.ExecContext(ctx,
			`UPDATE monitor_providers SET next_due_at = ?, updated_at = ? WHERE monitor_id = ? AND active = 1`,
			formatTime(scheduleAt), formatTime(now), id,
		); err != nil {
			return wrap("schedule resumed targets", err)
		}
		return nil
	})
	if err != nil {
		return domain.Monitor{}, err
	}
	return s.GetMonitor(ctx, id)
}

// DeleteMonitor hard-deletes a monitor; provider and history rows cascade.
func (s *Store) DeleteMonitor(ctx context.Context, id string) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, `DELETE FROM monitors WHERE id = ?`, id)
	if err != nil {
		return wrap("delete monitor", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete monitor rows: %w", err)
	}
	if deleted == 0 {
		return domain.ErrMonitorNotFound
	}
	return nil
}

func loadMonitor(ctx context.Context, db dbtx, id string) (domain.Monitor, error) {
	var monitor domain.Monitor
	var enabled int
	var expiresAt, createdAt, updatedAt string
	var expiredAt sql.NullString
	err := db.QueryRowContext(ctx, monitorSelectQuery, id).Scan(
		&monitor.ID, &monitor.Label, &monitor.OriginName, &monitor.DestinationName, &monitor.DepartureDate,
		&monitor.Adults, &monitor.Children, &monitor.Infants, &enabled,
		&expiresAt, &expiredAt, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Monitor{}, domain.ErrMonitorNotFound
		}
		return domain.Monitor{}, wrap("get monitor", err)
	}
	if enabled != 0 && enabled != 1 {
		return domain.Monitor{}, fmt.Errorf("store: monitor %q has invalid enabled value", id)
	}
	monitor.Enabled = enabled == 1
	if monitor.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return domain.Monitor{}, err
	}
	if monitor.ExpiredAt, err = parseOptionalTime(expiredAt); err != nil {
		return domain.Monitor{}, err
	}
	if monitor.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.Monitor{}, err
	}
	if monitor.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return domain.Monitor{}, err
	}
	providers, err := loadMonitorProviders(ctx, db, id)
	if err != nil {
		return domain.Monitor{}, err
	}
	monitor.Providers = providers
	return monitor, nil
}

func loadMonitorProviders(ctx context.Context, db dbtx, monitorID string) ([]domain.MonitorProvider, error) {
	rows, err := db.QueryContext(ctx, monitorProviderSelectQuery, monitorID)
	if err != nil {
		return nil, wrap("list monitor providers", err)
	}
	defer rows.Close()

	providers := make([]domain.MonitorProvider, 0)
	for rows.Next() {
		var target domain.MonitorProvider
		var providerID string
		var active int
		var bookingSlug, genderCode sql.NullString
		var nextDueAt, lastAttemptAt, lastSuccessAt, lastErrorAt sql.NullString
		var lastOutcome, lastErrorKind sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(
			&target.ID, &target.MonitorID, &providerID, &active,
			&target.OriginID, &target.DestinationID, &bookingSlug, &genderCode,
			&target.ConfigVersion, &nextDueAt, &lastAttemptAt, &lastSuccessAt,
			&lastOutcome, &lastErrorKind, &lastErrorAt, &target.ConsecutiveFailures,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, wrap("scan monitor provider", err)
		}
		if active != 0 && active != 1 {
			return nil, fmt.Errorf("store: monitor provider %d has invalid active value", target.ID)
		}
		target.ProviderID = provider.ID(providerID)
		target.Active = active == 1
		if bookingSlug.Valid {
			target.BookingSlug = bookingSlug.String
		}
		if genderCode.Valid {
			target.GenderCode = genderCode.String
		}
		if lastOutcome.Valid {
			target.LastOutcome = lastOutcome.String
		}
		if lastErrorKind.Valid {
			target.LastErrorKind = lastErrorKind.String
		}
		if target.NextDueAt, err = parseOptionalTime(nextDueAt); err != nil {
			return nil, err
		}
		if target.LastAttemptAt, err = parseOptionalTime(lastAttemptAt); err != nil {
			return nil, err
		}
		if target.LastSuccessAt, err = parseOptionalTime(lastSuccessAt); err != nil {
			return nil, err
		}
		if target.LastErrorAt, err = parseOptionalTime(lastErrorAt); err != nil {
			return nil, err
		}
		if target.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if target.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return nil, err
		}
		providers = append(providers, target)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate monitor providers", err)
	}
	return providers, nil
}

func insertMonitorProvider(ctx context.Context, db dbtx, monitorID string, target domain.MonitorProvider) error {
	if _, err := db.ExecContext(ctx, insertMonitorProviderQuery,
		monitorID, string(target.ProviderID), boolToInt(target.Active),
		target.OriginID, target.DestinationID,
		nullableString(target.BookingSlug), nullableString(target.GenderCode),
		target.ConfigVersion, formatOptionalTime(target.NextDueAt),
		formatOptionalTime(target.LastAttemptAt), formatOptionalTime(target.LastSuccessAt),
		nullableString(target.LastOutcome), nullableString(target.LastErrorKind),
		formatOptionalTime(target.LastErrorAt), target.ConsecutiveFailures,
		formatTime(target.CreatedAt), formatTime(target.UpdatedAt),
	); err != nil {
		return wrap("create monitor provider", err)
	}
	return nil
}

func updateMonitor(ctx context.Context, db dbtx, monitor domain.Monitor) error {
	result, err := db.ExecContext(ctx, updateMonitorQuery,
		monitor.Label, monitor.OriginName, monitor.DestinationName, monitor.DepartureDate,
		monitor.Adults, monitor.Children, monitor.Infants, boolToInt(monitor.Enabled),
		formatTime(monitor.ExpiresAt), formatOptionalTime(monitor.ExpiredAt),
		formatTime(monitor.UpdatedAt), monitor.ID,
	)
	if err != nil {
		return wrap("update monitor", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update monitor rows: %w", err)
	}
	if updated == 0 {
		return domain.ErrMonitorNotFound
	}
	return nil
}

func updateMonitorProvider(ctx context.Context, db dbtx, monitorID string, target domain.MonitorProvider) error {
	result, err := db.ExecContext(ctx, updateMonitorProviderQuery,
		string(target.ProviderID), boolToInt(target.Active), target.OriginID, target.DestinationID,
		nullableString(target.BookingSlug), nullableString(target.GenderCode),
		target.ConfigVersion, formatOptionalTime(target.NextDueAt),
		formatOptionalTime(target.LastAttemptAt), formatOptionalTime(target.LastSuccessAt),
		nullableString(target.LastOutcome), nullableString(target.LastErrorKind),
		formatOptionalTime(target.LastErrorAt), target.ConsecutiveFailures,
		formatTime(target.UpdatedAt), target.ID, monitorID,
	)
	if err != nil {
		return wrap("update monitor provider", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update monitor provider rows: %w", err)
	}
	if updated == 0 {
		return ErrConflict
	}
	return nil
}

func parseOptionalTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func formatOptionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

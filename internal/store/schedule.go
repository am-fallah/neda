package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
)

const scheduleTargetsQuery = `SELECT mp.id, mp.monitor_id, mp.provider_id, mp.config_version,
	mp.next_due_at, m.expires_at
FROM monitor_providers mp
JOIN monitors m ON m.id = mp.monitor_id
WHERE mp.active = 1
  AND m.enabled = 1
  AND m.expired_at IS NULL
ORDER BY (mp.next_due_at IS NULL), mp.next_due_at, mp.id`

const recoverRunningChecksQuery = `UPDATE check_runs
SET outcome = 'abandoned', finished_at = ?
WHERE outcome = 'running'`

const staggerTargetQuery = `UPDATE monitor_providers
SET next_due_at = ?, updated_at = ?
WHERE id = ? AND active = 1`

// ListScheduleTargets returns every active target of an enabled, non-expired
// monitor. Nullable next_due_at values are retained so startup staggering can
// distinguish unscheduled targets from targets already due.
func (s *Store) ListScheduleTargets(ctx context.Context, now time.Time) ([]domain.ScheduleTarget, error) {
	db, err := s.executor()
	if err != nil {
		return nil, err
	}
	return listScheduleTargets(ctx, db, now)
}

// RecoverRunningChecks marks interrupted attempts abandoned without touching
// target scheduling, inventory, episodes, provider health, events, or outbox
// state. It is a single recovery transaction.
func (s *Store) RecoverRunningChecks(ctx context.Context, now time.Time) (int64, error) {
	var recovered int64
	err := s.WithTx(ctx, func(tx *Store) error {
		result, err := tx.db.ExecContext(ctx, recoverRunningChecksQuery, formatTime(now))
		if err != nil {
			return wrap("recover running checks", err)
		}
		recovered, err = result.RowsAffected()
		if err != nil {
			return fmt.Errorf("store: recover running check rows: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return recovered, nil
}

// StaggerDueTargets reschedules active targets whose next_due_at is null or
// already due, using planner for each target. Targets with a future
// next_due_at are preserved. The whole update runs in one transaction.
func (s *Store) StaggerDueTargets(ctx context.Context, now time.Time, planner func(domain.ScheduleTarget) time.Time) (int64, error) {
	if planner == nil {
		return 0, errors.New("store: nil schedule planner")
	}
	var staggered int64
	err := s.WithTx(ctx, func(tx *Store) error {
		targets, err := listScheduleTargets(ctx, tx.db, now)
		if err != nil {
			return err
		}
		for _, target := range targets {
			if target.NextDueAt != nil && target.NextDueAt.After(now) {
				continue
			}
			due := planner(target).UTC()
			if due.Before(now) {
				due = now.UTC()
			}
			result, err := tx.db.ExecContext(ctx, staggerTargetQuery,
				formatTime(due), formatTime(now), target.ID,
			)
			if err != nil {
				return wrap("stagger target", err)
			}
			updated, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("store: stagger target rows: %w", err)
			}
			if updated == 0 {
				// The target was deactivated between selection and update. It
				// must not be polled, so leaving it unscheduled is correct.
				continue
			}
			staggered += updated
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return staggered, nil
}

func listScheduleTargets(ctx context.Context, db dbtx, now time.Time) ([]domain.ScheduleTarget, error) {
	rows, err := db.QueryContext(ctx, scheduleTargetsQuery)
	if err != nil {
		return nil, wrap("list schedule targets", err)
	}
	defer rows.Close()

	targets := make([]domain.ScheduleTarget, 0)
	for rows.Next() {
		var target domain.ScheduleTarget
		var providerID string
		var nextDueAt sql.NullString
		var expiresAt string
		if err := rows.Scan(&target.ID, &target.MonitorID, &providerID, &target.ConfigVersion, &nextDueAt, &expiresAt); err != nil {
			return nil, wrap("scan schedule target", err)
		}
		expiry, err := parseTime(expiresAt)
		if err != nil {
			return nil, err
		}
		// Compare parsed times rather than SQLite text so sub-second values
		// cannot be misordered by variable-length RFC3339Nano fractions.
		if !expiry.After(now) {
			continue
		}
		target.ProviderID = provider.ID(providerID)
		if target.NextDueAt, err = parseOptionalTime(nextDueAt); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate schedule targets", err)
	}
	return targets, nil
}

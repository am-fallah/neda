package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const applyTargetScheduleQuery = `UPDATE monitor_providers
SET consecutive_failures = ?, next_due_at = ?, updated_at = ?
WHERE id = ?`

// ApplyTargetSchedule persists only the target scheduling fields owned by the
// backoff policy. It is transaction-scoped: callers invoke it on a *Store that
// belongs to an existing transaction, normally the SCH-010 outcome
// transaction. It never begins or commits a transaction and does not write
// target diagnostics, availability, health, events, or outbox rows.
func (s *Store) ApplyTargetSchedule(ctx context.Context, targetID int64, failures int, nextDue, now time.Time) error {
	if targetID <= 0 {
		return errors.New("store: target id must be positive")
	}
	if failures < 0 {
		return errors.New("store: consecutive failures must not be negative")
	}
	if nextDue.IsZero() {
		return errors.New("store: next due time must be set")
	}
	if now.IsZero() {
		return errors.New("store: schedule update time must be set")
	}
	db, err := s.executor()
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, applyTargetScheduleQuery,
		failures, formatTime(nextDue), formatTime(now), targetID,
	)
	if err != nil {
		return wrap("apply target schedule", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: apply target schedule rows: %w", err)
	}
	if updated == 0 {
		return ErrConflict
	}
	return nil
}

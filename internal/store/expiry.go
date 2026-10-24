package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/am-fallah/neda/internal/availability"
)

const unexpiredMonitorsQuery = `SELECT id, expires_at FROM monitors WHERE expired_at IS NULL`

const markExpiredMonitorQuery = `UPDATE monitors
SET expired_at = ?, updated_at = ?
WHERE id = ? AND expired_at IS NULL`

// ReconcileExpirations applies the SCH-007 expiry sweep in one transaction. It
// marks every monitor whose parsed expires_at is at or before now, delegates
// open-episode closure to SCH-005, and returns the earliest remaining future
// expiry. Paused and disabled monitors are included. It never polls, creates
// events, or writes target scheduling, check-run, or provider-health state.
func (s *Store) ReconcileExpirations(ctx context.Context, now time.Time) (int64, *time.Time, error) {
	if now.IsZero() {
		return 0, nil, errors.New("store: expiry observation time is required")
	}
	now = now.UTC()
	var expired int64
	var nextExpiry *time.Time
	err := s.WithSQLTx(ctx, func(tx *sql.Tx, _ *Store) error {
		rows, err := tx.QueryContext(ctx, unexpiredMonitorsQuery)
		if err != nil {
			return wrap("list unexpired monitors", err)
		}
		due := make([]string, 0)
		var earliestFuture time.Time
		hasFuture := false
		for rows.Next() {
			var id string
			var rawExpiry string
			if err := rows.Scan(&id, &rawExpiry); err != nil {
				_ = rows.Close()
				return wrap("scan unexpired monitor", err)
			}
			expiry, err := parseTime(rawExpiry)
			if err != nil {
				_ = rows.Close()
				return fmt.Errorf("store: parse monitor %s expiry: %w", id, err)
			}
			if !expiry.After(now) {
				due = append(due, id)
				continue
			}
			if !hasFuture || expiry.Before(earliestFuture) {
				earliestFuture = expiry
				hasFuture = true
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return wrap("iterate unexpired monitors", err)
		}
		if err := rows.Close(); err != nil {
			return wrap("close unexpired monitor rows", err)
		}

		for _, monitorID := range due {
			result, err := tx.ExecContext(ctx, markExpiredMonitorQuery,
				formatTime(now), formatTime(now), monitorID,
			)
			if err != nil {
				return wrap("mark monitor expired", err)
			}
			updated, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("store: mark monitor expired rows: %w", err)
			}
			if updated != 1 {
				return fmt.Errorf("store: monitor %s expired concurrently", monitorID)
			}
			if _, err := availability.CloseExpired(ctx, tx, monitorID, now); err != nil {
				return fmt.Errorf("store: close expired episodes for %s: %w", monitorID, err)
			}
			expired++
		}
		if hasFuture {
			value := earliestFuture
			nextExpiry = &value
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	return expired, nextExpiry, nil
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const monitorProviderExistsQuery = `SELECT 1 FROM monitor_providers WHERE id = ?`

const insertRunningCheckQuery = `INSERT INTO check_runs
	(monitor_provider_id, config_version, started_at, outcome, created_at)
VALUES (?, ?, ?, 'running', ?)`

// StartCheckRun performs SCH-004's single attempt-start transaction. It
// verifies the target row still exists, then inserts exactly one running
// check_runs row and commits before any provider I/O.
//
// It writes only check_runs. It never updates target scheduling or diagnostics,
// availability state, episodes, provider health, events, or outbox rows. The
// outcome transaction is owned by SCH-010.
func (s *Store) StartCheckRun(ctx context.Context, targetID, configVersion int64, startedAt time.Time) (int64, error) {
	if targetID <= 0 {
		return 0, errors.New("store: check target id must be positive")
	}
	if configVersion < 1 {
		return 0, errors.New("store: check config version must be positive")
	}
	if startedAt.IsZero() {
		return 0, errors.New("store: check start time must be set")
	}

	var checkRunID int64
	err := s.WithTx(ctx, func(tx *Store) error {
		var exists int
		if err := tx.db.QueryRowContext(ctx, monitorProviderExistsQuery, targetID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return wrap("verify check target", err)
		}
		result, err := tx.db.ExecContext(ctx, insertRunningCheckQuery,
			targetID, configVersion, formatTime(startedAt), formatTime(startedAt),
		)
		if err != nil {
			return wrap("start check run", err)
		}
		checkRunID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("store: start check run id: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return checkRunID, nil
}

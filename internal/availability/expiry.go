package availability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const closeExpiredEpisodesQuery = `UPDATE availability_episodes
SET closed_at = ?, close_reason = 'expired'
WHERE closed_at IS NULL AND state_id IN (
	SELECT s.id FROM inventory_states s
	JOIN monitor_providers mp ON mp.id = s.monitor_provider_id
	WHERE mp.monitor_id = ?
)`

// CloseExpired closes every open availability episode belonging to monitorID
// with close_reason='expired'. It is the SCH-005-owned episode transition used
// by the SCH-007 expiry sweep. It never creates an event or delivery, and never
// begins, commits, or rolls back the caller's transaction.
func CloseExpired(ctx context.Context, tx *sql.Tx, monitorID string, now time.Time) (int64, error) {
	if tx == nil {
		return 0, errors.New("availability: nil transaction")
	}
	if strings.TrimSpace(monitorID) == "" {
		return 0, errors.New("availability: monitor id is required")
	}
	if now.IsZero() {
		return 0, errors.New("availability: expiry observation time is required")
	}
	result, err := tx.ExecContext(ctx, closeExpiredEpisodesQuery, formatTime(now.UTC()), strings.TrimSpace(monitorID))
	if err != nil {
		return 0, fmt.Errorf("availability: close expired episodes: %w", err)
	}
	closed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("availability: close expired episode rows: %w", err)
	}
	return closed, nil
}

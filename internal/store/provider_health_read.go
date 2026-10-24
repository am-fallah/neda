package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Provider health is global per provider. This read model is used by API-003;
// it never mutates counters, episodes, scheduling, availability, or outbox
// state.

// ProviderHealthEpisode is the currently open provider-degraded episode.
type ProviderHealthEpisode struct {
	ID                 int64
	OpenedAt           time.Time
	FailureCountAtOpen int
	LastErrorKind      string
}

// ProviderHealthRecord is one provider_health row with its optional open
// episode. Missing rows mean the provider has no completed applicable outcome
// yet; the API presents that as unknown, not healthy or unhealthy.
type ProviderHealthRecord struct {
	ProviderID string

	LastAttemptAt  *time.Time
	LastSuccessAt  *time.Time
	LastFailureAt  *time.Time
	LastErrorKind  string
	LastDurationMS *int64

	ConsecutiveFailures int
	OpenEpisode         *ProviderHealthEpisode
	UpdatedAt           time.Time
}

const providerHealthReadQuery = `SELECT ph.provider_id,
	ph.last_attempt_at, ph.last_success_at, ph.last_failure_at,
	ph.last_error_kind, ph.last_duration_ms, ph.consecutive_failures,
	ph.open_episode_id, ph.updated_at,
	phe.id, phe.opened_at, phe.failure_count_at_open, phe.last_error_kind
FROM provider_health ph
LEFT JOIN provider_health_episodes phe
	ON phe.id = ph.open_episode_id AND phe.closed_at IS NULL
ORDER BY ph.provider_id`

// ListProviderHealth returns every provider_health row in stable provider-ID
// order. The slice is never nil.
func (s *Store) ListProviderHealth(ctx context.Context) ([]ProviderHealthRecord, error) {
	db, err := s.executor()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, providerHealthReadQuery)
	if err != nil {
		return nil, wrap("list provider health", err)
	}
	defer rows.Close()

	records := make([]ProviderHealthRecord, 0)
	for rows.Next() {
		var (
			record                   ProviderHealthRecord
			providerID               string
			lastAttempt, lastSuccess sql.NullString
			lastFailure              sql.NullString
			lastErrorKind            sql.NullString
			lastDuration             sql.NullInt64
			openEpisodeID            sql.NullInt64
			updatedAt                string
			episodeID                sql.NullInt64
			episodeOpenedAt          sql.NullString
			episodeFailureCount      sql.NullInt64
			episodeLastErrorKind     sql.NullString
		)
		if err := rows.Scan(
			&providerID, &lastAttempt, &lastSuccess, &lastFailure,
			&lastErrorKind, &lastDuration, &record.ConsecutiveFailures,
			&openEpisodeID, &updatedAt, &episodeID, &episodeOpenedAt,
			&episodeFailureCount, &episodeLastErrorKind,
		); err != nil {
			return nil, wrap("scan provider health", err)
		}
		if strings.TrimSpace(providerID) == "" {
			return nil, fmt.Errorf("store: provider health row has an empty provider id")
		}
		if record.ConsecutiveFailures < 0 {
			return nil, fmt.Errorf("store: provider health %q has a negative failure count", providerID)
		}
		record.ProviderID = providerID
		var err error
		if record.LastAttemptAt, err = parseOptionalTime(lastAttempt); err != nil {
			return nil, err
		}
		if record.LastSuccessAt, err = parseOptionalTime(lastSuccess); err != nil {
			return nil, err
		}
		if record.LastFailureAt, err = parseOptionalTime(lastFailure); err != nil {
			return nil, err
		}
		record.LastErrorKind = optionalString(lastErrorKind)
		if lastDuration.Valid {
			if lastDuration.Int64 < 0 {
				return nil, fmt.Errorf("store: provider health %q has a negative duration", providerID)
			}
			value := lastDuration.Int64
			record.LastDurationMS = &value
		}
		if record.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return nil, err
		}
		if openEpisodeID.Valid {
			if !episodeID.Valid || episodeID.Int64 != openEpisodeID.Int64 {
				return nil, fmt.Errorf("store: provider health %q references a missing open episode", providerID)
			}
			if !episodeOpenedAt.Valid || !episodeFailureCount.Valid || episodeFailureCount.Int64 < 1 {
				return nil, fmt.Errorf("store: provider health %q has an invalid open episode", providerID)
			}
			openedAt, err := parseTime(episodeOpenedAt.String)
			if err != nil {
				return nil, err
			}
			record.OpenEpisode = &ProviderHealthEpisode{
				ID:                 episodeID.Int64,
				OpenedAt:           openedAt,
				FailureCountAtOpen: int(episodeFailureCount.Int64),
				LastErrorKind:      optionalString(episodeLastErrorKind),
			}
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate provider health", err)
	}
	if err := rows.Close(); err != nil {
		return nil, wrap("close provider health", err)
	}
	return records, nil
}

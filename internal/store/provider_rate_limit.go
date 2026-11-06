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

const providerRateLimitColumns = `provider_id, scope, consecutive_429s,
	adaptive_interval_ms, cooldown_until, consecutive_successes,
	last_429_at, last_success_at, last_retry_after_ms, updated_at`

const getProviderRateLimitQuery = `SELECT ` + providerRateLimitColumns + `
FROM provider_rate_limits
WHERE provider_id = ? AND scope = ?`

const listProviderRateLimitsQuery = `SELECT ` + providerRateLimitColumns + `
FROM provider_rate_limits
ORDER BY provider_id, scope`

const upsertProviderRateLimitQuery = `INSERT INTO provider_rate_limits (
	provider_id, scope, consecutive_429s, adaptive_interval_ms, cooldown_until,
	consecutive_successes, last_429_at, last_success_at, last_retry_after_ms, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider_id, scope) DO UPDATE SET
	consecutive_429s = excluded.consecutive_429s,
	adaptive_interval_ms = excluded.adaptive_interval_ms,
	cooldown_until = excluded.cooldown_until,
	consecutive_successes = excluded.consecutive_successes,
	last_429_at = excluded.last_429_at,
	last_success_at = excluded.last_success_at,
	last_retry_after_ms = excluded.last_retry_after_ms,
	updated_at = excluded.updated_at`

type providerRateLimitScanner interface {
	Scan(dest ...any) error
}

// GetProviderRateLimit returns one durable rate-limit state row. A missing row
// reports found=false with the zero state; callers must not treat that as an
// error or a provider failure.
func (s *Store) GetProviderRateLimit(ctx context.Context, providerID provider.ID, scope string) (domain.ProviderRateLimit, bool, error) {
	providerID, scope, err := validateProviderRateLimitKey(providerID, scope)
	if err != nil {
		return domain.ProviderRateLimit{}, false, err
	}
	db, err := s.executor()
	if err != nil {
		return domain.ProviderRateLimit{}, false, err
	}
	state, err := scanProviderRateLimit(db.QueryRowContext(ctx, getProviderRateLimitQuery, string(providerID), scope))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProviderRateLimit{
			ProviderID: providerID,
			Scope:      scope,
		}, false, nil
	}
	if err != nil {
		return domain.ProviderRateLimit{}, false, wrap("get provider rate limit", err)
	}
	return state, true, nil
}

// ListProviderRateLimits returns every durable provider rate-limit state row.
// The scheduler reconstructs its in-memory guard from this list at startup and
// refreshes it while running, so a restart cannot silently lose a cooldown.
func (s *Store) ListProviderRateLimits(ctx context.Context) ([]domain.ProviderRateLimit, error) {
	db, err := s.executor()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, listProviderRateLimitsQuery)
	if err != nil {
		return nil, wrap("list provider rate limits", err)
	}
	defer rows.Close()

	states := make([]domain.ProviderRateLimit, 0)
	for rows.Next() {
		state, err := scanProviderRateLimit(rows)
		if err != nil {
			return nil, wrap("scan provider rate limit", err)
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate provider rate limits", err)
	}
	return states, nil
}

// UpsertProviderRateLimit writes one durable adaptive state row. It is
// transaction-scoped when called from the outcome transaction; it never begins
// or commits a transaction.
func (s *Store) UpsertProviderRateLimit(ctx context.Context, state domain.ProviderRateLimit, now time.Time) error {
	providerID, scope, err := validateProviderRateLimitKey(state.ProviderID, state.Scope)
	if err != nil {
		return err
	}
	if state.Consecutive429s < 0 {
		return errors.New("store: consecutive 429 count must not be negative")
	}
	if state.AdaptiveInterval < 0 {
		return errors.New("store: adaptive interval must not be negative")
	}
	if state.ConsecutiveSuccesses < 0 {
		return errors.New("store: consecutive success count must not be negative")
	}
	if state.LastRetryAfter < 0 {
		return errors.New("store: retry-after must not be negative")
	}
	if now.IsZero() {
		return errors.New("store: provider rate limit update time must be set")
	}
	for name, value := range map[string]*time.Time{
		"cooldown_until":  state.CooldownUntil,
		"last_429_at":     state.Last429At,
		"last_success_at": state.LastSuccessAt,
	} {
		if value != nil && value.IsZero() {
			return fmt.Errorf("store: provider rate limit %s must not be a zero time", name)
		}
	}
	db, err := s.executor()
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, upsertProviderRateLimitQuery,
		string(providerID),
		scope,
		state.Consecutive429s,
		state.AdaptiveInterval.Milliseconds(),
		optionalTimeValue(state.CooldownUntil),
		state.ConsecutiveSuccesses,
		optionalTimeValue(state.Last429At),
		optionalTimeValue(state.LastSuccessAt),
		state.LastRetryAfter.Milliseconds(),
		formatTime(now),
	); err != nil {
		return wrap("upsert provider rate limit", err)
	}
	return nil
}

func scanProviderRateLimit(scanner providerRateLimitScanner) (domain.ProviderRateLimit, error) {
	var state domain.ProviderRateLimit
	var providerID string
	var adaptiveMS int64
	var retryAfterMS int64
	var cooldownUntil sql.NullString
	var last429At sql.NullString
	var lastSuccessAt sql.NullString
	var updatedAt string
	if err := scanner.Scan(
		&providerID,
		&state.Scope,
		&state.Consecutive429s,
		&adaptiveMS,
		&cooldownUntil,
		&state.ConsecutiveSuccesses,
		&last429At,
		&lastSuccessAt,
		&retryAfterMS,
		&updatedAt,
	); err != nil {
		return domain.ProviderRateLimit{}, err
	}
	if adaptiveMS < 0 || retryAfterMS < 0 {
		return domain.ProviderRateLimit{}, errors.New("store: provider rate limit duration is negative")
	}
	state.ProviderID = provider.ID(providerID)
	state.AdaptiveInterval = time.Duration(adaptiveMS) * time.Millisecond
	state.LastRetryAfter = time.Duration(retryAfterMS) * time.Millisecond
	var err error
	if state.CooldownUntil, err = parseOptionalTime(cooldownUntil); err != nil {
		return domain.ProviderRateLimit{}, err
	}
	if state.Last429At, err = parseOptionalTime(last429At); err != nil {
		return domain.ProviderRateLimit{}, err
	}
	if state.LastSuccessAt, err = parseOptionalTime(lastSuccessAt); err != nil {
		return domain.ProviderRateLimit{}, err
	}
	if state.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return domain.ProviderRateLimit{}, err
	}
	return state, nil
}

func validateProviderRateLimitKey(providerID provider.ID, scope string) (provider.ID, string, error) {
	if providerID == "" {
		return "", "", errors.New("store: provider id must not be empty")
	}
	if scope == "" {
		scope = domain.ProviderRateLimitScopeProvider
	}
	if len(scope) > 64 {
		return "", "", errors.New("store: provider rate limit scope is too long")
	}
	return providerID, scope, nil
}

func optionalTimeValue(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

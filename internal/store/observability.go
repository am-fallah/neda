package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

// ProviderTrafficMinute is the exact logical-check aggregate for one provider
// and UTC minute, derived from check_runs.
type ProviderTrafficMinute struct {
	ProviderID     provider.ID
	MinuteStart    time.Time
	Checks         int64
	Successes      int64
	Failures       int64
	RateLimited    int64
	Interrupted    int64
	Discarded      int64
	Incomplete     int64
	DurationSumMS  int64
	DurationCount  int64
	Buckets        [10]int64
	QueueWaitSumMS int64
	QueueWaitCount int64
}

// ProviderMinuteMetrics is one durable rollup row.
type ProviderMinuteMetrics struct {
	MinuteStart time.Time
	ProviderID  provider.ID

	ProviderChecks int64
	Successes      int64
	Failures       int64
	RateLimited    int64
	Interrupted    int64
	Discarded      int64
	Incomplete     int64
	DurationSumMS  int64
	DurationCount  int64
	Buckets        [10]int64

	DispatchAttempts    int64
	SchedulerSamples    int64
	DueAvg              int64
	DueMax              int64
	OverdueMax          int64
	DeferredAvg         int64
	DeferredMax         int64
	InFlightMax         int64
	OldestDueAgeMaxMS   int64
	DeferralCooldown    int64
	DeferralSpacing     int64
	DeferralConcurrency int64
	QueueWaitSumMS      int64
	QueueWaitCount      int64
	TrafficDerivedAt    *time.Time
	QueueSampledAt      *time.Time
	LastExecutionAt     *time.Time
}

// ProviderQueueSample is one bounded point-in-time scheduler sample. The ID is
// the durable identity that makes replay idempotent across process restarts.
type ProviderQueueSample struct {
	ID                    string
	ProviderID            provider.ID
	SampledAt             time.Time
	MinuteStart           time.Time
	Due                   int64
	InFlight              int64
	Queued                int64
	Overdue               int64
	Deferred              int64
	DeferredByCooldown    int64
	DeferredBySpacing     int64
	DeferredByConcurrency int64
	OldestPendingAgeMS    int64
	NextEligibleAt        *time.Time
	BlockedByConcurrency  bool
	Dispatched            int64
	CreatedAt             time.Time
}

// ProviderDeferralEpisode is one durable logical deferral episode. Identity is
// target + reason, never next_due_at.
type ProviderDeferralEpisode struct {
	ID         string
	ProviderID provider.ID
	TargetID   *int64
	Reason     string
	StartedAt  time.Time
	LastSeenAt time.Time
	ResolvedAt *time.Time
	CreatedAt  time.Time
}

// Provider rate-limit event kinds. Every observed logical 429 is
// rate_limited; escalation versus maintenance is derived from
// previous_level/escalation_level. The other kinds are separate transitions.
const (
	RateLimitEventRateLimited      = "rate_limited"
	RateLimitEventCooldownExtended = "cooldown_extended"
	RateLimitEventRecoveryProgress = "recovery_progress"
	RateLimitEventRecoveryStep     = "recovery_step"
	RateLimitEventRecovered        = "recovered"
)

// ProviderRateLimitEvent is one best-effort adaptive/cooldown transition.
type ProviderRateLimitEvent struct {
	ID                    int64
	ProviderID            provider.ID
	Scope                 string
	OccurredAt            time.Time
	Kind                  string
	PreviousLevel         int
	EscalationLevel       int
	PreviousAdaptive      time.Duration
	Adaptive              time.Duration
	PreviousCooldownUntil *time.Time
	CooldownUntil         *time.Time
	RetryAfter            time.Duration
	CheckRunID            *int64
	CreatedAt             time.Time
}

// ProviderActiveTarget is the bounded target-schedule row shown on the
// provider detail page.
type ProviderActiveTarget struct {
	TargetID            int64
	MonitorID           string
	MonitorLabel        string
	ProviderID          provider.ID
	NextDueAt           *time.Time
	ConsecutiveFailures int
	LastOutcome         string
	LastErrorKind       string
}

// ProviderScheduleSummary is the bounded per-provider schedule projection used
// for health-state derivation.
type ProviderScheduleSummary struct {
	ActiveTargets     int
	EarliestNextDueAt *time.Time
}

const providerTrafficMinuteQuery = `SELECT mp.provider_id, cr.provider_started_at,
	cr.outcome, cr.provider_error_kind, cr.provider_duration_ms, cr.queue_wait_ms
FROM check_runs cr
JOIN monitor_providers mp ON mp.id = cr.monitor_provider_id
WHERE cr.provider_request = 1
  AND julianday(cr.provider_started_at) >= julianday(?)
  AND julianday(cr.provider_started_at) < julianday(?)
ORDER BY mp.provider_id, cr.provider_started_at`

// AggregateProviderTraffic returns exact traffic aggregates for a time range.
// It is used by the background minute aggregator, not by the scheduler.
func (s *Store) AggregateProviderTraffic(ctx context.Context, from, to time.Time) ([]ProviderTrafficMinute, error) {
	db, err := s.executor()
	if err != nil {
		return nil, err
	}
	if !to.After(from) {
		return nil, fmt.Errorf("store: traffic aggregation range is invalid")
	}
	rows, err := db.QueryContext(ctx, providerTrafficMinuteQuery, formatTime(from), formatTime(to))
	if err != nil {
		return nil, wrap("aggregate provider traffic", err)
	}
	defer rows.Close()

	byKey := make(map[string]*ProviderTrafficMinute)
	order := make([]string, 0)
	for rows.Next() {
		var providerID, startedAt, outcome string
		var durationMS, queueWaitMS sql.NullInt64
		var errorKindText sql.NullString
		if err := rows.Scan(&providerID, &startedAt, &outcome, &errorKindText, &durationMS, &queueWaitMS); err != nil {
			return nil, wrap("scan provider traffic", err)
		}
		started, err := parseTime(startedAt)
		if err != nil {
			return nil, err
		}
		minute := started.UTC().Truncate(time.Minute)
		key := providerID + "\x00" + minute.Format(time.RFC3339)
		aggregate := byKey[key]
		if aggregate == nil {
			aggregate = &ProviderTrafficMinute{ProviderID: provider.ID(providerID), MinuteStart: minute}
			byKey[key] = aggregate
			order = append(order, key)
		}
		aggregate.Checks++
		errorKind := ""
		if errorKindText.Valid {
			errorKind = errorKindText.String
		}
		durationEligible := true
		switch outcome {
		case "success", "empty":
			if errorKind == string(provider.ErrCanceled) {
				aggregate.Interrupted++
				durationEligible = false
			} else {
				aggregate.Successes++
			}
		case "abandoned":
			aggregate.Interrupted++
			durationEligible = false
		case "discarded":
			aggregate.Discarded++
			if errorKind == string(provider.ErrRateLimited) {
				aggregate.RateLimited++
			}
		case "error":
			if errorKind == string(provider.ErrCanceled) {
				aggregate.Interrupted++
				durationEligible = false
			} else {
				aggregate.Failures++
				if errorKind == string(provider.ErrRateLimited) {
					aggregate.RateLimited++
				}
			}
		default:
			// provider_request=1 with outcome='running' cannot be produced by the
			// normal finalization path, but treat it as an explicit incomplete
			// observation rather than a success.
			aggregate.Incomplete++
			durationEligible = false
		}
		if durationMS.Valid && durationEligible {
			aggregate.DurationSumMS += durationMS.Int64
			aggregate.DurationCount++
			bucket := durationBucket(durationMS.Int64)
			aggregate.Buckets[bucket]++
		}
		if queueWaitMS.Valid {
			aggregate.QueueWaitSumMS += queueWaitMS.Int64
			aggregate.QueueWaitCount++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate provider traffic", err)
	}
	result := make([]ProviderTrafficMinute, 0, len(order))
	for _, key := range order {
		result = append(result, *byKey[key])
	}
	return result, nil
}

// durationBucket maps a non-negative millisecond duration into ten disjoint
// buckets. Bucket i covers (bound[i-1], bound[i]]; the last bucket is >20000.
func durationBucket(ms int64) int {
	switch {
	case ms <= 50:
		return 0
	case ms <= 100:
		return 1
	case ms <= 250:
		return 2
	case ms <= 500:
		return 3
	case ms <= 1000:
		return 4
	case ms <= 2000:
		return 5
	case ms <= 5000:
		return 6
	case ms <= 10000:
		return 7
	case ms <= 20000:
		return 8
	default:
		return 9
	}
}

const upsertProviderTrafficQuery = `INSERT INTO provider_minute_metrics (
	minute_start, provider_id, provider_checks, successes, failures, rate_limited,
	interrupted, discarded, incomplete, duration_sum_ms, duration_count,
	dur_0_50, dur_51_100, dur_101_250, dur_251_500, dur_501_1000,
	dur_1001_2000, dur_2001_5000, dur_5001_10000, dur_10001_20000,
	dur_gt_20000, queue_wait_sum_ms, queue_wait_count, traffic_derived_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(minute_start, provider_id) DO UPDATE SET
	provider_checks = excluded.provider_checks,
	successes = excluded.successes,
	failures = excluded.failures,
	rate_limited = excluded.rate_limited,
	interrupted = excluded.interrupted,
	discarded = excluded.discarded,
	incomplete = excluded.incomplete,
	duration_sum_ms = excluded.duration_sum_ms,
	duration_count = excluded.duration_count,
	dur_0_50 = excluded.dur_0_50,
	dur_51_100 = excluded.dur_51_100,
	dur_101_250 = excluded.dur_101_250,
	dur_251_500 = excluded.dur_251_500,
	dur_501_1000 = excluded.dur_501_1000,
	dur_1001_2000 = excluded.dur_1001_2000,
	dur_2001_5000 = excluded.dur_2001_5000,
	dur_5001_10000 = excluded.dur_5001_10000,
	dur_10001_20000 = excluded.dur_10001_20000,
	dur_gt_20000 = excluded.dur_gt_20000,
	queue_wait_sum_ms = excluded.queue_wait_sum_ms,
	queue_wait_count = excluded.queue_wait_count,
	traffic_derived_at = excluded.traffic_derived_at`

// UpsertProviderTrafficMinutes writes the traffic group without touching queue
// columns owned by the runtime sampler. Re-processing a minute replaces the
// exact recomputed values, so replay is idempotent.
func (s *Store) UpsertProviderTrafficMinutes(ctx context.Context, minutes []ProviderTrafficMinute, now time.Time) error {
	if len(minutes) == 0 {
		return nil
	}
	derivedAt := formatTime(now.UTC())
	return s.WithTx(ctx, func(tx *Store) error {
		for _, minute := range minutes {
			if _, err := tx.db.ExecContext(ctx, upsertProviderTrafficQuery,
				formatTime(minute.MinuteStart), string(minute.ProviderID),
				minute.Checks, minute.Successes, minute.Failures, minute.RateLimited,
				minute.Interrupted, minute.Discarded, minute.Incomplete,
				minute.DurationSumMS, minute.DurationCount,
				minute.Buckets[0], minute.Buckets[1], minute.Buckets[2], minute.Buckets[3],
				minute.Buckets[4], minute.Buckets[5], minute.Buckets[6], minute.Buckets[7],
				minute.Buckets[8], minute.Buckets[9], minute.QueueWaitSumMS, minute.QueueWaitCount,
				derivedAt,
			); err != nil {
				return wrap("upsert provider traffic", err)
			}
		}
		return nil
	})
}

const insertProviderQueueSampleQuery = `INSERT INTO provider_queue_samples (
	id, provider_id, sampled_at, minute_start, due, in_flight, queued, overdue,
	deferred, deferred_by_cooldown, deferred_by_spacing, deferred_by_concurrency,
	oldest_pending_age_ms, next_eligible_at, blocked_by_concurrency, dispatched, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING`

const upsertProviderDeferralEpisodeQuery = `INSERT INTO provider_deferral_episodes (
	id, provider_id, target_id, reason, started_at, last_seen_at, resolved_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	last_seen_at = excluded.last_seen_at,
	resolved_at = COALESCE(provider_deferral_episodes.resolved_at, excluded.resolved_at)`

// PersistObservabilityBatch writes durable queue samples and deferral-episode
// updates in one transaction. Sample replay is ignored by stable sample ID;
// episode updates are idempotent and never reopen a resolved episode.
func (s *Store) PersistObservabilityBatch(ctx context.Context, samples []ProviderQueueSample, episodes []ProviderDeferralEpisode) error {
	if len(samples) == 0 && len(episodes) == 0 {
		return nil
	}
	return s.WithTx(ctx, func(tx *Store) error {
		for _, episode := range episodes {
			if episode.ID == "" || episode.ProviderID == "" {
				return fmt.Errorf("store: deferral episode identity is required")
			}
			if episode.CreatedAt.IsZero() {
				episode.CreatedAt = episode.StartedAt
			}
			if _, err := tx.db.ExecContext(ctx, upsertProviderDeferralEpisodeQuery,
				episode.ID, string(episode.ProviderID), episode.TargetID, episode.Reason,
				formatTime(episode.StartedAt), formatTime(episode.LastSeenAt),
				optionalTimeValue(episode.ResolvedAt), formatTime(episode.CreatedAt),
			); err != nil {
				return wrap("upsert provider deferral episode", err)
			}
		}
		for _, sample := range samples {
			if sample.ID == "" || sample.ProviderID == "" {
				return fmt.Errorf("store: queue sample identity is required")
			}
			if sample.CreatedAt.IsZero() {
				sample.CreatedAt = sample.SampledAt
			}
			if _, err := tx.db.ExecContext(ctx, insertProviderQueueSampleQuery,
				sample.ID, string(sample.ProviderID), formatTime(sample.SampledAt),
				formatTime(sample.MinuteStart), sample.Due, sample.InFlight, sample.Queued,
				sample.Overdue, sample.Deferred, sample.DeferredByCooldown,
				sample.DeferredBySpacing, sample.DeferredByConcurrency,
				sample.OldestPendingAgeMS, optionalTimeValue(sample.NextEligibleAt),
				boolToInt(sample.BlockedByConcurrency), sample.Dispatched,
				formatTime(sample.CreatedAt),
			); err != nil {
				return wrap("insert provider queue sample", err)
			}
		}
		return nil
	})
}

const recomputeProviderQueueMinutesQuery = `WITH sample_agg AS (
	SELECT minute_start, provider_id,
		COUNT(*) AS samples,
		COALESCE(SUM(dispatched), 0) AS dispatched,
		COALESCE(SUM(due) / COUNT(*), 0) AS due_avg,
		COALESCE(MAX(due), 0) AS due_max,
		COALESCE(MAX(overdue), 0) AS overdue_max,
		COALESCE(SUM(deferred) / COUNT(*), 0) AS deferred_avg,
		COALESCE(MAX(deferred), 0) AS deferred_max,
		COALESCE(MAX(in_flight), 0) AS in_flight_max,
		COALESCE(MAX(oldest_pending_age_ms), 0) AS oldest_due_age_max_ms,
		MAX(sampled_at) AS queue_sampled_at,
		MAX(sampled_at) AS last_execution_at
	FROM provider_queue_samples
	WHERE minute_start >= ? AND minute_start < ?
	GROUP BY minute_start, provider_id
), episode_agg AS (
	SELECT strftime('%Y-%m-%dT%H:%M:00Z', started_at) AS minute_start,
		provider_id,
		SUM(CASE WHEN reason = 'cooldown' THEN 1 ELSE 0 END) AS cooldown,
		SUM(CASE WHEN reason = 'spacing' THEN 1 ELSE 0 END) AS spacing,
		SUM(CASE WHEN reason = 'concurrency' THEN 1 ELSE 0 END) AS concurrency
	FROM provider_deferral_episodes
	WHERE started_at >= ? AND started_at < ?
	GROUP BY minute_start, provider_id
), keys AS (
	SELECT minute_start, provider_id FROM sample_agg
	UNION
	SELECT minute_start, provider_id FROM episode_agg
)
INSERT INTO provider_minute_metrics (
	minute_start, provider_id, dispatch_attempts, scheduler_samples, due_avg, due_max,
	overdue_max, deferred_avg, deferred_max, in_flight_max, oldest_due_age_max_ms,
	deferral_episodes_cooldown, deferral_episodes_spacing,
	deferral_episodes_concurrency, queue_sampled_at, last_execution_at
)
SELECT k.minute_start, k.provider_id,
	COALESCE(s.dispatched, 0), COALESCE(s.samples, 0), COALESCE(s.due_avg, 0),
	COALESCE(s.due_max, 0), COALESCE(s.overdue_max, 0), COALESCE(s.deferred_avg, 0),
	COALESCE(s.deferred_max, 0), COALESCE(s.in_flight_max, 0),
	COALESCE(s.oldest_due_age_max_ms, 0), COALESCE(e.cooldown, 0),
	COALESCE(e.spacing, 0), COALESCE(e.concurrency, 0),
	s.queue_sampled_at, s.last_execution_at
FROM keys k
LEFT JOIN sample_agg s ON s.minute_start = k.minute_start AND s.provider_id = k.provider_id
LEFT JOIN episode_agg e ON e.minute_start = k.minute_start AND e.provider_id = k.provider_id
ON CONFLICT(minute_start, provider_id) DO UPDATE SET
	dispatch_attempts = excluded.dispatch_attempts,
	scheduler_samples = excluded.scheduler_samples,
	due_avg = excluded.due_avg,
	due_max = excluded.due_max,
	overdue_max = excluded.overdue_max,
	deferred_avg = excluded.deferred_avg,
	deferred_max = excluded.deferred_max,
	in_flight_max = excluded.in_flight_max,
	oldest_due_age_max_ms = excluded.oldest_due_age_max_ms,
	deferral_episodes_cooldown = excluded.deferral_episodes_cooldown,
	deferral_episodes_spacing = excluded.deferral_episodes_spacing,
	deferral_episodes_concurrency = excluded.deferral_episodes_concurrency,
	queue_sampled_at = excluded.queue_sampled_at,
	last_execution_at = excluded.last_execution_at`

// RecomputeProviderQueueMinutes deterministically rebuilds the queue group
// from durable samples and episodes for the given half-open minute range.
// Traffic columns are left untouched.
func (s *Store) RecomputeProviderQueueMinutes(ctx context.Context, from, to time.Time) error {
	if !to.After(from) {
		return fmt.Errorf("store: queue recompute range is invalid")
	}
	fromText := formatTime(from.UTC())
	toText := formatTime(to.UTC())
	db, err := s.executor()
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, recomputeProviderQueueMinutesQuery,
		fromText, toText, fromText, toText,
	); err != nil {
		return wrap("recompute provider queue minutes", err)
	}
	return nil
}

const listActiveDeferralEpisodesQuery = `SELECT id, provider_id, target_id, reason,
	started_at, last_seen_at
FROM provider_deferral_episodes
WHERE resolved_at IS NULL AND target_id IS NOT NULL
ORDER BY started_at, id`

const closeOrphanedDeferralEpisodesQuery = `UPDATE provider_deferral_episodes
SET resolved_at = ?
WHERE resolved_at IS NULL AND target_id IS NULL`

// RestoreActiveDeferralEpisodes returns active episodes for recorder seeding and
// closes orphaned active rows whose target was deleted. It is startup-only.
func (s *Store) RestoreActiveDeferralEpisodes(ctx context.Context, now time.Time) ([]ProviderDeferralEpisode, error) {
	if now.IsZero() {
		return nil, fmt.Errorf("store: restore timestamp is required")
	}
	result := make([]ProviderDeferralEpisode, 0)
	err := s.WithTx(ctx, func(tx *Store) error {
		if _, err := tx.db.ExecContext(ctx, closeOrphanedDeferralEpisodesQuery, formatTime(now)); err != nil {
			return wrap("close orphaned deferral episodes", err)
		}
		rows, err := tx.db.QueryContext(ctx, listActiveDeferralEpisodesQuery)
		if err != nil {
			return wrap("list active deferral episodes", err)
		}
		defer rows.Close()
		for rows.Next() {
			var episode ProviderDeferralEpisode
			var providerID, reason, startedAt, lastSeenAt string
			var targetID sql.NullInt64
			if err := rows.Scan(&episode.ID, &providerID, &targetID, &reason, &startedAt, &lastSeenAt); err != nil {
				return wrap("scan active deferral episode", err)
			}
			episode.ProviderID = provider.ID(providerID)
			episode.Reason = reason
			if targetID.Valid {
				value := targetID.Int64
				episode.TargetID = &value
			}
			if episode.StartedAt, err = parseTime(startedAt); err != nil {
				return err
			}
			if episode.LastSeenAt, err = parseTime(lastSeenAt); err != nil {
				return err
			}
			episode.CreatedAt = episode.StartedAt
			result = append(result, episode)
		}
		if err := rows.Err(); err != nil {
			return wrap("iterate active deferral episodes", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

const listProviderMinuteMetricsQuery = `SELECT minute_start, provider_id,
	provider_checks, successes, failures, rate_limited, interrupted, discarded,
	incomplete, duration_sum_ms, duration_count,
	dur_0_50, dur_51_100, dur_101_250, dur_251_500, dur_501_1000,
	dur_1001_2000, dur_2001_5000, dur_5001_10000, dur_10001_20000,
	dur_gt_20000, dispatch_attempts, scheduler_samples, due_avg, due_max, overdue_max,
	deferred_avg, deferred_max, in_flight_max, oldest_due_age_max_ms,
	deferral_episodes_cooldown, deferral_episodes_spacing, deferral_episodes_concurrency,
	queue_wait_sum_ms, queue_wait_count, traffic_derived_at, queue_sampled_at, last_execution_at
FROM provider_minute_metrics
WHERE provider_id = ? AND minute_start >= ?
ORDER BY minute_start`

// ListProviderMinuteMetrics returns rollup rows for one provider since a
// cutoff. It is a bounded read model and does not mutate state.
func (s *Store) ListProviderMinuteMetrics(ctx context.Context, providerID provider.ID, since time.Time) ([]ProviderMinuteMetrics, error) {
	if providerID == "" {
		return nil, fmt.Errorf("store: provider id is required")
	}
	db, err := s.executor()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, listProviderMinuteMetricsQuery, string(providerID), formatTime(since))
	if err != nil {
		return nil, wrap("list provider minute metrics", err)
	}
	defer rows.Close()
	result := make([]ProviderMinuteMetrics, 0)
	for rows.Next() {
		var row ProviderMinuteMetrics
		var providerID string
		var minuteStart string
		var trafficDerived, queueSampled, lastExecution sql.NullString
		if err := rows.Scan(
			&minuteStart, &providerID,
			&row.ProviderChecks, &row.Successes, &row.Failures, &row.RateLimited,
			&row.Interrupted, &row.Discarded, &row.Incomplete,
			&row.DurationSumMS, &row.DurationCount,
			&row.Buckets[0], &row.Buckets[1], &row.Buckets[2], &row.Buckets[3],
			&row.Buckets[4], &row.Buckets[5], &row.Buckets[6], &row.Buckets[7],
			&row.Buckets[8], &row.Buckets[9],
			&row.DispatchAttempts, &row.SchedulerSamples, &row.DueAvg, &row.DueMax, &row.OverdueMax,
			&row.DeferredAvg, &row.DeferredMax, &row.InFlightMax, &row.OldestDueAgeMaxMS,
			&row.DeferralCooldown, &row.DeferralSpacing, &row.DeferralConcurrency,
			&row.QueueWaitSumMS, &row.QueueWaitCount,
			&trafficDerived, &queueSampled, &lastExecution,
		); err != nil {
			return nil, wrap("scan provider minute metrics", err)
		}
		row.ProviderID = provider.ID(providerID)
		if row.MinuteStart, err = parseTime(minuteStart); err != nil {
			return nil, err
		}
		if row.TrafficDerivedAt, err = parseOptionalTime(trafficDerived); err != nil {
			return nil, err
		}
		if row.QueueSampledAt, err = parseOptionalTime(queueSampled); err != nil {
			return nil, err
		}
		if row.LastExecutionAt, err = parseOptionalTime(lastExecution); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate provider minute metrics", err)
	}
	return result, nil
}

const sumProviderTrafficQuery = `SELECT provider_id,
	COALESCE(SUM(provider_checks),0), COALESCE(SUM(successes),0),
	COALESCE(SUM(failures),0), COALESCE(SUM(rate_limited),0),
	COALESCE(SUM(interrupted),0), COALESCE(SUM(discarded),0),
	COALESCE(SUM(incomplete),0), COALESCE(SUM(duration_sum_ms),0),
	COALESCE(SUM(duration_count),0), COALESCE(SUM(queue_wait_sum_ms),0),
	COALESCE(SUM(queue_wait_count),0), COALESCE(SUM(dispatch_attempts),0),
	COALESCE(SUM(dur_0_50),0), COALESCE(SUM(dur_51_100),0),
	COALESCE(SUM(dur_101_250),0), COALESCE(SUM(dur_251_500),0),
	COALESCE(SUM(dur_501_1000),0), COALESCE(SUM(dur_1001_2000),0),
	COALESCE(SUM(dur_2001_5000),0), COALESCE(SUM(dur_5001_10000),0),
	COALESCE(SUM(dur_10001_20000),0), COALESCE(SUM(dur_gt_20000),0)
FROM provider_minute_metrics
WHERE minute_start >= ?
GROUP BY provider_id`

// ProviderTrafficTotals is the rollup sum for one provider.
type ProviderTrafficTotals struct {
	ProviderID       provider.ID
	Checks           int64
	Successes        int64
	Failures         int64
	RateLimited      int64
	Interrupted      int64
	Discarded        int64
	Incomplete       int64
	DurationSumMS    int64
	DurationCount    int64
	QueueWaitSumMS   int64
	QueueWaitCount   int64
	DispatchAttempts int64
	Buckets          [10]int64
}

// SumProviderTraffic returns per-provider totals from the durable rollup.
func (s *Store) SumProviderTraffic(ctx context.Context, since time.Time) (map[provider.ID]ProviderTrafficTotals, error) {
	db, err := s.executor()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, sumProviderTrafficQuery, formatTime(since))
	if err != nil {
		return nil, wrap("sum provider traffic", err)
	}
	defer rows.Close()
	result := make(map[provider.ID]ProviderTrafficTotals)
	for rows.Next() {
		var row ProviderTrafficTotals
		var providerID string
		if err := rows.Scan(&providerID, &row.Checks, &row.Successes, &row.Failures,
			&row.RateLimited, &row.Interrupted, &row.Discarded, &row.Incomplete,
			&row.DurationSumMS, &row.DurationCount,
			&row.QueueWaitSumMS, &row.QueueWaitCount, &row.DispatchAttempts,
			&row.Buckets[0], &row.Buckets[1], &row.Buckets[2], &row.Buckets[3],
			&row.Buckets[4], &row.Buckets[5], &row.Buckets[6], &row.Buckets[7],
			&row.Buckets[8], &row.Buckets[9]); err != nil {
			return nil, wrap("scan provider traffic totals", err)
		}
		row.ProviderID = provider.ID(providerID)
		result[row.ProviderID] = row
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate provider traffic totals", err)
	}
	return result, nil
}

const insertProviderRateLimitEventQuery = `INSERT INTO provider_rate_limit_events (
	provider_id, scope, occurred_at, kind, previous_level, escalation_level,
	previous_adaptive_ms, adaptive_ms, previous_cooldown_until, cooldown_until,
	retry_after_ms, check_run_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// InsertProviderRateLimitEvent writes one best-effort transition event.
func (s *Store) InsertProviderRateLimitEvent(ctx context.Context, event ProviderRateLimitEvent) error {
	if event.ProviderID == "" {
		return fmt.Errorf("store: provider id is required")
	}
	if event.Scope == "" {
		event.Scope = "provider"
	}
	now := event.CreatedAt
	if now.IsZero() {
		now = event.OccurredAt
	}
	db, err := s.executor()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, insertProviderRateLimitEventQuery,
		string(event.ProviderID), event.Scope, formatTime(event.OccurredAt), event.Kind,
		event.PreviousLevel, event.EscalationLevel,
		event.PreviousAdaptive.Milliseconds(), event.Adaptive.Milliseconds(),
		optionalTimeValue(event.PreviousCooldownUntil),
		optionalTimeValue(event.CooldownUntil), event.RetryAfter.Milliseconds(),
		event.CheckRunID, formatTime(now),
	)
	if err != nil {
		return wrap("insert provider rate limit event", err)
	}
	return nil
}

const listProviderRateLimitEventsQuery = `SELECT id, provider_id, scope, occurred_at, kind,
	previous_level, escalation_level, previous_adaptive_ms, adaptive_ms,
	previous_cooldown_until, cooldown_until, retry_after_ms, check_run_id, created_at
FROM provider_rate_limit_events
WHERE provider_id = ? AND julianday(occurred_at) >= julianday(?)
ORDER BY julianday(occurred_at) DESC, id DESC
LIMIT ?`

// ListProviderRateLimitEvents returns recent events for one provider.
func (s *Store) ListProviderRateLimitEvents(ctx context.Context, providerID provider.ID, since time.Time, limit int) ([]ProviderRateLimitEvent, error) {
	if providerID == "" {
		return nil, fmt.Errorf("store: provider id is required")
	}
	if limit < 1 || limit > 500 {
		return nil, fmt.Errorf("store: rate-limit event limit must be between 1 and 500")
	}
	db, err := s.executor()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, listProviderRateLimitEventsQuery, string(providerID), formatTime(since), limit)
	if err != nil {
		return nil, wrap("list provider rate limit events", err)
	}
	defer rows.Close()
	result := make([]ProviderRateLimitEvent, 0)
	for rows.Next() {
		var event ProviderRateLimitEvent
		var providerID, scope, occurredAt, kind, createdAt string
		var previousCooldown, cooldown sql.NullString
		var checkRunID sql.NullInt64
		var previousMS, adaptiveMS, retryMS int64
		if err := rows.Scan(&event.ID, &providerID, &scope, &occurredAt, &kind,
			&event.PreviousLevel, &event.EscalationLevel, &previousMS, &adaptiveMS,
			&previousCooldown, &cooldown, &retryMS, &checkRunID, &createdAt); err != nil {
			return nil, wrap("scan provider rate limit event", err)
		}
		event.ProviderID = provider.ID(providerID)
		event.Scope = scope
		event.Kind = kind
		event.PreviousAdaptive = time.Duration(previousMS) * time.Millisecond
		event.Adaptive = time.Duration(adaptiveMS) * time.Millisecond
		event.RetryAfter = time.Duration(retryMS) * time.Millisecond
		if event.OccurredAt, err = parseTime(occurredAt); err != nil {
			return nil, err
		}
		if event.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if event.PreviousCooldownUntil, err = parseOptionalTime(previousCooldown); err != nil {
			return nil, err
		}
		if event.CooldownUntil, err = parseOptionalTime(cooldown); err != nil {
			return nil, err
		}
		if checkRunID.Valid {
			value := checkRunID.Int64
			event.CheckRunID = &value
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate provider rate limit events", err)
	}
	return result, nil
}

const listProviderActiveTargetsQuery = `SELECT mp.id, mp.monitor_id, m.label, mp.provider_id,
	mp.next_due_at, mp.consecutive_failures, mp.last_outcome, mp.last_error_kind
FROM monitor_providers mp
JOIN monitors m ON m.id = mp.monitor_id
WHERE mp.active = 1 AND m.enabled = 1 AND m.expired_at IS NULL
  AND julianday(m.expires_at) > julianday(?)
  AND mp.provider_id = ?
ORDER BY (mp.next_due_at IS NULL), mp.next_due_at, mp.id
LIMIT ?`

// ListProviderActiveTargets returns current target scheduling rows for one
// provider. It is bounded and does not change scheduling state.
func (s *Store) ListProviderActiveTargets(ctx context.Context, providerID provider.ID, now time.Time, limit int) ([]ProviderActiveTarget, error) {
	if providerID == "" {
		return nil, fmt.Errorf("store: provider id is required")
	}
	if limit < 1 || limit > 500 {
		return nil, fmt.Errorf("store: target limit must be between 1 and 500")
	}
	db, err := s.executor()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, listProviderActiveTargetsQuery, formatTime(now), string(providerID), limit)
	if err != nil {
		return nil, wrap("list provider active targets", err)
	}
	defer rows.Close()
	result := make([]ProviderActiveTarget, 0)
	for rows.Next() {
		var target ProviderActiveTarget
		var providerID string
		var nextDue, lastOutcome, lastError sql.NullString
		if err := rows.Scan(&target.TargetID, &target.MonitorID, &target.MonitorLabel,
			&providerID, &nextDue, &target.ConsecutiveFailures, &lastOutcome, &lastError); err != nil {
			return nil, wrap("scan provider active target", err)
		}
		target.ProviderID = provider.ID(providerID)
		target.LastOutcome = optionalString(lastOutcome)
		target.LastErrorKind = optionalString(lastError)
		if target.NextDueAt, err = parseOptionalTime(nextDue); err != nil {
			return nil, err
		}
		result = append(result, target)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate provider active targets", err)
	}
	return result, nil
}

const providerScheduleSummaryQuery = `SELECT mp.provider_id, COUNT(*), MIN(mp.next_due_at)
FROM monitor_providers mp
JOIN monitors m ON m.id = mp.monitor_id
WHERE mp.active = 1 AND m.enabled = 1 AND m.expired_at IS NULL
  AND julianday(m.expires_at) > julianday(?)
GROUP BY mp.provider_id`

// ProviderScheduleSummaries returns active-target counts and the earliest next
// due time per provider for health-state derivation.
func (s *Store) ProviderScheduleSummaries(ctx context.Context, now time.Time) (map[provider.ID]ProviderScheduleSummary, error) {
	db, err := s.executor()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, providerScheduleSummaryQuery, formatTime(now))
	if err != nil {
		return nil, wrap("provider schedule summaries", err)
	}
	defer rows.Close()
	result := make(map[provider.ID]ProviderScheduleSummary)
	for rows.Next() {
		var providerID string
		var summary ProviderScheduleSummary
		var earliest sql.NullString
		if err := rows.Scan(&providerID, &summary.ActiveTargets, &earliest); err != nil {
			return nil, wrap("scan provider schedule summary", err)
		}
		if summary.EarliestNextDueAt, err = parseOptionalTime(earliest); err != nil {
			return nil, err
		}
		result[provider.ID(providerID)] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate provider schedule summaries", err)
	}
	return result, nil
}

const countActiveTargetsQuery = `SELECT mp.provider_id, COUNT(*)
FROM monitor_providers mp
JOIN monitors m ON m.id = mp.monitor_id
WHERE mp.active = 1 AND m.enabled = 1 AND m.expired_at IS NULL
  AND julianday(m.expires_at) > julianday(?)
GROUP BY mp.provider_id`

// CountActiveTargetsByProvider returns eligible target counts.
func (s *Store) CountActiveTargetsByProvider(ctx context.Context, now time.Time) (map[provider.ID]int, error) {
	db, err := s.executor()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, countActiveTargetsQuery, formatTime(now))
	if err != nil {
		return nil, wrap("count active targets", err)
	}
	defer rows.Close()
	result := make(map[provider.ID]int)
	for rows.Next() {
		var providerID string
		var count int
		if err := rows.Scan(&providerID, &count); err != nil {
			return nil, wrap("scan active target count", err)
		}
		result[provider.ID(providerID)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate active target count", err)
	}
	return result, nil
}

const countIncompleteChecksQuery = `SELECT mp.provider_id, COUNT(*)
FROM check_runs cr
JOIN monitor_providers mp ON mp.id = cr.monitor_provider_id
WHERE cr.outcome = 'abandoned' AND cr.provider_request = 0
  AND cr.error_kind IS NULL
  AND julianday(cr.started_at) >= julianday(?)
GROUP BY mp.provider_id`

// CountIncompleteChecksByProvider returns crash-abandoned attempts that have
// no durable provider-request marker. They are never counted as successes or
// ordinary failures.
func (s *Store) CountIncompleteChecksByProvider(ctx context.Context, since time.Time) (map[provider.ID]int64, error) {
	db, err := s.executor()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, countIncompleteChecksQuery, formatTime(since))
	if err != nil {
		return nil, wrap("count incomplete checks", err)
	}
	defer rows.Close()
	result := make(map[provider.ID]int64)
	for rows.Next() {
		var providerID string
		var count int64
		if err := rows.Scan(&providerID, &count); err != nil {
			return nil, wrap("scan incomplete check count", err)
		}
		result[provider.ID(providerID)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate incomplete check count", err)
	}
	return result, nil
}

// ProviderCheckActivity is one logical provider check for the recent activity
// timeline. Incomplete rows are crash-abandoned attempts with no durable
// provider-request marker; their duration and error kind are unknown.
type ProviderCheckActivity struct {
	CheckRunID        int64
	MonitorID         string
	MonitorLabel      string
	ProviderID        provider.ID
	StartedAt         time.Time
	FinishedAt        *time.Time
	Outcome           string
	ProviderErrorKind string
	DurationMS        *int64
	QueueWaitMS       *int64
	Incomplete        bool
}

const listProviderCheckActivityQuery = `SELECT cr.id, mp.monitor_id, m.label, mp.provider_id,
	COALESCE(cr.provider_started_at, cr.started_at), cr.finished_at, cr.outcome,
	cr.provider_error_kind, cr.provider_duration_ms, cr.queue_wait_ms, cr.provider_request
FROM check_runs cr
JOIN monitor_providers mp ON mp.id = cr.monitor_provider_id
JOIN monitors m ON m.id = mp.monitor_id
WHERE (cr.provider_request = 1 OR (cr.outcome = 'abandoned' AND cr.provider_request = 0 AND cr.error_kind IS NULL))
  AND julianday(COALESCE(cr.provider_started_at, cr.started_at)) >= julianday(?)
  AND mp.provider_id = ?
ORDER BY julianday(COALESCE(cr.provider_started_at, cr.started_at)) DESC, cr.id DESC
LIMIT ?`

// ListProviderCheckActivity returns recent logical checks for one provider.
func (s *Store) ListProviderCheckActivity(ctx context.Context, providerID provider.ID, since time.Time, limit int) ([]ProviderCheckActivity, error) {
	if providerID == "" {
		return nil, fmt.Errorf("store: provider id is required")
	}
	if limit < 1 || limit > 200 {
		return nil, fmt.Errorf("store: activity limit must be between 1 and 200")
	}
	db, err := s.executor()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, listProviderCheckActivityQuery, formatTime(since), string(providerID), limit)
	if err != nil {
		return nil, wrap("list provider check activity", err)
	}
	defer rows.Close()
	result := make([]ProviderCheckActivity, 0)
	for rows.Next() {
		var activity ProviderCheckActivity
		var providerID, startedAt, outcome string
		var finishedAt, providerError sql.NullString
		var durationMS, queueWaitMS sql.NullInt64
		var providerRequest int
		if err := rows.Scan(&activity.CheckRunID, &activity.MonitorID, &activity.MonitorLabel,
			&providerID, &startedAt, &finishedAt, &outcome, &providerError,
			&durationMS, &queueWaitMS, &providerRequest); err != nil {
			return nil, wrap("scan provider check activity", err)
		}
		activity.ProviderID = provider.ID(providerID)
		activity.Outcome = outcome
		activity.ProviderErrorKind = optionalString(providerError)
		activity.Incomplete = providerRequest == 0 && outcome == "abandoned" && !providerError.Valid
		if activity.StartedAt, err = parseTime(startedAt); err != nil {
			return nil, err
		}
		if activity.FinishedAt, err = parseOptionalTime(finishedAt); err != nil {
			return nil, err
		}
		if durationMS.Valid {
			value := durationMS.Int64
			activity.DurationMS = &value
		}
		if queueWaitMS.Valid {
			value := queueWaitMS.Int64
			activity.QueueWaitMS = &value
		}
		result = append(result, activity)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate provider check activity", err)
	}
	return result, nil
}

// DeleteProviderQueueSamplesBefore removes source samples outside retention.
func (s *Store) DeleteProviderQueueSamplesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	db, err := s.executor()
	if err != nil {
		return 0, err
	}
	result, err := db.ExecContext(ctx, `DELETE FROM provider_queue_samples WHERE minute_start < ?`, formatTime(cutoff))
	if err != nil {
		return 0, wrap("delete provider queue samples", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: provider queue sample rows: %w", err)
	}
	return count, nil
}

// DeleteResolvedDeferralEpisodesBefore removes resolved episode history outside
// retention while keeping active episodes for restart deduplication.
func (s *Store) DeleteResolvedDeferralEpisodesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	db, err := s.executor()
	if err != nil {
		return 0, err
	}
	result, err := db.ExecContext(ctx,
		`DELETE FROM provider_deferral_episodes WHERE resolved_at IS NOT NULL AND started_at < ?`,
		formatTime(cutoff))
	if err != nil {
		return 0, wrap("delete resolved deferral episodes", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: deferral episode rows: %w", err)
	}
	return count, nil
}

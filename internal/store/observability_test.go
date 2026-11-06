package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/store"
)

func insertObservabilityTarget(t *testing.T, db *sql.DB, monitorID, providerID string) int64 {
	t.Helper()
	now := testNow.Format(time.RFC3339Nano)
	expires := testNow.Add(24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO monitors
		(id, label, origin_name, destination_name, departure_date, adults, children, infants,
		 enabled, expires_at, created_at, updated_at)
		VALUES (?, ?, 'Origin', 'Destination', '2026-12-15', 1, 0, 0, 1, ?, ?, ?)`,
		monitorID, "Monitor "+monitorID, expires, now, now); err != nil {
		t.Fatalf("insert monitor: %v", err)
	}
	result, err := db.Exec(`INSERT INTO monitor_providers
		(monitor_id, provider_id, active, origin_id, destination_id, config_version,
		 next_due_at, consecutive_failures, created_at, updated_at)
		VALUES (?, ?, 1, 'origin', 'destination', 1, ?, 0, ?, ?)`,
		monitorID, providerID, now, now, now)
	if err != nil {
		t.Fatalf("insert target: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("target id: %v", err)
	}
	return id
}

func insertObservabilityCheck(t *testing.T, db *sql.DB, targetID int64, startedAt time.Time, outcome, providerError string, durationMS, queueWaitMS int64, providerRequest bool) {
	t.Helper()
	finished := startedAt.Add(time.Duration(durationMS) * time.Millisecond)
	var providerStarted any
	var providerDuration any
	var providerErrorValue any
	var queueWait any
	if providerRequest {
		providerStarted = startedAt.Format(time.RFC3339Nano)
		providerDuration = durationMS
		queueWait = queueWaitMS
	}
	if providerError != "" {
		providerErrorValue = providerError
	}
	finishedValue := finished.Format(time.RFC3339Nano)
	if outcome == "running" {
		finishedValue = ""
	}
	if _, err := db.Exec(`INSERT INTO check_runs (
		monitor_provider_id, config_version, started_at, finished_at, outcome,
		error_kind, http_status, duration_ms, journey_count, offering_count,
		reservable_count, not_reservable_count, unknown_count, warning_count, created_at,
		provider_request, provider_started_at, provider_duration_ms, provider_error_kind,
		queue_wait_ms, expected_spacing_ms, configured_interval_ms, adaptive_interval_ms
	) VALUES (?, 1, ?, NULLIF(?, ''), ?, ?, NULL, ?, 0, 0, 0, 0, 0, 0, ?, ?, ?, ?, ?, ?, 5000, 300000, 600000)`,
		targetID, startedAt.Format(time.RFC3339Nano), finishedValue, outcome,
		nullIfEmpty(providerError), durationMS, startedAt.Format(time.RFC3339Nano),
		boolInt(providerRequest), providerStarted, providerDuration, providerErrorValue,
		queueWait); err != nil {
		t.Fatalf("insert check run: %v", err)
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func TestObservabilityTrafficAggregationAndRollup(t *testing.T) {
	st, db := newTestStore(t)
	ctx := context.Background()
	targetID := insertObservabilityTarget(t, db, "obs-monitor", "obs-provider")

	start := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	insertObservabilityCheck(t, db, targetID, start.Add(10*time.Second), "success", "", 100, 10, true)
	insertObservabilityCheck(t, db, targetID, start.Add(20*time.Second), "error", "rate_limited", 200, 20, true)
	insertObservabilityCheck(t, db, targetID, start.Add(30*time.Second), "abandoned", "canceled", 50, 30, true)
	insertObservabilityCheck(t, db, targetID, start.Add(35*time.Second), "discarded", "", 300, 35, true)
	// Crash-abandoned rows have no durable provider-request marker and must not
	// be counted as completed checks.
	insertObservabilityCheck(t, db, targetID, start.Add(40*time.Second), "abandoned", "", 0, 0, false)

	minutes, err := st.AggregateProviderTraffic(ctx, start, start.Add(time.Minute))
	if err != nil {
		t.Fatalf("AggregateProviderTraffic: %v", err)
	}
	if len(minutes) != 1 {
		t.Fatalf("minutes = %d, want 1", len(minutes))
	}
	minute := minutes[0]
	if minute.Checks != 4 || minute.Successes != 1 || minute.Failures != 1 ||
		minute.RateLimited != 1 || minute.Interrupted != 1 || minute.Discarded != 1 ||
		minute.Incomplete != 0 {
		t.Fatalf("traffic = %+v", minute)
	}
	// Interrupted attempts are excluded from duration; discarded completed
	// adapter calls are included.
	if minute.DurationSumMS != 600 || minute.DurationCount != 3 {
		t.Fatalf("duration = sum %d count %d, want 600/3", minute.DurationSumMS, minute.DurationCount)
	}
	if minute.QueueWaitSumMS != 95 || minute.QueueWaitCount != 4 {
		t.Fatalf("queue wait = sum %d count %d, want 95/4", minute.QueueWaitSumMS, minute.QueueWaitCount)
	}

	// Duplicate processing replaces exact values instead of double-counting.
	for run := 0; run < 2; run++ {
		if err := st.UpsertProviderTrafficMinutes(ctx, minutes, start.Add(time.Minute)); err != nil {
			t.Fatalf("UpsertProviderTrafficMinutes run %d: %v", run, err)
		}
	}

	episodeTarget := targetID
	samples := []store.ProviderQueueSample{
		{
			ID: "sample-1", ProviderID: "obs-provider", SampledAt: start.Add(10 * time.Second),
			MinuteStart: start, Due: 2, InFlight: 0, Queued: 2, Deferred: 1,
			DeferredBySpacing: 1, Dispatched: 1, CreatedAt: start.Add(10 * time.Second),
		},
		{
			ID: "sample-2", ProviderID: "obs-provider", SampledAt: start.Add(25 * time.Second),
			MinuteStart: start, Due: 4, InFlight: 1, Queued: 3, Deferred: 3,
			DeferredBySpacing: 3, Dispatched: 2, CreatedAt: start.Add(25 * time.Second),
		},
	}
	episodes := []store.ProviderDeferralEpisode{
		{
			ID: "episode-1", ProviderID: "obs-provider", TargetID: &episodeTarget,
			Reason: "cooldown", StartedAt: start.Add(12 * time.Second),
			LastSeenAt: start.Add(12 * time.Second), CreatedAt: start.Add(12 * time.Second),
		},
		{
			ID: "episode-2", ProviderID: "obs-provider", TargetID: &episodeTarget,
			Reason: "spacing", StartedAt: start.Add(18 * time.Second),
			LastSeenAt: start.Add(18 * time.Second), CreatedAt: start.Add(18 * time.Second),
		},
	}
	if err := st.PersistObservabilityBatch(ctx, samples, episodes); err != nil {
		t.Fatalf("PersistObservabilityBatch: %v", err)
	}
	// Replay the same identities and updates; source rows and rollups must not
	// duplicate.
	if err := st.PersistObservabilityBatch(ctx, samples, episodes); err != nil {
		t.Fatalf("replay PersistObservabilityBatch: %v", err)
	}
	if err := st.RecomputeProviderQueueMinutes(ctx, start, start.Add(time.Minute)); err != nil {
		t.Fatalf("RecomputeProviderQueueMinutes: %v", err)
	}
	if err := st.RecomputeProviderQueueMinutes(ctx, start, start.Add(time.Minute)); err != nil {
		t.Fatalf("recompute replay: %v", err)
	}

	rows, err := st.ListProviderMinuteMetrics(ctx, "obs-provider", start)
	if err != nil {
		t.Fatalf("ListProviderMinuteMetrics: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rollup rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.SchedulerSamples != 2 || row.DueAvg != 3 || row.DueMax != 4 ||
		row.DeferredAvg != 2 || row.DeferredMax != 3 || row.DispatchAttempts != 3 ||
		row.DeferralCooldown != 1 || row.DeferralSpacing != 1 ||
		row.DeferralConcurrency != 0 || row.ProviderChecks != 4 {
		t.Fatalf("rollup row = %+v", row)
	}

	// A late sample corrects the same minute without duplicating prior samples.
	late := []store.ProviderQueueSample{{
		ID: "sample-3", ProviderID: "obs-provider", SampledAt: start.Add(45 * time.Second),
		MinuteStart: start, Due: 6, Queued: 6, Deferred: 5, DeferredBySpacing: 5,
		Dispatched: 1, CreatedAt: start.Add(45 * time.Second),
	}}
	if err := st.PersistObservabilityBatch(ctx, late, nil); err != nil {
		t.Fatalf("late sample persist: %v", err)
	}
	if err := st.RecomputeProviderQueueMinutes(ctx, start, start.Add(time.Minute)); err != nil {
		t.Fatalf("late sample recompute: %v", err)
	}
	rows, err = st.ListProviderMinuteMetrics(ctx, "obs-provider", start)
	if err != nil {
		t.Fatalf("ListProviderMinuteMetrics after late sample: %v", err)
	}
	if rows[0].SchedulerSamples != 3 || rows[0].DueAvg != 4 || rows[0].DueMax != 6 ||
		rows[0].DispatchAttempts != 4 {
		t.Fatalf("late-sample rollup = %+v", rows[0])
	}

	totals, err := st.SumProviderTraffic(ctx, start)
	if err != nil {
		t.Fatalf("SumProviderTraffic: %v", err)
	}
	total := totals["obs-provider"]
	if total.Checks != 4 || total.Successes != 1 || total.Failures != 1 ||
		total.RateLimited != 1 || total.Interrupted != 1 || total.Discarded != 1 ||
		total.DispatchAttempts != 4 {
		t.Fatalf("totals = %+v", total)
	}

	if err := st.InsertProviderRateLimitEvent(ctx, store.ProviderRateLimitEvent{
		ProviderID: "obs-provider", Scope: "provider", OccurredAt: start.Add(time.Minute),
		Kind: store.RateLimitEventRateLimited, PreviousLevel: 1, EscalationLevel: 2,
		PreviousAdaptive: 10 * time.Minute, Adaptive: 20 * time.Minute,
		PreviousCooldownUntil: timePtr(start.Add(11 * time.Minute)),
		CooldownUntil:         timePtr(start.Add(21 * time.Minute)),
		RetryAfter:            15 * time.Minute, CreatedAt: start.Add(time.Minute),
	}); err != nil {
		t.Fatalf("InsertProviderRateLimitEvent: %v", err)
	}
	events, err := st.ListProviderRateLimitEvents(ctx, "obs-provider", start, 10)
	if err != nil {
		t.Fatalf("ListProviderRateLimitEvents: %v", err)
	}
	if len(events) != 1 || events[0].Kind != store.RateLimitEventRateLimited ||
		events[0].PreviousLevel != 1 || events[0].EscalationLevel != 2 ||
		events[0].Adaptive != 20*time.Minute || events[0].PreviousCooldownUntil == nil {
		t.Fatalf("events = %+v", events)
	}

	incomplete, err := st.CountIncompleteChecksByProvider(ctx, start)
	if err != nil {
		t.Fatalf("CountIncompleteChecksByProvider: %v", err)
	}
	if incomplete["obs-provider"] != 1 {
		t.Fatalf("incomplete = %v, want 1", incomplete)
	}
	activity, err := st.ListProviderCheckActivity(ctx, "obs-provider", start, 10)
	if err != nil {
		t.Fatalf("ListProviderCheckActivity: %v", err)
	}
	if len(activity) != 5 {
		t.Fatalf("activity = %d, want 5", len(activity))
	}
	foundIncomplete := false
	for _, item := range activity {
		if item.Incomplete {
			foundIncomplete = true
			if item.Outcome != "abandoned" || item.DurationMS != nil {
				t.Fatalf("incomplete activity = %+v", item)
			}
		}
	}
	if !foundIncomplete {
		t.Fatal("crash-abandoned check not surfaced as incomplete")
	}
}

func TestObservabilityRollupSurvivesProviderDeletion(t *testing.T) {
	st, db := newTestStore(t)
	ctx := context.Background()
	targetID := insertObservabilityTarget(t, db, "delete-monitor", "delete-provider")
	start := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	insertObservabilityCheck(t, db, targetID, start.Add(time.Second), "success", "", 100, 10, true)
	minutes, err := st.AggregateProviderTraffic(ctx, start, start.Add(time.Minute))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if err := st.UpsertProviderTrafficMinutes(ctx, minutes, start.Add(time.Minute)); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM monitors WHERE id = 'delete-monitor'`); err != nil {
		t.Fatalf("delete monitor: %v", err)
	}
	rows, err := st.ListProviderMinuteMetrics(ctx, "delete-provider", start)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(rows) != 1 || rows[0].ProviderChecks != 1 {
		t.Fatalf("rollup after deletion = %+v", rows)
	}
	var checks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM check_runs`).Scan(&checks); err != nil {
		t.Fatalf("count check runs: %v", err)
	}
	if checks != 0 {
		t.Fatalf("check runs after cascade = %d, want 0", checks)
	}
	// Recomputing the minute from the now-empty source does not remove the
	// already-durable rollup; it simply has nothing new to write.
	minutes, err = st.AggregateProviderTraffic(ctx, start, start.Add(time.Minute))
	if err != nil {
		t.Fatalf("re-aggregate after delete: %v", err)
	}
	if len(minutes) != 0 {
		t.Fatalf("re-aggregated rows = %d, want 0", len(minutes))
	}
	if err := st.UpsertProviderTrafficMinutes(ctx, minutes, start.Add(time.Minute)); err != nil {
		t.Fatalf("upsert empty: %v", err)
	}
	rows, err = st.ListProviderMinuteMetrics(ctx, "delete-provider", start)
	if err != nil {
		t.Fatalf("list after empty recompute: %v", err)
	}
	if len(rows) != 1 || rows[0].ProviderChecks != 1 {
		t.Fatalf("durable rollup changed after source deletion: %+v", rows)
	}
}

func TestObservabilityScheduleSummariesAndTargets(t *testing.T) {
	st, db := newTestStore(t)
	ctx := context.Background()
	insertObservabilityTarget(t, db, "summary-monitor", "summary-provider")
	summaries, err := st.ProviderScheduleSummaries(ctx, testNow)
	if err != nil {
		t.Fatalf("ProviderScheduleSummaries: %v", err)
	}
	summary := summaries["summary-provider"]
	if summary.ActiveTargets != 1 || summary.EarliestNextDueAt == nil {
		t.Fatalf("summary = %+v", summary)
	}
	targets, err := st.ListProviderActiveTargets(ctx, "summary-provider", testNow, 10)
	if err != nil {
		t.Fatalf("ListProviderActiveTargets: %v", err)
	}
	if len(targets) != 1 || targets[0].MonitorID != "summary-monitor" {
		t.Fatalf("targets = %+v", targets)
	}
}

package scheduler_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/provider/fake"
	"github.com/am-fallah/neda/internal/scheduler"
	"github.com/am-fallah/neda/internal/store"
	"github.com/google/uuid"
)

type outcomeHarness struct {
	st     *store.Store
	sqlDB  *sql.DB
	runner *scheduler.OutcomeRunner
	target scheduler.Target
	wakes  *atomic.Int32
}

func newOutcomeHarness(
	t *testing.T,
	id string,
	adapter provider.Provider,
	spec checkMonitorSpec,
	timeout time.Duration,
	deliveries scheduler.DeliverySource,
) *outcomeHarness {
	t.Helper()
	st, sqlDB := newSchedulerStore(t)
	if err := st.EnsurePollInterval(context.Background(), schedulerNow); err != nil {
		t.Fatalf("EnsurePollInterval: %v", err)
	}
	if spec.providerID == "" {
		spec.providerID = adapter.Metadata().ID
	}
	monitor := createCheckMonitor(t, st, id, spec)
	target := checkTargetFor(t, monitor, spec.providerID)
	wakes := &atomic.Int32{}
	stage, err := scheduler.NewCheckStage(scheduler.CheckStageConfig{
		Repository: st,
		Providers:  checkRegistry(t, adapter),
		Now:        func() time.Time { return schedulerNow },
		Timeout:    timeout,
		Logger:     discardLogger(),
	})
	if err != nil {
		t.Fatalf("NewCheckStage: %v", err)
	}
	runner, err := scheduler.NewOutcomeRunner(scheduler.OutcomeRunnerConfig{
		Stage:      stage,
		Store:      st,
		Deliveries: deliveries,
		Wake:       func() { wakes.Add(1) },
		Now:        func() time.Time { return schedulerNow },
		Random:     midpointRandom{},
		Logger:     discardLogger(),
	})
	if err != nil {
		t.Fatalf("NewOutcomeRunner: %v", err)
	}
	return &outcomeHarness{st: st, sqlDB: sqlDB, runner: runner, target: target, wakes: wakes}
}

func telegramDeliveries(context.Context, *sql.Tx) ([]notify.Delivery, error) {
	return []notify.Delivery{
		{Channel: notify.ChannelTelegram, TargetKey: "telegram"},
		{Channel: notify.ChannelPush, TargetKey: "push:subscription-1"},
	}, nil
}

func runOutcome(t *testing.T, harness *outcomeHarness) scheduler.Result {
	t.Helper()
	result, err := harness.runner.Run(context.Background(), harness.target)
	if err != nil {
		t.Fatalf("OutcomeRunner.Run: %v", err)
	}
	return result
}

func assertNoTargetOutcomeWrites(t *testing.T, sqlDB *sql.DB, targetID int64) {
	t.Helper()
	var nextDue, lastAttempt, lastSuccess, lastOutcome, lastErrorKind, lastErrorAt sql.NullString
	var failures int
	if err := sqlDB.QueryRow(`SELECT next_due_at, last_attempt_at, last_success_at, last_outcome,
		last_error_kind, last_error_at, consecutive_failures FROM monitor_providers WHERE id = ?`, targetID).Scan(
		&nextDue, &lastAttempt, &lastSuccess, &lastOutcome, &lastErrorKind, &lastErrorAt, &failures,
	); err != nil {
		t.Fatalf("query target outcome fields: %v", err)
	}
	if nextDue.Valid || lastAttempt.Valid || lastSuccess.Valid || lastOutcome.Valid ||
		lastErrorKind.Valid || lastErrorAt.Valid || failures != 0 {
		t.Fatalf("outcome changed target fields: %+v/%+v/%+v/%+v/%+v/%+v/%d",
			nextDue, lastAttempt, lastSuccess, lastOutcome, lastErrorKind, lastErrorAt, failures)
	}
}

func checkRunSnapshot(t *testing.T, sqlDB *sql.DB) (outcome string, errorKind sql.NullString, finished sql.NullString, duration sql.NullInt64, journeys, offerings, reservable, notReservable, unknown, warnings int) {
	t.Helper()
	if err := sqlDB.QueryRow(`SELECT outcome, error_kind, finished_at, duration_ms,
		journey_count, offering_count, reservable_count, not_reservable_count, unknown_count, warning_count
		FROM check_runs ORDER BY id DESC LIMIT 1`).Scan(
		&outcome, &errorKind, &finished, &duration, &journeys, &offerings,
		&reservable, &notReservable, &unknown, &warnings,
	); err != nil {
		t.Fatalf("query check run: %v", err)
	}
	return outcome, errorKind, finished, duration, journeys, offerings, reservable, notReservable, unknown, warnings
}

func countRows(t *testing.T, sqlDB *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	return count
}

func TestOutcomeRunnerSuccessCommitsAvailabilityHealthAndSchedule(t *testing.T) {
	harness := newOutcomeHarness(t, "outcome-success", fake.NewWithClock(func() time.Time { return schedulerNow }), checkMonitorSpec{
		providerID: "fake",
		originID:   fake.OriginAvailable,
	}, time.Second, telegramDeliveries)

	result := runOutcome(t, harness)
	if !result.Settled {
		t.Fatal("success outcome was not settled")
	}
	outcome, errorKind, finished, duration, journeys, offerings, reservable, notReservable, unknown, warnings := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "success" || errorKind.Valid {
		t.Fatalf("check outcome = %q error_kind=%+v", outcome, errorKind)
	}
	if !finished.Valid || !duration.Valid || duration.Int64 < 0 {
		t.Fatalf("finished/duration = %+v/%+v", finished, duration)
	}
	if journeys != 1 || offerings != 1 || reservable != 1 || notReservable != 0 || unknown != 0 || warnings != 0 {
		t.Fatalf("counts = %d/%d/%d/%d/%d/%d", journeys, offerings, reservable, notReservable, unknown, warnings)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM inventory_states WHERE state = 'reservable'`); count != 1 {
		t.Fatalf("reservable states = %d, want 1", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM availability_episodes WHERE closed_at IS NULL`); count != 1 {
		t.Fatalf("open episodes = %d, want 1", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events WHERE kind = 'availability'`); count != 1 {
		t.Fatalf("availability events = %d, want 1", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM notification_deliveries`); count != 2 {
		t.Fatalf("deliveries = %d, want 2", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(DISTINCT id) FROM notification_deliveries`); count != 2 {
		t.Fatalf("distinct delivery ids = %d, want 2", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM provider_health WHERE consecutive_failures = 0`); count != 1 {
		t.Fatalf("provider health success row = %d, want 1", count)
	}
	monitor, err := harness.st.GetMonitor(context.Background(), harness.target.MonitorID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	target := monitor.Providers[0]
	if target.LastOutcome != "success" || target.ConsecutiveFailures != 0 ||
		target.NextDueAt == nil || !target.NextDueAt.Equal(schedulerNow.Add(300*time.Second)) {
		t.Fatalf("target after success = %+v", target)
	}
	if harness.wakes.Load() != 1 {
		t.Fatalf("wake count = %d, want 1", harness.wakes.Load())
	}
}

func TestOutcomeRunnerMultipleAvailabilityEventsUseUniqueDeliveryIDs(t *testing.T) {
	adapter := newRecordingProvider("multiple", nil)
	adapter.metadata = provider.Metadata{ID: "multiple", DisplayName: "Multiple", TestOnly: true}
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return provider.Result{
			ObservedAt: schedulerNow,
			Journeys: []provider.JourneyObservation{{
				Journey: provider.Journey{Key: "j1"},
				Offerings: []provider.OfferingObservation{
					{Offering: provider.Offering{Key: "o1"}, State: provider.StateReservable},
					{Offering: provider.Offering{Key: "o2"}, State: provider.StateReservable},
				},
			}},
		}, nil
	}
	harness := newOutcomeHarness(t, "outcome-multiple", adapter, checkMonitorSpec{
		providerID: "multiple",
	}, time.Second, telegramDeliveries)
	result := runOutcome(t, harness)
	if !result.Settled {
		t.Fatal("multiple-event success was not settled")
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events WHERE kind = 'availability'`); count != 2 {
		t.Fatalf("availability events = %d, want 2", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM notification_deliveries`); count != 4 {
		t.Fatalf("deliveries = %d, want 4", count)
	}
	rows, err := harness.sqlDB.Query(`SELECT id FROM notification_deliveries`)
	if err != nil {
		t.Fatalf("query delivery ids: %v", err)
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan delivery id: %v", err)
		}
		if _, err := uuid.Parse(id); err != nil {
			t.Fatalf("delivery id %q is not a UUID: %v", id, err)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate delivery id %q", id)
		}
		seen[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate delivery ids: %v", err)
	}
	if harness.wakes.Load() != 1 {
		t.Fatalf("wake count = %d, want 1", harness.wakes.Load())
	}
}

func TestOutcomeRunnerSuccessWithoutDeliverySourcePersistsEventWithoutOutbox(t *testing.T) {
	harness := newOutcomeHarness(t, "outcome-no-destinations", fake.NewWithClock(func() time.Time { return schedulerNow }), checkMonitorSpec{
		providerID: "fake",
		originID:   fake.OriginAvailable,
	}, time.Second, nil)
	result := runOutcome(t, harness)
	if !result.Settled {
		t.Fatal("success outcome was not settled")
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events WHERE kind = 'availability'`); count != 1 {
		t.Fatalf("availability events = %d, want 1", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM notification_deliveries`); count != 0 {
		t.Fatalf("deliveries = %d, want 0 without a delivery source", count)
	}
	if harness.wakes.Load() != 0 {
		t.Fatalf("wake count = %d, want 0 without outbox rows", harness.wakes.Load())
	}
}

func TestOutcomeRunnerJourneyWithoutOfferingsIsSuccess(t *testing.T) {
	adapter := newRecordingProvider("no-offerings", nil)
	adapter.metadata = provider.Metadata{ID: "no-offerings", DisplayName: "No offerings", TestOnly: true}
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return provider.Result{
			ObservedAt: schedulerNow,
			Journeys:   []provider.JourneyObservation{{Journey: provider.Journey{Key: "j1"}}},
		}, nil
	}
	harness := newOutcomeHarness(t, "outcome-no-offerings", adapter, checkMonitorSpec{
		providerID: "no-offerings",
	}, time.Second, nil)
	result := runOutcome(t, harness)
	if !result.Settled {
		t.Fatal("journey-without-offerings outcome was not settled")
	}
	outcome, _, _, _, journeys, offerings, _, _, _, _ := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "success" || journeys != 1 || offerings != 0 {
		t.Fatalf("check outcome/counts = %q/%d/%d, want success/1/0", outcome, journeys, offerings)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM inventory_states`); count != 0 {
		t.Fatalf("inventory states = %d, want 0", count)
	}
}

func TestOutcomeRunnerValidEmpty(t *testing.T) {
	harness := newOutcomeHarness(t, "outcome-empty", fake.NewWithClock(func() time.Time { return schedulerNow }), checkMonitorSpec{
		providerID: "fake",
		originID:   fake.OriginEmpty,
	}, time.Second, nil)

	result := runOutcome(t, harness)
	if !result.Settled {
		t.Fatal("empty outcome was not settled")
	}
	outcome, errorKind, _, _, journeys, offerings, reservable, notReservable, unknown, warnings := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "empty" || errorKind.Valid {
		t.Fatalf("check outcome = %q error_kind=%+v", outcome, errorKind)
	}
	if journeys != 0 || offerings != 0 || reservable != 0 || notReservable != 0 || unknown != 0 || warnings != 0 {
		t.Fatalf("empty counts = %d/%d/%d/%d/%d/%d", journeys, offerings, reservable, notReservable, unknown, warnings)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM inventory_states`); count != 0 {
		t.Fatalf("inventory states = %d, want 0", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM availability_episodes`); count != 0 {
		t.Fatalf("episodes = %d, want 0", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("events = %d, want 0", count)
	}
	monitor, err := harness.st.GetMonitor(context.Background(), harness.target.MonitorID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if monitor.Providers[0].LastOutcome != "empty" || monitor.Providers[0].ConsecutiveFailures != 0 {
		t.Fatalf("target after empty = %+v", monitor.Providers[0])
	}
	if harness.wakes.Load() != 0 {
		t.Fatalf("wake count = %d, want 0", harness.wakes.Load())
	}
}

func TestOutcomeRunnerInfrastructureErrorAppliesBackoffAndHealth(t *testing.T) {
	harness := newOutcomeHarness(t, "outcome-infra", fake.NewWithClock(func() time.Time { return schedulerNow }), checkMonitorSpec{
		providerID: "fake",
		originID:   fake.OriginTimeout,
	}, time.Second, telegramDeliveries)

	result := runOutcome(t, harness)
	if !result.Settled {
		t.Fatal("provider error outcome was not settled")
	}
	outcome, errorKind, _, duration, journeys, offerings, reservable, notReservable, unknown, warnings := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "error" || !errorKind.Valid || errorKind.String != "timeout" {
		t.Fatalf("check outcome = %q error_kind=%+v", outcome, errorKind)
	}
	if !duration.Valid || journeys != 0 || offerings != 0 || reservable != 0 || notReservable != 0 || unknown != 0 || warnings != 0 {
		t.Fatalf("error counts = %+v/%d/%d/%d/%d/%d/%d", duration, journeys, offerings, reservable, notReservable, unknown, warnings)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM inventory_states`); count != 0 {
		t.Fatalf("inventory states = %d, want 0", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("events = %d, want 0", count)
	}
	var providerFailures int
	if err := harness.sqlDB.QueryRow(`SELECT consecutive_failures FROM provider_health WHERE provider_id = 'fake'`).Scan(&providerFailures); err != nil {
		t.Fatalf("provider health: %v", err)
	}
	if providerFailures != 1 {
		t.Fatalf("provider failures = %d, want 1", providerFailures)
	}
	monitor, err := harness.st.GetMonitor(context.Background(), harness.target.MonitorID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	target := monitor.Providers[0]
	if target.LastOutcome != "error" || target.ConsecutiveFailures != 1 ||
		target.NextDueAt == nil || !target.NextDueAt.Equal(schedulerNow.Add(10*time.Minute)) {
		t.Fatalf("target after infrastructure error = %+v", target)
	}
	if harness.wakes.Load() != 0 {
		t.Fatalf("wake count = %d, want 0", harness.wakes.Load())
	}
}

func TestOutcomeRunnerNonCountingErrorUpdatesDiagnosticsOnly(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		errorKind  string
		statusCode int
	}{
		{name: "route", origin: fake.OriginRouteNotFound, errorKind: "route_not_found"},
		{name: "http 404", origin: fake.OriginHTTP404, errorKind: "http_status", statusCode: 404},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newOutcomeHarness(t, "outcome-noncount-"+test.name, fake.NewWithClock(func() time.Time { return schedulerNow }), checkMonitorSpec{
				providerID: "fake",
				originID:   test.origin,
			}, time.Second, telegramDeliveries)
			result := runOutcome(t, harness)
			if !result.Settled {
				t.Fatal("non-counting error was not settled")
			}
			outcome, errorKind, _, _, _, _, _, _, _, _ := checkRunSnapshot(t, harness.sqlDB)
			if outcome != "error" || !errorKind.Valid || errorKind.String != test.errorKind {
				t.Fatalf("check outcome = %q error_kind=%+v", outcome, errorKind)
			}
			var providerFailures int
			var lastErrorKind sql.NullString
			if err := harness.sqlDB.QueryRow(`SELECT consecutive_failures, last_error_kind FROM provider_health WHERE provider_id = 'fake'`).Scan(&providerFailures, &lastErrorKind); err != nil {
				t.Fatalf("provider health: %v", err)
			}
			if providerFailures != 0 || !lastErrorKind.Valid {
				t.Fatalf("provider diagnostics = %d/%+v", providerFailures, lastErrorKind)
			}
			if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events`); count != 0 {
				t.Fatalf("events = %d, want 0", count)
			}
			monitor, err := harness.st.GetMonitor(context.Background(), harness.target.MonitorID)
			if err != nil {
				t.Fatalf("GetMonitor: %v", err)
			}
			target := monitor.Providers[0]
			if target.LastOutcome != "error" || target.ConsecutiveFailures != 0 ||
				target.NextDueAt == nil || !target.NextDueAt.Equal(schedulerNow.Add(300*time.Second)) {
				t.Fatalf("target after non-counting error = %+v", target)
			}
			if harness.wakes.Load() != 0 {
				t.Fatalf("wake count = %d, want 0", harness.wakes.Load())
			}
		})
	}
}

func TestOutcomeRunnerConfigurationErrorUpdatesDiagnosticsOnly(t *testing.T) {
	adapter := newRecordingProvider("config", nil)
	adapter.metadata = provider.Metadata{ID: "config", DisplayName: "Config", TestOnly: true}
	adapter.validateErr = &provider.Error{Kind: provider.ErrConfiguration, Op: "ValidateConfig"}
	harness := newOutcomeHarness(t, "outcome-config", adapter, checkMonitorSpec{
		providerID: "config",
	}, time.Second, telegramDeliveries)

	result := runOutcome(t, harness)
	if !result.Settled {
		t.Fatal("configuration error was not settled")
	}
	outcome, errorKind, _, _, _, _, _, _, _, _ := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "error" || !errorKind.Valid || errorKind.String != "configuration" {
		t.Fatalf("check outcome = %q error_kind=%+v", outcome, errorKind)
	}
	var providerFailures int
	var lastErrorKind sql.NullString
	if err := harness.sqlDB.QueryRow(`SELECT consecutive_failures, last_error_kind FROM provider_health WHERE provider_id = 'config'`).Scan(&providerFailures, &lastErrorKind); err != nil {
		t.Fatalf("provider health: %v", err)
	}
	if providerFailures != 0 || !lastErrorKind.Valid || lastErrorKind.String != "configuration" {
		t.Fatalf("provider diagnostics = %d/%+v", providerFailures, lastErrorKind)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM provider_health_episodes`); count != 0 {
		t.Fatalf("health episodes = %d, want 0", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("events = %d, want 0", count)
	}
	monitor, err := harness.st.GetMonitor(context.Background(), harness.target.MonitorID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	target := monitor.Providers[0]
	if target.LastOutcome != "error" || target.ConsecutiveFailures != 0 ||
		target.NextDueAt == nil || !target.NextDueAt.Equal(schedulerNow.Add(300*time.Second)) {
		t.Fatalf("target after configuration error = %+v", target)
	}
}

func TestOutcomeRunnerProviderSideCancellationIsFailure(t *testing.T) {
	harness := newOutcomeHarness(t, "outcome-provider-cancel", fake.NewWithClock(func() time.Time { return schedulerNow }), checkMonitorSpec{
		providerID: "fake",
		originID:   fake.OriginProviderCanceled,
	}, time.Second, nil)
	result := runOutcome(t, harness)
	if !result.Settled {
		t.Fatal("provider-side cancellation was not settled")
	}
	outcome, errorKind, _, _, _, _, _, _, _, _ := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "error" || !errorKind.Valid || errorKind.String != "invalid_response" {
		t.Fatalf("check outcome = %q error_kind=%+v", outcome, errorKind)
	}
	var failures int
	if err := harness.sqlDB.QueryRow(`SELECT consecutive_failures FROM provider_health WHERE provider_id = 'fake'`).Scan(&failures); err != nil {
		t.Fatalf("provider health: %v", err)
	}
	if failures != 1 {
		t.Fatalf("provider failures = %d, want 1", failures)
	}
}

func TestOutcomeRunnerDeletedTargetLeavesNoDiagnosticRow(t *testing.T) {
	adapter := newRecordingProvider("deleted", nil)
	adapter.metadata = provider.Metadata{ID: "deleted", DisplayName: "Deleted", TestOnly: true}
	harness := newOutcomeHarness(t, "outcome-deleted", adapter, checkMonitorSpec{
		providerID: "deleted",
	}, time.Second, telegramDeliveries)
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		if _, err := harness.sqlDB.Exec(`DELETE FROM monitor_providers WHERE id = ?`, harness.target.ID); err != nil {
			t.Fatalf("delete target: %v", err)
		}
		return provider.Result{ObservedAt: schedulerNow}, nil
	}
	result := runOutcome(t, harness)
	if result.Settled {
		t.Fatal("deleted target discard was settled")
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM check_runs`); count != 0 {
		t.Fatalf("check runs = %d, want 0 after cascade", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM inventory_states`); count != 0 {
		t.Fatalf("inventory states = %d, want 0", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM provider_health`); count != 0 {
		t.Fatalf("provider health rows = %d, want 0", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("events = %d, want 0", count)
	}
}

func TestOutcomeRunnerMalformedResultBecomesInvalidResponse(t *testing.T) {
	adapter := newRecordingProvider("malformed", nil)
	adapter.metadata = provider.Metadata{ID: "malformed", DisplayName: "Malformed", TestOnly: true}
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return provider.Result{}, nil
	}
	harness := newOutcomeHarness(t, "outcome-malformed", adapter, checkMonitorSpec{
		providerID: "malformed",
	}, time.Second, nil)

	result := runOutcome(t, harness)
	if !result.Settled {
		t.Fatal("malformed result was not settled")
	}
	outcome, errorKind, _, _, _, _, _, _, _, _ := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "error" || !errorKind.Valid || errorKind.String != "invalid_response" {
		t.Fatalf("check outcome = %q error_kind=%+v", outcome, errorKind)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM inventory_states`); count != 0 {
		t.Fatalf("inventory states = %d, want 0", count)
	}
	var failures int
	if err := harness.sqlDB.QueryRow(`SELECT consecutive_failures FROM provider_health WHERE provider_id = 'malformed'`).Scan(&failures); err != nil {
		t.Fatalf("provider health: %v", err)
	}
	if failures != 1 {
		t.Fatalf("provider failures = %d, want 1", failures)
	}
}

func TestOutcomeRunnerDiscardedMatrix(t *testing.T) {
	type mutate func(t *testing.T, harness *outcomeHarness)
	tests := []struct {
		name   string
		reason string
		mutate mutate
	}{
		{
			name:   "target inactive",
			reason: "target_inactive",
			mutate: func(t *testing.T, h *outcomeHarness) {
				if _, err := h.sqlDB.Exec(`UPDATE monitor_providers SET active = 0 WHERE id = ?`, h.target.ID); err != nil {
					t.Fatalf("deactivate target: %v", err)
				}
			},
		},
		{
			name:   "monitor disabled",
			reason: "monitor_disabled",
			mutate: func(t *testing.T, h *outcomeHarness) {
				if _, err := h.sqlDB.Exec(`UPDATE monitors SET enabled = 0 WHERE id = ?`, h.target.MonitorID); err != nil {
					t.Fatalf("disable monitor: %v", err)
				}
			},
		},
		{
			name:   "monitor expired",
			reason: "monitor_expired",
			mutate: func(t *testing.T, h *outcomeHarness) {
				if _, err := h.sqlDB.Exec(`UPDATE monitors SET expires_at = ? WHERE id = ?`,
					schedulerNow.Add(-time.Minute).Format(time.RFC3339Nano), h.target.MonitorID); err != nil {
					t.Fatalf("expire monitor: %v", err)
				}
			},
		},
		{
			name:   "config changed",
			reason: "config_changed",
			mutate: func(t *testing.T, h *outcomeHarness) {
				if _, err := h.sqlDB.Exec(`UPDATE monitor_providers SET config_version = config_version + 1 WHERE id = ?`, h.target.ID); err != nil {
					t.Fatalf("change config version: %v", err)
				}
			},
		},
		{
			name:   "booking changed",
			reason: "booking_config_changed",
			mutate: func(t *testing.T, h *outcomeHarness) {
				if _, err := h.sqlDB.Exec(`UPDATE monitor_providers SET booking_slug = 'new-slug' WHERE id = ?`, h.target.ID); err != nil {
					t.Fatalf("change booking slug: %v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := newRecordingProvider("discard", nil)
			adapter.metadata = provider.Metadata{ID: "discard", DisplayName: "Discard", TestOnly: true}
			adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
				return provider.Result{
					ObservedAt: schedulerNow,
					Journeys: []provider.JourneyObservation{{
						Journey: provider.Journey{Key: "j1"},
						Offerings: []provider.OfferingObservation{{
							Offering: provider.Offering{Key: "o1"},
							State:    provider.StateReservable,
						}},
					}},
				}, nil
			}
			harness := newOutcomeHarness(t, "outcome-discard-"+test.name, adapter, checkMonitorSpec{
				providerID: "discard",
			}, time.Second, telegramDeliveries)
			// Mutate after the check row and I/O have been observed. The
			// provider hook runs after SCH-004 commits the running row.
			adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
				test.mutate(t, harness)
				return provider.Result{
					ObservedAt: schedulerNow,
					Journeys: []provider.JourneyObservation{{
						Journey: provider.Journey{Key: "j1"},
						Offerings: []provider.OfferingObservation{{
							Offering: provider.Offering{Key: "o1"},
							State:    provider.StateReservable,
						}},
					}},
				}, nil
			}
			result := runOutcome(t, harness)
			if result.Settled {
				t.Fatal("discarded outcome was settled")
			}
			outcome, errorKind, _, _, _, _, _, _, _, _ := checkRunSnapshot(t, harness.sqlDB)
			if outcome != "discarded" || !errorKind.Valid || errorKind.String != test.reason {
				t.Fatalf("check outcome = %q error_kind=%+v", outcome, errorKind)
			}
			if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM inventory_states`); count != 0 {
				t.Fatalf("inventory states = %d, want 0", count)
			}
			if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM availability_episodes`); count != 0 {
				t.Fatalf("episodes = %d, want 0", count)
			}
			if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM provider_health`); count != 0 {
				t.Fatalf("provider health rows = %d, want 0", count)
			}
			if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events`); count != 0 {
				t.Fatalf("events = %d, want 0", count)
			}
			if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM notification_deliveries`); count != 0 {
				t.Fatalf("deliveries = %d, want 0", count)
			}
			assertNoTargetOutcomeWrites(t, harness.sqlDB, harness.target.ID)
			if harness.wakes.Load() != 0 {
				t.Fatalf("wake count = %d, want 0", harness.wakes.Load())
			}
		})
	}
}

func TestOutcomeRunnerAbandonedOnCallerCancellation(t *testing.T) {
	adapter := newRecordingProvider("cancel", nil)
	adapter.metadata = provider.Metadata{ID: "cancel", DisplayName: "Cancel", TestOnly: true}
	harness := newOutcomeHarness(t, "outcome-cancel", adapter, checkMonitorSpec{
		providerID: "cancel",
	}, time.Second, telegramDeliveries)
	ctx, cancel := context.WithCancel(context.Background())
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		cancel()
		return provider.Result{
			ObservedAt: schedulerNow,
			Journeys: []provider.JourneyObservation{{
				Journey: provider.Journey{Key: "j1"},
				Offerings: []provider.OfferingObservation{{
					Offering: provider.Offering{Key: "o1"},
					State:    provider.StateReservable,
				}},
			}},
		}, nil
	}
	result, err := harness.runner.Run(ctx, harness.target)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Settled {
		t.Fatal("abandoned outcome was not settled")
	}
	outcome, errorKind, _, _, _, _, _, _, _, _ := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "abandoned" || !errorKind.Valid || errorKind.String != "canceled" {
		t.Fatalf("check outcome = %q error_kind=%+v", outcome, errorKind)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM inventory_states`); count != 0 {
		t.Fatalf("inventory states = %d, want 0", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM provider_health`); count != 0 {
		t.Fatalf("provider health rows = %d, want 0", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM availability_episodes`); count != 0 {
		t.Fatalf("episodes = %d, want 0", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("events = %d, want 0", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM notification_deliveries`); count != 0 {
		t.Fatalf("deliveries = %d, want 0", count)
	}
	monitor, err := harness.st.GetMonitor(context.Background(), harness.target.MonitorID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	target := monitor.Providers[0]
	if target.NextDueAt != nil || target.LastAttemptAt != nil || target.LastOutcome != "" || target.ConsecutiveFailures != 0 {
		t.Fatalf("abandoned outcome changed target: %+v", target)
	}
	if harness.wakes.Load() != 0 {
		t.Fatalf("wake count = %d, want 0", harness.wakes.Load())
	}
}

func TestOutcomeRunnerTargetInvalidityPrecedesCancellation(t *testing.T) {
	adapter := newRecordingProvider("precedence", nil)
	adapter.metadata = provider.Metadata{ID: "precedence", DisplayName: "Precedence", TestOnly: true}
	harness := newOutcomeHarness(t, "outcome-precedence", adapter, checkMonitorSpec{
		providerID: "precedence",
	}, time.Second, nil)
	ctx, cancel := context.WithCancel(context.Background())
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		if _, err := harness.sqlDB.Exec(`UPDATE monitor_providers SET active = 0 WHERE id = ?`, harness.target.ID); err != nil {
			t.Fatalf("deactivate target: %v", err)
		}
		cancel()
		return provider.Result{ObservedAt: schedulerNow}, nil
	}
	result, err := harness.runner.Run(ctx, harness.target)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Settled {
		t.Fatal("discarded precedence outcome was settled")
	}
	outcome, errorKind, _, _, _, _, _, _, _, _ := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "discarded" || !errorKind.Valid || errorKind.String != "target_inactive" {
		t.Fatalf("check outcome = %q error_kind=%+v", outcome, errorKind)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM provider_health`); count != 0 {
		t.Fatalf("provider health rows = %d, want 0", count)
	}
}

func TestOutcomeRunnerSuccessReturnedAfterDeadlineIsTimeout(t *testing.T) {
	adapter := newRecordingProvider("late-success", nil)
	adapter.metadata = provider.Metadata{ID: "late-success", DisplayName: "Late success", TestOnly: true}
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		time.Sleep(50 * time.Millisecond)
		return provider.Result{ObservedAt: schedulerNow}, nil
	}
	harness := newOutcomeHarness(t, "outcome-late-success", adapter, checkMonitorSpec{
		providerID: "late-success",
	}, 10*time.Millisecond, nil)
	result := runOutcome(t, harness)
	if !result.Settled {
		t.Fatal("late success was not settled")
	}
	outcome, errorKind, _, _, _, _, _, _, _, _ := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "error" || !errorKind.Valid || errorKind.String != "timeout" {
		t.Fatalf("check outcome = %q error_kind=%+v, want error/timeout", outcome, errorKind)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM inventory_states`); count != 0 {
		t.Fatalf("inventory states = %d, want 0", count)
	}
}

func TestOutcomeRunnerDeadlineIsProviderFailure(t *testing.T) {
	adapter := newRecordingProvider("deadline", nil)
	adapter.metadata = provider.Metadata{ID: "deadline", DisplayName: "Deadline", TestOnly: true}
	adapter.checkFn = func(ctx context.Context, _ provider.CheckRequest) (provider.Result, error) {
		<-ctx.Done()
		return provider.Result{}, ctx.Err()
	}
	harness := newOutcomeHarness(t, "outcome-deadline", adapter, checkMonitorSpec{
		providerID: "deadline",
	}, 20*time.Millisecond, nil)
	result := runOutcome(t, harness)
	if !result.Settled {
		t.Fatal("deadline outcome was not settled")
	}
	outcome, errorKind, _, _, _, _, _, _, _, _ := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "error" || !errorKind.Valid || errorKind.String != "timeout" {
		t.Fatalf("check outcome = %q error_kind=%+v", outcome, errorKind)
	}
	var failures int
	if err := harness.sqlDB.QueryRow(`SELECT consecutive_failures FROM provider_health WHERE provider_id = 'deadline'`).Scan(&failures); err != nil {
		t.Fatalf("provider health: %v", err)
	}
	if failures != 1 {
		t.Fatalf("provider failures = %d, want 1", failures)
	}
}

func TestOutcomeRunnerOutcomeTransactionFailureSettlesAndLeavesRunningRow(t *testing.T) {
	harness := newOutcomeHarness(t, "outcome-tx-failure", fake.NewWithClock(func() time.Time { return schedulerNow }), checkMonitorSpec{
		providerID: "fake",
		originID:   fake.OriginAvailable,
	}, time.Second, func(context.Context, *sql.Tx) ([]notify.Delivery, error) {
		return nil, errors.New("injected delivery source failure")
	})
	result, err := harness.runner.Run(context.Background(), harness.target)
	if err == nil {
		t.Fatal("Run succeeded despite outcome transaction failure")
	}
	if !result.Settled {
		t.Fatal("outcome transaction failure did not settle")
	}
	if strings.Contains(err.Error(), "injected") {
		t.Fatalf("outcome failure leaked raw error detail: %q", err.Error())
	}
	outcome, errorKind, finished, _, _, _, _, _, _, _ := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "running" || finished.Valid {
		t.Fatalf("check row after rollback = %q/%+v, want running/NULL", outcome, finished)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM inventory_states`); count != 0 {
		t.Fatalf("inventory states = %d, want 0", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM provider_health`); count != 0 {
		t.Fatalf("provider health rows = %d, want 0", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("events = %d, want 0", count)
	}
	assertNoTargetOutcomeWrites(t, harness.sqlDB, harness.target.ID)
	recovered, err := harness.st.RecoverRunningChecks(context.Background(), schedulerNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("RecoverRunningChecks: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	outcome, errorKind, finished, _, _, _, _, _, _, _ = checkRunSnapshot(t, harness.sqlDB)
	if outcome != "abandoned" || !finished.Valid || (errorKind.Valid && errorKind.String != "") {
		t.Fatalf("startup recovery result = %q/%+v/%+v", outcome, finished, errorKind)
	}
}

func TestOutcomeRunnerHealthFailureRollsBackAvailabilityAndOutbox(t *testing.T) {
	harness := newOutcomeHarness(t, "outcome-health-failure", fake.NewWithClock(func() time.Time { return schedulerNow }), checkMonitorSpec{
		providerID: "fake",
		originID:   fake.OriginAvailable,
	}, time.Second, telegramDeliveries)
	if _, err := harness.sqlDB.Exec(`CREATE TRIGGER fail_health_insert
		BEFORE INSERT ON provider_health
		BEGIN
			SELECT RAISE(ABORT, 'injected health failure');
		END`); err != nil {
		t.Fatalf("create health failure trigger: %v", err)
	}

	result, err := harness.runner.Run(context.Background(), harness.target)
	if err == nil {
		t.Fatal("Run succeeded despite injected health failure")
	}
	if !result.Settled {
		t.Fatal("health failure did not settle")
	}
	outcome, errorKind, finished, _, _, _, _, _, _, _ := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "running" || finished.Valid {
		t.Fatalf("check row after rollback = %q/%+v/%+v, want running/NULL/NULL", outcome, errorKind, finished)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM inventory_states`,
		`SELECT COUNT(*) FROM availability_episodes`,
		`SELECT COUNT(*) FROM provider_health`,
		`SELECT COUNT(*) FROM events`,
		`SELECT COUNT(*) FROM notification_deliveries`,
	} {
		if count := countRows(t, harness.sqlDB, query); count != 0 {
			t.Fatalf("%s = %d, want 0", query, count)
		}
	}
	assertNoTargetOutcomeWrites(t, harness.sqlDB, harness.target.ID)
}

func TestOutcomeRunnerMixedAvailabilityAndHealthRecoveryDeliveryIDs(t *testing.T) {
	adapter := newRecordingProvider("mixed", nil)
	adapter.metadata = provider.Metadata{ID: "mixed", DisplayName: "Mixed", TestOnly: true}
	var calls int
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		calls++
		if calls <= 3 {
			return provider.Result{}, &provider.Error{Kind: provider.ErrTimeout, Op: "mixed.Check"}
		}
		return provider.Result{
			ObservedAt: schedulerNow,
			Journeys: []provider.JourneyObservation{{
				Journey: provider.Journey{Key: "j1"},
				Offerings: []provider.OfferingObservation{{
					Offering: provider.Offering{Key: "o1"},
					State:    provider.StateReservable,
				}},
			}},
		}, nil
	}
	harness := newOutcomeHarness(t, "outcome-mixed", adapter, checkMonitorSpec{
		providerID: "mixed",
	}, time.Second, telegramDeliveries)
	for i := 0; i < 3; i++ {
		runOutcome(t, harness)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events WHERE kind = 'provider_degraded'`); count != 1 {
		t.Fatalf("degraded events = %d, want 1", count)
	}
	result := runOutcome(t, harness)
	if !result.Settled {
		t.Fatal("mixed recovery outcome was not settled")
	}
	outcome, _, _, _, _, _, _, _, _, _ := checkRunSnapshot(t, harness.sqlDB)
	if outcome != "success" {
		t.Fatalf("check outcome = %q, want success", outcome)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events WHERE kind = 'availability'`); count != 1 {
		t.Fatalf("availability events = %d, want 1", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM events WHERE kind = 'provider_recovered'`); count != 1 {
		t.Fatalf("recovery events = %d, want 1", count)
	}
	if count := countRows(t, harness.sqlDB, `SELECT COUNT(*) FROM notification_deliveries`); count != 6 {
		t.Fatalf("deliveries = %d, want 6 (degraded + availability + recovery)", count)
	}
	rows, err := harness.sqlDB.Query(`SELECT id FROM notification_deliveries`)
	if err != nil {
		t.Fatalf("query delivery ids: %v", err)
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan delivery id: %v", err)
		}
		if _, err := uuid.Parse(id); err != nil {
			t.Fatalf("delivery id %q is not a UUID: %v", id, err)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate delivery id %q", id)
		}
		seen[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate delivery ids: %v", err)
	}
	if harness.wakes.Load() == 0 {
		t.Fatal("mixed outcome did not wake the delivery worker")
	}
}

func TestNewOutcomeRunnerValidatesDependencies(t *testing.T) {
	if _, err := scheduler.NewOutcomeRunner(scheduler.OutcomeRunnerConfig{}); !errors.Is(err, scheduler.ErrNilOutcomeStage) {
		t.Fatalf("missing stage error = %v, want ErrNilOutcomeStage", err)
	}
	stage := &scheduler.CheckStage{}
	if _, err := scheduler.NewOutcomeRunner(scheduler.OutcomeRunnerConfig{Stage: stage}); !errors.Is(err, scheduler.ErrNilOutcomeStore) {
		t.Fatalf("missing store error = %v, want ErrNilOutcomeStore", err)
	}
}

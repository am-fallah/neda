package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/observability"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/provider/fake"
	"github.com/am-fallah/neda/internal/store"
)

func TestObservabilityOverviewUsesLogicalChecksAndQueueSnapshot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "observability-api.db")
	database, err := db.Open(ctx, path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	applied, err := db.Migrate(ctx, database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if err := database.Ready(ctx, applied); err != nil {
		t.Fatalf("database.Ready: %v", err)
	}
	st := store.New(database.DB)

	now := time.Now().UTC().Truncate(time.Minute)
	expires := now.Add(24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := database.Exec(`INSERT INTO monitors
		(id, label, origin_name, destination_name, departure_date, adults, children, infants,
		 enabled, expires_at, created_at, updated_at)
		VALUES ('m1','Monitor','Origin','Destination','2026-12-15',1,0,0,1,?,?,?)`,
		expires, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert monitor: %v", err)
	}
	target, err := database.Exec(`INSERT INTO monitor_providers
		(monitor_id, provider_id, active, origin_id, destination_id, config_version,
		 next_due_at, consecutive_failures, created_at, updated_at)
		VALUES ('m1','fake',1,'origin','destination',1,?,0,?,?)`,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("insert target: %v", err)
	}
	targetID, _ := target.LastInsertId()
	started := now.Add(10 * time.Second)
	if _, err := database.Exec(`INSERT INTO check_runs (
		monitor_provider_id, config_version, started_at, finished_at, outcome,
		error_kind, http_status, duration_ms, journey_count, offering_count,
		reservable_count, not_reservable_count, unknown_count, warning_count, created_at,
		provider_request, provider_started_at, provider_duration_ms, provider_error_kind,
		queue_wait_ms, expected_spacing_ms, configured_interval_ms, adaptive_interval_ms
	) VALUES (?,1,?,?, 'success', NULL, NULL, 120, 0,0,0,0,0,0, ?, 1, ?, 120, NULL,
		1000, 5000, 300000, 0)`,
		targetID, started.Format(time.RFC3339Nano), started.Add(120*time.Millisecond).Format(time.RFC3339Nano),
		started.Format(time.RFC3339Nano), started.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert check: %v", err)
	}
	minutes, err := st.AggregateProviderTraffic(ctx, now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("AggregateProviderTraffic: %v", err)
	}
	if len(minutes) != 1 {
		t.Fatalf("minutes = %d, want 1", len(minutes))
	}
	if err := st.UpsertProviderTrafficMinutes(ctx, minutes, now); err != nil {
		t.Fatalf("UpsertProviderTrafficMinutes: %v", err)
	}

	registry, err := provider.NewRegistry(fake.New())
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	recorder := observability.NewRecorder()
	recorder.ObservePass(observability.PassObservation{
		Now: now, MonitoringEnabled: true, ConfiguredInterval: 5 * time.Minute,
		ProviderSpacing: 5 * time.Second, Workers: 2, RunningWorkers: 1,
		Providers: []observability.ProviderPassObservation{{
			ProviderID: "fake", Due: 2, InFlight: 1, Queued: 1, Deferred: 1,
			DeferredBySpacing: 1,
		}},
	})
	server := New(Config{
		Observability:     st,
		SchedulerSnapshot: recorder.Snapshot,
		Providers:         registry,
	}, nil, nil, applied)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/observability", nil)
	response := httptest.NewRecorder()
	server.handleObservability(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload observabilityResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.TrafficSummary.ProviderChecks != 1 || payload.TrafficSummary.Successes != 1 {
		t.Fatalf("traffic summary = %+v", payload.TrafficSummary)
	}
	if len(payload.Providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(payload.Providers))
	}
	row := payload.Providers[0]
	if row.ProviderChecks.Last24h != 1 || row.ProviderChecks.SuccessRate == nil {
		t.Fatalf("provider checks = %+v", row.ProviderChecks)
	}
	if row.Queue == nil || row.Queue.Queued != 1 || row.Queue.DeferredBySpacing != 1 {
		t.Fatalf("queue = %+v", row.Queue)
	}
}

func TestObservabilityUnavailableWithoutReadStore(t *testing.T) {
	server := New(Config{}, nil, nil, 0)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/observability", nil)
	response := httptest.NewRecorder()
	server.handleObservability(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
}

func TestObservabilityOverviewExplicitEmptyHistoryAndStoppedScheduler(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "empty-observability.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	applied, err := db.Migrate(ctx, database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if err := database.Ready(ctx, applied); err != nil {
		t.Fatalf("database.Ready: %v", err)
	}
	st := store.New(database.DB)
	registry, err := provider.NewRegistry(fake.New())
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	recorder := observability.NewRecorder()
	recorder.MarkStopped(time.Now().UTC())
	server := New(Config{
		Observability:     st,
		SchedulerSnapshot: recorder.Snapshot,
		Providers:         registry,
	}, nil, nil, applied)

	response := httptest.NewRecorder()
	server.handleObservability(response, httptest.NewRequest(http.MethodGet, "/api/v1/observability", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload observabilityResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.TrafficSummary.HistoryAvailable || payload.TrafficSummary.Recorded7d != 0 {
		t.Fatalf("empty history summary = %+v", payload.TrafficSummary)
	}
	if !payload.Scheduler.Stopped || payload.Scheduler.Running || payload.Scheduler.Stale {
		t.Fatalf("stopped scheduler = %+v", payload.Scheduler)
	}
	if len(payload.Providers) != 1 || payload.Providers[0].ProviderChecks.Recorded7d != 0 {
		t.Fatalf("providers = %+v", payload.Providers)
	}
	if payload.Providers[0].HealthState != observability.StateIdle {
		t.Fatalf("empty provider state = %q, want idle", payload.Providers[0].HealthState)
	}
}

func TestP95FromDisjointBuckets(t *testing.T) {
	if value := p95FromBuckets([10]int64{}, 0); value != nil {
		t.Fatalf("empty histogram p95 = %v, want nil", *value)
	}
	first := p95FromBuckets([10]int64{100}, 100)
	if first == nil || *first != 50 {
		t.Fatalf("first-bucket p95 = %v, want 50", first)
	}
	boundary := p95FromBuckets([10]int64{90, 10}, 100)
	if boundary == nil || *boundary != 100 {
		t.Fatalf("boundary p95 = %v, want 100", boundary)
	}
	overflow := p95FromBuckets([10]int64{94, 0, 0, 0, 0, 0, 0, 0, 0, 6}, 100)
	if overflow != nil {
		t.Fatalf("overflow p95 = %v, want nil", *overflow)
	}
}

func TestAggregateShortTrafficRespectsWindowBoundaries(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	minutes := []store.ProviderTrafficMinute{
		{ProviderID: "p", MinuteStart: now.Add(-30 * time.Minute), Checks: 3},
		{ProviderID: "p", MinuteStart: now.Add(-10 * time.Minute), Checks: 5},
		{ProviderID: "p", MinuteStart: now.Add(-2 * time.Minute), Checks: 7},
		{ProviderID: "p", MinuteStart: now.Add(-30 * time.Second), Checks: 11},
	}
	windows := aggregateShortTraffic(minutes, now)["p"]
	if windows.Last1h.Checks != 26 || windows.Last15m.Checks != 23 ||
		windows.Last5m.Checks != 18 || windows.Last1m.Checks != 11 {
		t.Fatalf("windows = %+v", windows)
	}
}

func TestQueueResponseStaleAndAggregationSemantics(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	sampled := now.Add(-10 * time.Second)
	queue := queueResponse(observability.ProviderQueueSnapshot{Due: 2, Queued: 1}, &sampled, now)
	if queue.Stale || queue.Aggregation != "sampled_point_in_time" ||
		queue.SampleIntervalSeconds != int64(observability.DefaultQueueSampleInterval/time.Second) {
		t.Fatalf("fresh queue = %+v", queue)
	}
	old := now.Add(-10 * time.Minute)
	if !queueResponse(observability.ProviderQueueSnapshot{}, &old, now).Stale {
		t.Fatal("old queue sample not marked stale")
	}
	if !queueResponse(observability.ProviderQueueSnapshot{}, nil, now).Stale {
		t.Fatal("missing queue sample not marked stale")
	}
}

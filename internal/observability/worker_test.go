package observability

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/store"
)

func openObservabilityStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "worker.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	applied, err := db.Migrate(ctx, database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := database.Ready(ctx, applied); err != nil {
		t.Fatalf("ready: %v", err)
	}
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	expires := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if _, err := database.Exec(`INSERT INTO monitors
		(id, label, origin_name, destination_name, departure_date, adults, children, infants,
		 enabled, expires_at, created_at, updated_at)
		VALUES ('m1','Monitor','Origin','Destination','2026-12-15',1,0,0,1,?,?,?)`,
		expires, now, now); err != nil {
		t.Fatalf("insert monitor: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO monitor_providers
		(monitor_id, provider_id, active, origin_id, destination_id, config_version,
		 next_due_at, consecutive_failures, created_at, updated_at)
		VALUES ('m1','p',1,'origin','destination',1,?,0,?,?)`,
		now, now, now); err != nil {
		t.Fatalf("insert target: %v", err)
	}
	return store.New(database.DB)
}

func workerPass(now time.Time, due time.Time, reason DeferralReason) PassObservation {
	return PassObservation{
		Now: now, MonitoringEnabled: true, ConfiguredInterval: 5 * time.Minute,
		TargetsComplete: true,
		Targets:         []TargetObservation{{TargetID: 1, ProviderID: "p", NextDueAt: timePtr(due)}},
		Providers: []ProviderPassObservation{{
			ProviderID: "p", Due: 1, Queued: 1, Deferred: 1,
			Deferrals: []DeferralObservation{{
				TargetID: 1, ProviderID: "p", Due: due, Reason: reason,
			}},
		}},
	}
}

func TestAggregatorPersistsAndReplaysQueueSamples(t *testing.T) {
	ctx := context.Background()
	st := openObservabilityStore(t)
	now := time.Date(2026, 11, 6, 12, 0, 10, 0, time.UTC)
	recorder := NewRecorderWithSampleInterval(0)
	recorder.ObservePass(workerPass(now, now.Add(-time.Minute), ReasonCooldown))

	aggregator := NewAggregator(AggregatorConfig{
		ReadStore: st, WriteStore: st, Recorder: recorder,
		Now: func() time.Time { return now }, Interval: time.Minute,
	})
	if err := aggregator.Restore(ctx); err != nil {
		t.Fatalf("restore: %v", err)
	}
	aggregator.runOnce(ctx)
	rows, err := st.ListProviderMinuteMetrics(ctx, "p", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].SchedulerSamples != 1 || rows[0].DeferralCooldown != 1 ||
		rows[0].DueMax != 1 {
		t.Fatalf("rows after first run = %+v", rows)
	}

	// Replaying the same minute does not duplicate samples or episodes.
	aggregator.runOnce(ctx)
	rows, err = st.ListProviderMinuteMetrics(ctx, "p", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("list replay: %v", err)
	}
	if len(rows) != 1 || rows[0].SchedulerSamples != 1 || rows[0].DeferralCooldown != 1 {
		t.Fatalf("rows after replay = %+v", rows)
	}

	// A restart restores the active episode, so the next minute does not count
	// the same logical deferral again.
	restarted := NewRecorderWithSampleInterval(0)
	next := now.Add(time.Minute)
	aggregator2 := NewAggregator(AggregatorConfig{
		ReadStore: st, WriteStore: st, Recorder: restarted,
		Now: func() time.Time { return next }, Interval: time.Minute,
	})
	if err := aggregator2.Restore(ctx); err != nil {
		t.Fatalf("restart restore: %v", err)
	}
	restarted.ObservePass(workerPass(next, now.Add(-2*time.Minute), ReasonCooldown))
	aggregator2.runOnce(ctx)
	rows, err = st.ListProviderMinuteMetrics(ctx, "p", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("list restart: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows after restart = %+v", rows)
	}
	if rows[1].DeferralCooldown != 0 || rows[1].SchedulerSamples != 1 {
		t.Fatalf("restart minute recounted active episode: %+v", rows[1])
	}

	// A late sample in the first minute corrects the same rollup.
	restarted.ObservePass(workerPass(next.Add(20*time.Second), now.Add(-2*time.Minute), ReasonCooldown))
	aggregator2.runOnce(ctx)
	rows, err = st.ListProviderMinuteMetrics(ctx, "p", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("list late: %v", err)
	}
	if len(rows) != 2 || rows[0].SchedulerSamples != 1 || rows[1].SchedulerSamples != 2 {
		t.Fatalf("rows after late sample = %+v", rows)
	}
}

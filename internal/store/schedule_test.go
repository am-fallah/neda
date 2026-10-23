package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/store"
)

type scheduleTargetSpec struct {
	providerID provider.ID
	active     bool
	due        *time.Time
}

func createScheduleMonitor(t *testing.T, st *store.Store, id string, enabled bool, expiresAt time.Time, expiredAt *time.Time, specs ...scheduleTargetSpec) domain.Monitor {
	t.Helper()
	targets := make([]domain.MonitorProvider, 0, len(specs))
	for _, spec := range specs {
		targets = append(targets, domain.MonitorProvider{
			MonitorID:     id,
			ProviderID:    spec.providerID,
			Active:        spec.active,
			OriginID:      "origin-" + string(spec.providerID),
			DestinationID: "destination-" + string(spec.providerID),
			ConfigVersion: 1,
			NextDueAt:     spec.due,
			CreatedAt:     testNow,
			UpdatedAt:     testNow.Add(-time.Hour),
		})
	}
	created, err := st.CreateMonitor(context.Background(), domain.Monitor{
		ID:              id,
		Label:           "Schedule " + id,
		OriginName:      "Tehran",
		DestinationName: "Mashhad",
		DepartureDate:   "2026-10-30",
		Adults:          1,
		Enabled:         enabled,
		ExpiresAt:       expiresAt,
		ExpiredAt:       expiredAt,
		CreatedAt:       testNow,
		UpdatedAt:       testNow,
		Providers:       targets,
	})
	if err != nil {
		t.Fatalf("CreateMonitor(%s): %v", id, err)
	}
	return created
}

func targetID(t *testing.T, monitor domain.Monitor, providerID provider.ID) int64 {
	t.Helper()
	for _, target := range monitor.Providers {
		if target.ProviderID == providerID {
			return target.ID
		}
	}
	t.Fatalf("monitor %s has no target for provider %s", monitor.ID, providerID)
	return 0
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func TestListScheduleTargetsFiltersEligibility(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	now := testNow
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	expires := now.Add(24 * time.Hour)

	eligible := createScheduleMonitor(t, st, "eligible", true, expires, nil,
		scheduleTargetSpec{providerID: "p-eligible", active: true, due: &past})
	subsecondFuture := createScheduleMonitor(t, st, "subsecond-future", true, now.Add(500*time.Millisecond), nil,
		scheduleTargetSpec{providerID: "p-subsecond-future", active: true, due: &past})
	futureMonitor := createScheduleMonitor(t, st, "future", true, expires, nil,
		scheduleTargetSpec{providerID: "p-future", active: true, due: &future})
	unscheduled := createScheduleMonitor(t, st, "unscheduled", true, expires, nil,
		scheduleTargetSpec{providerID: "p-unscheduled", active: true, due: nil})
	createScheduleMonitor(t, st, "paused", false, expires, nil,
		scheduleTargetSpec{providerID: "p-paused", active: true, due: &past})
	createScheduleMonitor(t, st, "expired-at", true, expires, timePtr(now),
		scheduleTargetSpec{providerID: "p-expired-at", active: true, due: &past})
	createScheduleMonitor(t, st, "expired-by", true, now.Add(-time.Minute), nil,
		scheduleTargetSpec{providerID: "p-expired-by", active: true, due: &past})
	createScheduleMonitor(t, st, "expires-now", true, now, nil,
		scheduleTargetSpec{providerID: "p-expires-now", active: true, due: &past})
	createScheduleMonitor(t, st, "subsecond-past", true, now.Add(-500*time.Millisecond), nil,
		scheduleTargetSpec{providerID: "p-subsecond-past", active: true, due: &past})
	createScheduleMonitor(t, st, "inactive", true, expires, nil,
		scheduleTargetSpec{providerID: "p-inactive", active: false, due: &past})

	targets, err := st.ListScheduleTargets(ctx, now)
	if err != nil {
		t.Fatalf("ListScheduleTargets: %v", err)
	}
	if len(targets) != 4 {
		t.Fatalf("targets = %d, want 4: %+v", len(targets), targets)
	}
	want := []int64{
		targetID(t, eligible, "p-eligible"),
		targetID(t, subsecondFuture, "p-subsecond-future"),
		targetID(t, futureMonitor, "p-future"),
		targetID(t, unscheduled, "p-unscheduled"),
	}
	for index, target := range targets {
		if target.ID != want[index] {
			t.Fatalf("target %d ID = %d, want %d (order should be due, due, future, unscheduled)", index, target.ID, want[index])
		}
	}
	if targets[0].NextDueAt == nil || !targets[0].NextDueAt.Equal(past) {
		t.Fatalf("due target next_due_at = %v, want %v", targets[0].NextDueAt, past)
	}
	if targets[1].ConfigVersion != 1 {
		t.Fatalf("due target config version = %d, want 1", targets[1].ConfigVersion)
	}
	if targets[3].NextDueAt != nil {
		t.Fatalf("unscheduled target next_due_at = %v, want nil", targets[3].NextDueAt)
	}
}

func TestStaggerDueTargetsPreservesFutureAndClampsPast(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	now := testNow
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	expires := now.Add(24 * time.Hour)

	due := createScheduleMonitor(t, st, "due", true, expires, nil,
		scheduleTargetSpec{providerID: "p-due", active: true, due: &past})
	null := createScheduleMonitor(t, st, "null", true, expires, nil,
		scheduleTargetSpec{providerID: "p-null", active: true, due: nil})
	clamped := createScheduleMonitor(t, st, "clamped", true, expires, nil,
		scheduleTargetSpec{providerID: "p-clamped", active: true, due: &past})
	futureMonitor := createScheduleMonitor(t, st, "future", true, expires, nil,
		scheduleTargetSpec{providerID: "p-future", active: true, due: &future})
	paused := createScheduleMonitor(t, st, "paused", false, expires, nil,
		scheduleTargetSpec{providerID: "p-paused", active: true, due: &past})

	calls := make(map[int64]bool)
	staggered, err := st.StaggerDueTargets(ctx, now, func(target domain.ScheduleTarget) time.Time {
		calls[target.ID] = true
		switch target.ProviderID {
		case "p-due":
			return now.Add(10 * time.Second)
		case "p-null":
			return now.Add(20 * time.Second)
		case "p-clamped":
			return now.Add(-time.Hour)
		default:
			t.Fatalf("planner called for unexpected target %+v", target)
			return now
		}
	})
	if err != nil {
		t.Fatalf("StaggerDueTargets: %v", err)
	}
	if staggered != 3 {
		t.Fatalf("staggered = %d, want 3", staggered)
	}
	if calls[targetID(t, futureMonitor, "p-future")] || calls[targetID(t, paused, "p-paused")] {
		t.Fatal("planner called for a future or paused target")
	}

	wantDue := map[string]time.Time{
		"p-due":     now.Add(10 * time.Second),
		"p-null":    now.Add(20 * time.Second),
		"p-clamped": now,
	}
	for _, monitor := range []domain.Monitor{due, null, clamped} {
		reloaded, err := st.GetMonitor(ctx, monitor.ID)
		if err != nil {
			t.Fatalf("GetMonitor(%s): %v", monitor.ID, err)
		}
		target := reloaded.Providers[0]
		if target.NextDueAt == nil || !target.NextDueAt.Equal(wantDue[string(target.ProviderID)]) {
			t.Fatalf("%s next_due_at = %v, want %v", target.ProviderID, target.NextDueAt, wantDue[string(target.ProviderID)])
		}
	}

	preserved, err := st.GetMonitor(ctx, futureMonitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor(future): %v", err)
	}
	if preserved.Providers[0].NextDueAt == nil || !preserved.Providers[0].NextDueAt.Equal(future) {
		t.Fatalf("future next_due_at = %v, want %v", preserved.Providers[0].NextDueAt, future)
	}
	stillPaused, err := st.GetMonitor(ctx, paused.ID)
	if err != nil {
		t.Fatalf("GetMonitor(paused): %v", err)
	}
	if stillPaused.Providers[0].NextDueAt == nil || !stillPaused.Providers[0].NextDueAt.Equal(past) {
		t.Fatal("paused target was staggered")
	}

	if _, err := st.StaggerDueTargets(ctx, now, nil); err == nil {
		t.Fatal("StaggerDueTargets(nil planner) succeeded")
	}
}

func TestRecoverRunningChecksOnlyTouchesRunningRows(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()
	now := testNow
	future := now.Add(time.Hour)
	monitor := createScheduleMonitor(t, st, "recovery", true, now.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "recovery-provider", active: true, due: &future})
	targetID := targetID(t, monitor, "recovery-provider")

	result, err := sqlDB.Exec(`INSERT INTO check_runs
		(monitor_provider_id, config_version, started_at, outcome, created_at)
		VALUES (?, 1, ?, 'running', ?)`,
		targetID, formatTestTime(now.Add(-time.Minute)), formatTestTime(now.Add(-time.Minute)),
	)
	if err != nil {
		t.Fatalf("insert running check: %v", err)
	}
	runningID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("running check id: %v", err)
	}
	result, err = sqlDB.Exec(`INSERT INTO check_runs
		(monitor_provider_id, config_version, started_at, finished_at, outcome, journey_count, created_at)
		VALUES (?, 1, ?, ?, 'success', 2, ?)`,
		targetID, formatTestTime(now.Add(-2*time.Minute)), formatTestTime(now.Add(-time.Minute)), formatTestTime(now.Add(-2*time.Minute)),
	)
	if err != nil {
		t.Fatalf("insert success check: %v", err)
	}
	successID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("success check id: %v", err)
	}

	stateResult, err := sqlDB.Exec(`INSERT INTO inventory_states
		(monitor_provider_id, config_version, journey_key, offering_key, state, state_since, last_observed_at, updated_at)
		VALUES (?, 1, 'journey', 'offering', 'reservable', ?, ?, ?)`,
		targetID, formatTestTime(now), formatTestTime(now), formatTestTime(now),
	)
	if err != nil {
		t.Fatalf("insert inventory state: %v", err)
	}
	stateID, err := stateResult.LastInsertId()
	if err != nil {
		t.Fatalf("inventory state id: %v", err)
	}
	episodeResult, err := sqlDB.Exec(`INSERT INTO availability_episodes
		(state_id, episode_seq, opened_at) VALUES (?, 1, ?)`,
		stateID, formatTestTime(now),
	)
	if err != nil {
		t.Fatalf("insert episode: %v", err)
	}
	episodeID, err := episodeResult.LastInsertId()
	if err != nil {
		t.Fatalf("episode id: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO events
		(id, kind, dedupe_key, monitor_id, provider_id, episode_id, occurred_at, created_at)
		VALUES ('event-1', 'availability', 'dedupe-1', ?, 'recovery-provider', ?, ?, ?)`,
		monitor.ID, episodeID, formatTestTime(now), formatTestTime(now),
	); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	before, err := st.GetMonitor(ctx, monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor before recovery: %v", err)
	}
	countsBefore := tableCounts(t, sqlDB)

	recovered, err := st.RecoverRunningChecks(ctx, now)
	if err != nil {
		t.Fatalf("RecoverRunningChecks: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}

	var outcome string
	var finishedAt sql.NullString
	var configVersion int64
	if err := sqlDB.QueryRow(`SELECT outcome, finished_at, config_version FROM check_runs WHERE id = ?`, runningID).
		Scan(&outcome, &finishedAt, &configVersion); err != nil {
		t.Fatalf("read recovered check: %v", err)
	}
	if outcome != "abandoned" || !finishedAt.Valid || configVersion != 1 {
		t.Fatalf("recovered check = %q/%v/%d, want abandoned/set/1", outcome, finishedAt, configVersion)
	}
	if err := sqlDB.QueryRow(`SELECT outcome, finished_at, journey_count FROM check_runs WHERE id = ?`, successID).
		Scan(&outcome, &finishedAt, new(int)); err != nil {
		t.Fatalf("read success check: %v", err)
	}
	if outcome != "success" || !finishedAt.Valid {
		t.Fatalf("success check changed to %q/%v", outcome, finishedAt)
	}

	countsAfter := tableCounts(t, sqlDB)
	for table, count := range countsBefore {
		if countsAfter[table] != count {
			t.Fatalf("%s count = %d after recovery, want %d", table, countsAfter[table], count)
		}
	}
	after, err := st.GetMonitor(ctx, monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor after recovery: %v", err)
	}
	if after.Providers[0].NextDueAt == nil || !after.Providers[0].NextDueAt.Equal(*before.Providers[0].NextDueAt) {
		t.Fatalf("target next_due_at changed during recovery: %v -> %v", before.Providers[0].NextDueAt, after.Providers[0].NextDueAt)
	}
	if after.Providers[0].LastAttemptAt != nil || after.Providers[0].ConfigVersion != before.Providers[0].ConfigVersion {
		t.Fatal("target diagnostics changed during recovery")
	}
}

func tableCounts(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	counts := make(map[string]int)
	for _, table := range []string{"inventory_states", "availability_episodes", "events"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		counts[table] = count
	}
	return counts
}

func TestPollIntervalDefaultsBoundsAndPreservesOperatorValue(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	if err := st.EnsurePollInterval(ctx, testNow); err != nil {
		t.Fatalf("EnsurePollInterval: %v", err)
	}
	interval, err := st.PollInterval(ctx)
	if err != nil {
		t.Fatalf("PollInterval: %v", err)
	}
	if interval != domain.DefaultPollInterval {
		t.Fatalf("default interval = %v, want %v", interval, domain.DefaultPollInterval)
	}
	setting, err := st.GetSetting(ctx, store.PollIntervalSettingKey)
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if setting.Value != "300" || setting.IsSecret {
		t.Fatalf("seeded setting = %+v, want 300/non-secret", setting)
	}

	if err := st.SetSetting(ctx, store.Setting{
		Key: store.PollIntervalSettingKey, Value: "120", UpdatedAt: testNow,
	}); err != nil {
		t.Fatalf("SetSetting(120): %v", err)
	}
	if err := st.EnsurePollInterval(ctx, testNow.Add(time.Minute)); err != nil {
		t.Fatalf("EnsurePollInterval(existing): %v", err)
	}
	interval, err = st.PollInterval(ctx)
	if err != nil {
		t.Fatalf("PollInterval(existing): %v", err)
	}
	if interval != 2*time.Minute {
		t.Fatalf("operator interval = %v, want 2m", interval)
	}

	for _, value := range []string{"59", "3601", "abc"} {
		if err := st.SetSetting(ctx, store.Setting{
			Key: store.PollIntervalSettingKey, Value: value, UpdatedAt: testNow,
		}); err != nil {
			t.Fatalf("SetSetting(%q): %v", value, err)
		}
		if _, err := st.PollInterval(ctx); err == nil {
			t.Fatalf("PollInterval(%q) succeeded", value)
		}
	}
	for _, value := range []string{"60", "3600"} {
		if err := st.SetSetting(ctx, store.Setting{
			Key: store.PollIntervalSettingKey, Value: value, UpdatedAt: testNow,
		}); err != nil {
			t.Fatalf("SetSetting(%q): %v", value, err)
		}
		if _, err := st.PollInterval(ctx); err != nil {
			t.Fatalf("PollInterval(%q): %v", value, err)
		}
	}
}

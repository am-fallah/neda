package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/store"
)

func TestStartCheckRunInsertsOnlyRunningRow(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()
	monitor := createScheduleMonitor(t, st, "check-start", true, testNow.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "p-check-start", active: true})
	targetID := targetID(t, monitor, "p-check-start")
	before := monitor.Providers[0]

	checkRunID, err := st.StartCheckRun(ctx, targetID, before.ConfigVersion, testNow)
	if err != nil {
		t.Fatalf("StartCheckRun: %v", err)
	}
	if checkRunID <= 0 {
		t.Fatalf("check run id = %d, want positive", checkRunID)
	}

	var (
		monitorProviderID int64
		configVersion     int64
		startedAt         string
		finishedAt        sql.NullString
		outcome           string
		createdAt         string
	)
	if err := sqlDB.QueryRowContext(ctx, `SELECT monitor_provider_id, config_version, started_at,
		finished_at, outcome, created_at FROM check_runs WHERE id = ?`, checkRunID).Scan(
		&monitorProviderID, &configVersion, &startedAt, &finishedAt, &outcome, &createdAt,
	); err != nil {
		t.Fatalf("query check run: %v", err)
	}
	if monitorProviderID != targetID {
		t.Fatalf("monitor_provider_id = %d, want %d", monitorProviderID, targetID)
	}
	if configVersion != before.ConfigVersion {
		t.Fatalf("config_version = %d, want %d", configVersion, before.ConfigVersion)
	}
	wantTime := testNow.Format(time.RFC3339Nano)
	if startedAt != wantTime || createdAt != wantTime {
		t.Fatalf("started_at=%q created_at=%q, want %q", startedAt, createdAt, wantTime)
	}
	if outcome != "running" {
		t.Fatalf("outcome = %q, want running", outcome)
	}
	if finishedAt.Valid {
		t.Fatalf("finished_at = %q, want NULL", finishedAt.String)
	}

	var checkRuns int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM check_runs`).Scan(&checkRuns); err != nil {
		t.Fatalf("count check runs: %v", err)
	}
	if checkRuns != 1 {
		t.Fatalf("check_runs count = %d, want 1", checkRuns)
	}

	reloaded, err := st.GetMonitor(ctx, monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	after := reloaded.Providers[0]
	if after.NextDueAt != nil ||
		after.LastAttemptAt != nil ||
		after.LastSuccessAt != nil ||
		after.LastOutcome != "" ||
		after.LastErrorKind != "" ||
		after.LastErrorAt != nil ||
		after.ConsecutiveFailures != 0 ||
		!after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("start transaction changed target state: before=%+v after=%+v", before, after)
	}
	for _, table := range []string{"inventory_states", "availability_episodes", "events"} {
		var count int
		query := map[string]string{
			"inventory_states":      `SELECT COUNT(*) FROM inventory_states`,
			"availability_episodes": `SELECT COUNT(*) FROM availability_episodes`,
			"events":                `SELECT COUNT(*) FROM events`,
		}[table]
		if err := sqlDB.QueryRowContext(ctx, query).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", table, count)
		}
	}
}

func TestStartCheckRunRejectsMissingTarget(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()

	_, err := st.StartCheckRun(ctx, 999999, 1, testNow)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("StartCheckRun error = %v, want ErrNotFound", err)
	}
	var count int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM check_runs`).Scan(&count); err != nil {
		t.Fatalf("count check runs: %v", err)
	}
	if count != 0 {
		t.Fatalf("check_runs count = %d, want 0", count)
	}
}

func TestStartCheckRunRejectsInvalidInput(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	monitor := createScheduleMonitor(t, st, "check-invalid", true, testNow.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "p-check-invalid", active: true})
	targetID := targetID(t, monitor, "p-check-invalid")

	cases := []struct {
		name          string
		targetID      int64
		configVersion int64
		startedAt     time.Time
	}{
		{"zero target", 0, 1, testNow},
		{"negative target", -1, 1, testNow},
		{"zero config version", targetID, 0, testNow},
		{"zero start time", targetID, 1, time.Time{}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := st.StartCheckRun(ctx, test.targetID, test.configVersion, test.startedAt); err == nil {
				t.Fatal("StartCheckRun succeeded, want validation error")
			}
		})
	}
}

func TestStartCheckRunCanceledContextInsertsNothing(t *testing.T) {
	st, sqlDB := newTestStore(t)
	monitor := createScheduleMonitor(t, st, "check-canceled", true, testNow.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "p-check-canceled", active: true})
	targetID := targetID(t, monitor, "p-check-canceled")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := st.StartCheckRun(ctx, targetID, 1, testNow); err == nil {
		t.Fatal("StartCheckRun succeeded with canceled context")
	}

	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM check_runs`).Scan(&count); err != nil {
		t.Fatalf("count check runs: %v", err)
	}
	if count != 0 {
		t.Fatalf("check_runs count = %d, want 0", count)
	}
}

package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/store"
)

func TestApplyTargetScheduleUpdatesOnlySchedulingFields(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	due := testNow.Add(time.Minute)
	monitor := createScheduleMonitor(t, st, "schedule-fields", true, testNow.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "p-schedule", active: true, due: &due})
	id := targetID(t, monitor, "p-schedule")
	before := monitor.Providers[0]

	nextDue := testNow.Add(30 * time.Minute)
	if err := st.ApplyTargetSchedule(ctx, id, 3, nextDue, testNow); err != nil {
		t.Fatalf("ApplyTargetSchedule: %v", err)
	}
	reloaded, err := st.GetMonitor(ctx, monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	after := reloaded.Providers[0]
	if after.ConsecutiveFailures != 3 {
		t.Fatalf("ConsecutiveFailures = %d, want 3", after.ConsecutiveFailures)
	}
	if after.NextDueAt == nil || !after.NextDueAt.Equal(nextDue) {
		t.Fatalf("NextDueAt = %v, want %v", after.NextDueAt, nextDue)
	}
	if !after.UpdatedAt.Equal(testNow) {
		t.Fatalf("UpdatedAt = %v, want %v", after.UpdatedAt, testNow)
	}
	if after.ConfigVersion != before.ConfigVersion ||
		after.Active != before.Active ||
		after.ProviderID != before.ProviderID ||
		after.OriginID != before.OriginID ||
		after.DestinationID != before.DestinationID ||
		after.LastAttemptAt != nil ||
		after.LastSuccessAt != nil ||
		after.LastOutcome != "" ||
		after.LastErrorKind != "" ||
		after.LastErrorAt != nil {
		t.Fatalf("diagnostics/identity changed: before=%+v after=%+v", before, after)
	}
}

func TestApplyTargetScheduleIsTransactionScoped(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	due := testNow.Add(time.Minute)
	monitor := createScheduleMonitor(t, st, "schedule-tx", true, testNow.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "p-tx", active: true, due: &due})
	id := targetID(t, monitor, "p-tx")

	boom := errors.New("boom")
	err := st.WithTx(ctx, func(tx *store.Store) error {
		if err := tx.ApplyTargetSchedule(ctx, id, 5, testNow.Add(time.Hour), testNow); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("WithTx error = %v, want boom", err)
	}
	rolledBack, err := st.GetMonitor(ctx, monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor after rollback: %v", err)
	}
	if rolledBack.Providers[0].ConsecutiveFailures != 0 ||
		rolledBack.Providers[0].NextDueAt == nil ||
		!rolledBack.Providers[0].NextDueAt.Equal(due) {
		t.Fatalf("rollback leaked schedule: %+v", rolledBack.Providers[0])
	}

	err = st.WithTx(ctx, func(tx *store.Store) error {
		return tx.ApplyTargetSchedule(ctx, id, 2, testNow.Add(20*time.Minute), testNow)
	})
	if err != nil {
		t.Fatalf("WithTx commit: %v", err)
	}
	committed, err := st.GetMonitor(ctx, monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor after commit: %v", err)
	}
	if committed.Providers[0].ConsecutiveFailures != 2 ||
		committed.Providers[0].NextDueAt == nil ||
		!committed.Providers[0].NextDueAt.Equal(testNow.Add(20*time.Minute)) {
		t.Fatalf("commit lost schedule: %+v", committed.Providers[0])
	}
}

func TestApplyTargetScheduleRejectsMissingTargetAndInvalidInput(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	due := testNow.Add(time.Minute)
	monitor := createScheduleMonitor(t, st, "schedule-invalid", true, testNow.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "p-invalid", active: true, due: &due})
	id := targetID(t, monitor, "p-invalid")

	if err := st.ApplyTargetSchedule(ctx, 999999, 1, testNow.Add(time.Hour), testNow); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("missing target error = %v, want ErrConflict", err)
	}
	validDue := testNow.Add(time.Hour)
	cases := []struct {
		name     string
		id       int64
		failures int
		nextDue  time.Time
		now      time.Time
	}{
		{"zero target", 0, 1, validDue, testNow},
		{"negative failures", id, -1, validDue, testNow},
		{"zero next due", id, 1, time.Time{}, testNow},
		{"zero update time", id, 1, validDue, time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := st.ApplyTargetSchedule(ctx, tc.id, tc.failures, tc.nextDue, tc.now); err == nil {
				t.Fatal("ApplyTargetSchedule succeeded")
			}
		})
	}
}

func TestResumePreservesTargetFailures(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	due := testNow.Add(time.Minute)
	monitor := createScheduleMonitor(t, st, "schedule-resume", true, testNow.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "p-resume", active: true, due: &due})
	id := targetID(t, monitor, "p-resume")
	if err := st.ApplyTargetSchedule(ctx, id, 2, testNow.Add(time.Hour), testNow); err != nil {
		t.Fatalf("ApplyTargetSchedule: %v", err)
	}
	if _, err := st.PauseMonitor(ctx, monitor.ID, testNow); err != nil {
		t.Fatalf("PauseMonitor: %v", err)
	}
	if _, err := st.ResumeMonitor(ctx, monitor.ID, testNow, testNow.Add(time.Second)); err != nil {
		t.Fatalf("ResumeMonitor: %v", err)
	}
	reloaded, err := st.GetMonitor(ctx, monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if reloaded.Providers[0].ConsecutiveFailures != 2 {
		t.Fatalf("resume reset failures to %d, want 2", reloaded.Providers[0].ConsecutiveFailures)
	}
}

func TestApplyTargetSchedulePersistsAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "target-schedule-persist.db")
	first, err := db.Open(ctx, path)
	if err != nil {
		t.Fatalf("db.Open first: %v", err)
	}
	applied, err := db.Migrate(ctx, first.DB, migrations.FS)
	if err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if err := first.Ready(ctx, applied); err != nil {
		t.Fatalf("database.Ready: %v", err)
	}
	firstStore := store.New(first.DB)
	due := testNow.Add(time.Minute)
	monitor := createScheduleMonitor(t, firstStore, "schedule-restart", true, testNow.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: "p-restart", active: true, due: &due})
	id := targetID(t, monitor, "p-restart")
	nextDue := testNow.Add(2 * time.Hour)
	if err := firstStore.ApplyTargetSchedule(ctx, id, 4, nextDue, testNow); err != nil {
		t.Fatalf("ApplyTargetSchedule: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := db.Open(ctx, path)
	if err != nil {
		t.Fatalf("db.Open second: %v", err)
	}
	defer func() {
		if err := second.Close(); err != nil {
			t.Errorf("close second: %v", err)
		}
	}()
	secondStore := store.New(second.DB)
	reloaded, err := secondStore.GetMonitor(ctx, monitor.ID)
	if err != nil {
		t.Fatalf("GetMonitor after reopen: %v", err)
	}
	target := reloaded.Providers[0]
	if target.ConsecutiveFailures != 4 {
		t.Fatalf("ConsecutiveFailures after reopen = %d, want 4", target.ConsecutiveFailures)
	}
	if target.NextDueAt == nil || !target.NextDueAt.Equal(nextDue) {
		t.Fatalf("NextDueAt after reopen = %v, want %v", target.NextDueAt, nextDue)
	}
}

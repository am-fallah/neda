package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/store"
)

func startOutcomeCheck(t *testing.T, st *store.Store, suffix string) (string, int64, int64) {
	t.Helper()
	providerID := provider.ID("p-" + suffix)
	monitor := createScheduleMonitor(t, st, "outcome-"+suffix, true, testNow.Add(24*time.Hour), nil,
		scheduleTargetSpec{providerID: providerID, active: true})
	targetID := targetID(t, monitor, providerID)
	checkID, err := st.StartCheckRun(context.Background(), targetID, monitor.Providers[0].ConfigVersion, testNow)
	if err != nil {
		t.Fatalf("StartCheckRun: %v", err)
	}
	return monitor.ID, targetID, checkID
}

func TestFinalizeCheckRunSuccessAndConditionalConflict(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()
	monitorID, targetID, checkID := startOutcomeCheck(t, st, "success")
	httpStatus := 200
	finished := testNow.Add(2 * time.Second)

	err := st.WithSQLTx(ctx, func(tx *sql.Tx, _ *store.Store) error {
		return st.FinalizeCheckRun(ctx, tx, store.CheckFinalization{
			CheckRunID:         checkID,
			Outcome:            store.CheckOutcomeSuccess,
			FinishedAt:         finished,
			ErrorKind:          "",
			HTTPStatus:         &httpStatus,
			Duration:           2 * time.Second,
			JourneyCount:       2,
			OfferingCount:      7,
			ReservableCount:    3,
			NotReservableCount: 2,
			UnknownCount:       2,
			WarningCount:       1,
		})
	})
	if err != nil {
		t.Fatalf("FinalizeCheckRun: %v", err)
	}

	var (
		outcome, finishedAt       string
		errorKind                 sql.NullString
		httpValue                 sql.NullInt64
		duration                  int64
		journeys, offerings       int
		reservable, notReservable int
		unknown, warnings         int
	)
	if err := sqlDB.QueryRow(`SELECT outcome, finished_at, error_kind, http_status, duration_ms,
		journey_count, offering_count, reservable_count, not_reservable_count, unknown_count, warning_count
		FROM check_runs WHERE id = ?`, checkID).Scan(
		&outcome, &finishedAt, &errorKind, &httpValue, &duration,
		&journeys, &offerings, &reservable, &notReservable, &unknown, &warnings,
	); err != nil {
		t.Fatalf("query finalized check: %v", err)
	}
	if outcome != "success" || finishedAt != finished.Format(time.RFC3339Nano) || errorKind.Valid {
		t.Fatalf("identity = %q/%q/%+v", outcome, finishedAt, errorKind)
	}
	if !httpValue.Valid || httpValue.Int64 != 200 {
		t.Fatalf("http_status = %+v, want 200", httpValue)
	}
	if duration != 2000 || journeys != 2 || offerings != 7 || reservable != 3 ||
		notReservable != 2 || unknown != 2 || warnings != 1 {
		t.Fatalf("counts = %d/%d/%d/%d/%d/%d/%d", duration, journeys, offerings, reservable, notReservable, unknown, warnings)
	}

	reloaded, err := st.GetMonitor(ctx, monitorID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	after := reloaded.Providers[0]
	if after.LastAttemptAt != nil || after.LastSuccessAt != nil || after.LastOutcome != "" ||
		after.LastErrorKind != "" || after.LastErrorAt != nil || after.ConsecutiveFailures != 0 ||
		after.NextDueAt != nil {
		t.Fatalf("finalization changed target state: %+v", after)
	}
	if targetID <= 0 {
		t.Fatal("invalid target id")
	}

	err = st.WithSQLTx(ctx, func(tx *sql.Tx, _ *store.Store) error {
		return st.FinalizeCheckRun(ctx, tx, store.CheckFinalization{
			CheckRunID: checkID,
			Outcome:    store.CheckOutcomeError,
			FinishedAt: finished.Add(time.Second),
		})
	})
	if !errors.Is(err, store.ErrNoRunningCheck) {
		t.Fatalf("second finalize error = %v, want ErrNoRunningCheck", err)
	}

	if err := sqlDB.QueryRow(`SELECT outcome FROM check_runs WHERE id = ?`, checkID).Scan(&outcome); err != nil {
		t.Fatalf("query outcome after conflict: %v", err)
	}
	if outcome != "success" {
		t.Fatalf("outcome after conflict = %q, want success", outcome)
	}
}

func TestFinalizeCheckRunRollbackLeavesRunningRow(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()
	_, _, checkID := startOutcomeCheck(t, st, "rollback")
	boom := errors.New("boom")

	err := st.WithSQLTx(ctx, func(tx *sql.Tx, _ *store.Store) error {
		if err := st.FinalizeCheckRun(ctx, tx, store.CheckFinalization{
			CheckRunID: checkID,
			Outcome:    store.CheckOutcomeError,
			FinishedAt: testNow.Add(time.Second),
			ErrorKind:  "timeout",
			Duration:   time.Second,
		}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("WithSQLTx error = %v, want %v", err, boom)
	}

	var outcome string
	var finishedAt sql.NullString
	if err := sqlDB.QueryRow(`SELECT outcome, finished_at FROM check_runs WHERE id = ?`, checkID).Scan(&outcome, &finishedAt); err != nil {
		t.Fatalf("query running row: %v", err)
	}
	if outcome != "running" || finishedAt.Valid {
		t.Fatalf("row after rollback = %q/%+v, want running/NULL", outcome, finishedAt)
	}
}

func TestFinalizeCheckRunOutcomesAndValidation(t *testing.T) {
	tests := []struct {
		name    string
		outcome store.CheckOutcome
	}{
		{"empty", store.CheckOutcomeEmpty},
		{"error", store.CheckOutcomeError},
		{"discarded", store.CheckOutcomeDiscarded},
		{"abandoned", store.CheckOutcomeAbandoned},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, sqlDB := newTestStore(t)
			ctx := context.Background()
			_, _, checkID := startOutcomeCheck(t, st, test.name)
			err := st.WithSQLTx(ctx, func(tx *sql.Tx, _ *store.Store) error {
				return st.FinalizeCheckRun(ctx, tx, store.CheckFinalization{
					CheckRunID: checkID,
					Outcome:    test.outcome,
					FinishedAt: testNow.Add(time.Second),
					ErrorKind:  test.name + "_reason",
					Duration:   time.Second,
				})
			})
			if err != nil {
				t.Fatalf("FinalizeCheckRun(%s): %v", test.outcome, err)
			}
			var outcome string
			if err := sqlDB.QueryRow(`SELECT outcome FROM check_runs WHERE id = ?`, checkID).Scan(&outcome); err != nil {
				t.Fatalf("query outcome: %v", err)
			}
			if outcome != string(test.outcome) {
				t.Fatalf("outcome = %q, want %q", outcome, test.outcome)
			}
		})
	}

	st, _ := newTestStore(t)
	ctx := context.Background()
	invalid := []store.CheckFinalization{
		{CheckRunID: 0, Outcome: store.CheckOutcomeSuccess, FinishedAt: testNow},
		{CheckRunID: 1, Outcome: "running", FinishedAt: testNow},
		{CheckRunID: 1, Outcome: store.CheckOutcomeSuccess, FinishedAt: time.Time{}},
		{CheckRunID: 1, Outcome: store.CheckOutcomeSuccess, FinishedAt: testNow, Duration: -time.Second},
		{CheckRunID: 1, Outcome: store.CheckOutcomeSuccess, FinishedAt: testNow, JourneyCount: -1},
		{CheckRunID: 1, Outcome: store.CheckOutcomeSuccess, FinishedAt: testNow, HTTPStatus: intPtr(-1)},
	}
	for i, input := range invalid {
		if err := st.WithSQLTx(ctx, func(tx *sql.Tx, _ *store.Store) error {
			return st.FinalizeCheckRun(ctx, tx, input)
		}); err == nil {
			t.Fatalf("invalid finalization %d succeeded", i)
		}
	}
}

func intPtr(value int) *int { return &value }

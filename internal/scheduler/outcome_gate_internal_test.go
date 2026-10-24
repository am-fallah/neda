package scheduler

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/store"
)

// TestFinalizeSuccessReevaluatesGateInsideTransaction exercises the mandatory
// second gate directly: the target is valid when the attempt is constructed and
// is changed before the outcome transaction opens.
func TestFinalizeSuccessReevaluatesGateInsideTransaction(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		reason string
		mutate func(t *testing.T, sqlDB *sql.DB, targetID int64, monitorID string)
	}{
		{
			name:   "target inactive",
			reason: "target_inactive",
			mutate: func(t *testing.T, sqlDB *sql.DB, targetID int64, _ string) {
				mustExec(t, sqlDB, `UPDATE monitor_providers SET active = 0 WHERE id = ?`, targetID)
			},
		},
		{
			name:   "monitor disabled",
			reason: "monitor_disabled",
			mutate: func(t *testing.T, sqlDB *sql.DB, _ int64, monitorID string) {
				mustExec(t, sqlDB, `UPDATE monitors SET enabled = 0 WHERE id = ?`, monitorID)
			},
		},
		{
			name:   "monitor expired",
			reason: "monitor_expired",
			mutate: func(t *testing.T, sqlDB *sql.DB, _ int64, monitorID string) {
				mustExec(t, sqlDB, `UPDATE monitors SET expires_at = ? WHERE id = ?`,
					now.Add(-time.Minute).Format(time.RFC3339Nano), monitorID)
			},
		},
		{
			name:   "monitor expired marker",
			reason: "monitor_expired",
			mutate: func(t *testing.T, sqlDB *sql.DB, _ int64, monitorID string) {
				mustExec(t, sqlDB, `UPDATE monitors SET expired_at = ? WHERE id = ?`,
					now.Format(time.RFC3339Nano), monitorID)
			},
		},
		{
			name:   "config changed",
			reason: "config_changed",
			mutate: func(t *testing.T, sqlDB *sql.DB, targetID int64, _ string) {
				mustExec(t, sqlDB, `UPDATE monitor_providers SET config_version = config_version + 1 WHERE id = ?`, targetID)
			},
		},
		{
			name:   "booking changed",
			reason: "booking_config_changed",
			mutate: func(t *testing.T, sqlDB *sql.DB, targetID int64, _ string) {
				mustExec(t, sqlDB, `UPDATE monitor_providers SET booking_slug = 'new-slug' WHERE id = ?`, targetID)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, sqlDB := newInternalOutcomeStore(t)
			monitor := createInternalOutcomeMonitor(t, st, "gate-"+test.name)
			target := monitor.Providers[0]
			checkID, err := st.StartCheckRun(context.Background(), target.ID, target.ConfigVersion, now)
			if err != nil {
				t.Fatalf("StartCheckRun: %v", err)
			}
			attempt := CheckAttempt{
				CheckRunID:    checkID,
				TargetID:      target.ID,
				MonitorID:     monitor.ID,
				ProviderID:    target.ProviderID,
				ConfigVersion: target.ConfigVersion,
				BookingSlug:   target.BookingSlug,
				StartedAt:     now,
				FinishedAt:    now,
				Duration:      time.Second,
				Classification: provider.Classification{
					Category: provider.CategorySuccess,
				},
				Result: provider.Result{
					ObservedAt: now,
					Journeys: []provider.JourneyObservation{{
						Journey: provider.Journey{Key: "j1"},
						Offerings: []provider.OfferingObservation{{
							Offering: provider.Offering{Key: "o1"},
							State:    provider.StateReservable,
						}},
					}},
				},
			}

			test.mutate(t, sqlDB, target.ID, monitor.ID)

			runner, err := NewOutcomeRunner(OutcomeRunnerConfig{
				Stage:  &CheckStage{},
				Store:  st,
				Now:    func() time.Time { return now },
				Random: defaultRandom{},
			})
			if err != nil {
				t.Fatalf("NewOutcomeRunner: %v", err)
			}
			result, err := runner.finalizeSuccess(context.Background(), attempt)
			if err != nil {
				t.Fatalf("finalizeSuccess: %v", err)
			}
			if result.Settled {
				t.Fatal("in-transaction gate discard was settled")
			}

			var outcome string
			var errorKind sql.NullString
			if err := sqlDB.QueryRow(`SELECT outcome, error_kind FROM check_runs WHERE id = ?`, checkID).Scan(&outcome, &errorKind); err != nil {
				t.Fatalf("query check run: %v", err)
			}
			if outcome != "discarded" || !errorKind.Valid || errorKind.String != test.reason {
				t.Fatalf("check outcome = %q/%+v, want discarded/%s", outcome, errorKind, test.reason)
			}
			for _, query := range []string{
				`SELECT COUNT(*) FROM inventory_states`,
				`SELECT COUNT(*) FROM availability_episodes`,
				`SELECT COUNT(*) FROM provider_health`,
				`SELECT COUNT(*) FROM events`,
				`SELECT COUNT(*) FROM notification_deliveries`,
			} {
				if count := mustCount(t, sqlDB, query); count != 0 {
					t.Fatalf("%s = %d, want 0", query, count)
				}
			}
		})
	}
}

func TestFinalizeErrorReevaluatesGateInsideTransaction(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	st, sqlDB := newInternalOutcomeStore(t)
	monitor := createInternalOutcomeMonitor(t, st, "error-gate")
	target := monitor.Providers[0]
	checkID, err := st.StartCheckRun(context.Background(), target.ID, target.ConfigVersion, now)
	if err != nil {
		t.Fatalf("StartCheckRun: %v", err)
	}
	attempt := CheckAttempt{
		CheckRunID:    checkID,
		TargetID:      target.ID,
		MonitorID:     monitor.ID,
		ProviderID:    target.ProviderID,
		ConfigVersion: target.ConfigVersion,
		BookingSlug:   target.BookingSlug,
		StartedAt:     now,
		FinishedAt:    now,
		Duration:      time.Second,
		Err:           &provider.Error{Kind: provider.ErrTimeout, Op: "test"},
		Classification: provider.Classification{
			Category:           provider.CategoryInfrastructure,
			Kind:               provider.ErrTimeout,
			CountsTowardOutage: true,
			TargetBackoff:      true,
		},
	}
	if _, err := sqlDB.Exec(`UPDATE monitor_providers SET config_version = config_version + 1 WHERE id = ?`, target.ID); err != nil {
		t.Fatalf("change config version: %v", err)
	}
	runner, err := NewOutcomeRunner(OutcomeRunnerConfig{
		Stage:  &CheckStage{},
		Store:  st,
		Now:    func() time.Time { return now },
		Random: defaultRandom{},
	})
	if err != nil {
		t.Fatalf("NewOutcomeRunner: %v", err)
	}
	result, err := runner.finalizeError(context.Background(), attempt)
	if err != nil {
		t.Fatalf("finalizeError: %v", err)
	}
	if result.Settled {
		t.Fatal("error-path gate discard was settled")
	}
	var outcome string
	var errorKind sql.NullString
	if err := sqlDB.QueryRow(`SELECT outcome, error_kind FROM check_runs WHERE id = ?`, checkID).Scan(&outcome, &errorKind); err != nil {
		t.Fatalf("query check run: %v", err)
	}
	if outcome != "discarded" || !errorKind.Valid || errorKind.String != "config_changed" {
		t.Fatalf("check outcome = %q/%+v, want discarded/config_changed", outcome, errorKind)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM inventory_states`,
		`SELECT COUNT(*) FROM provider_health`,
		`SELECT COUNT(*) FROM events`,
		`SELECT COUNT(*) FROM notification_deliveries`,
	} {
		if count := mustCount(t, sqlDB, query); count != 0 {
			t.Fatalf("%s = %d, want 0", query, count)
		}
	}
}

func TestFinalizeErrorDoesNotDetachNonTimeoutShutdown(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	st, sqlDB := newInternalOutcomeStore(t)
	monitor := createInternalOutcomeMonitor(t, st, "error-shutdown")
	target := monitor.Providers[0]
	checkID, err := st.StartCheckRun(context.Background(), target.ID, target.ConfigVersion, now)
	if err != nil {
		t.Fatalf("StartCheckRun: %v", err)
	}
	attempt := CheckAttempt{
		CheckRunID:    checkID,
		TargetID:      target.ID,
		MonitorID:     monitor.ID,
		ProviderID:    target.ProviderID,
		ConfigVersion: target.ConfigVersion,
		BookingSlug:   target.BookingSlug,
		StartedAt:     now,
		FinishedAt:    now,
		Duration:      time.Second,
		Err:           &provider.Error{Kind: provider.ErrNetwork, Op: "test"},
		Classification: provider.Classification{
			Category:           provider.CategoryInfrastructure,
			Kind:               provider.ErrNetwork,
			CountsTowardOutage: true,
			TargetBackoff:      true,
		},
	}
	runner, err := NewOutcomeRunner(OutcomeRunnerConfig{
		Stage:  &CheckStage{},
		Store:  st,
		Now:    func() time.Time { return now },
		Random: defaultRandom{},
	})
	if err != nil {
		t.Fatalf("NewOutcomeRunner: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := runner.finalizeError(ctx, attempt)
	if err == nil {
		t.Fatal("finalizeError succeeded with canceled non-timeout context")
	}
	if !result.Settled {
		t.Fatal("canceled finalizeError did not settle")
	}
	var outcome string
	if err := sqlDB.QueryRow(`SELECT outcome FROM check_runs WHERE id = ?`, checkID).Scan(&outcome); err != nil {
		t.Fatalf("query check run: %v", err)
	}
	if outcome != "running" {
		t.Fatalf("check outcome = %q, want running", outcome)
	}
}

func TestFinalizeErrorTimeoutDetachesShutdown(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	st, sqlDB := newInternalOutcomeStore(t)
	if err := st.EnsurePollInterval(context.Background(), now); err != nil {
		t.Fatalf("EnsurePollInterval: %v", err)
	}
	monitor := createInternalOutcomeMonitor(t, st, "timeout-shutdown")
	target := monitor.Providers[0]
	checkID, err := st.StartCheckRun(context.Background(), target.ID, target.ConfigVersion, now)
	if err != nil {
		t.Fatalf("StartCheckRun: %v", err)
	}
	attempt := CheckAttempt{
		CheckRunID:    checkID,
		TargetID:      target.ID,
		MonitorID:     monitor.ID,
		ProviderID:    target.ProviderID,
		ConfigVersion: target.ConfigVersion,
		BookingSlug:   target.BookingSlug,
		StartedAt:     now,
		FinishedAt:    now,
		Duration:      time.Second,
		Err:           &provider.Error{Kind: provider.ErrTimeout, Op: "test"},
		Classification: provider.Classification{
			Category:           provider.CategoryInfrastructure,
			Kind:               provider.ErrTimeout,
			CountsTowardOutage: true,
			TargetBackoff:      true,
		},
	}
	runner, err := NewOutcomeRunner(OutcomeRunnerConfig{
		Stage:  &CheckStage{},
		Store:  st,
		Now:    func() time.Time { return now },
		Random: defaultRandom{},
	})
	if err != nil {
		t.Fatalf("NewOutcomeRunner: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := runner.finalizeError(ctx, attempt)
	if err != nil {
		t.Fatalf("finalizeError timeout: %v", err)
	}
	if !result.Settled {
		t.Fatal("timeout finalization was not settled")
	}
	var outcome, errorKind string
	if err := sqlDB.QueryRow(`SELECT outcome, error_kind FROM check_runs WHERE id = ?`, checkID).Scan(&outcome, &errorKind); err != nil {
		t.Fatalf("query check run: %v", err)
	}
	if outcome != "error" || errorKind != "timeout" {
		t.Fatalf("check outcome = %q/%q, want error/timeout", outcome, errorKind)
	}
}

func TestFinalizeCancellationRechecksGate(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	st, sqlDB := newInternalOutcomeStore(t)
	monitor := createInternalOutcomeMonitor(t, st, "cancel-gate")
	target := monitor.Providers[0]
	checkID, err := st.StartCheckRun(context.Background(), target.ID, target.ConfigVersion, now)
	if err != nil {
		t.Fatalf("StartCheckRun: %v", err)
	}
	attempt := CheckAttempt{
		CheckRunID:    checkID,
		TargetID:      target.ID,
		MonitorID:     monitor.ID,
		ProviderID:    target.ProviderID,
		ConfigVersion: target.ConfigVersion,
		BookingSlug:   target.BookingSlug,
		StartedAt:     now,
		FinishedAt:    now,
		Duration:      time.Second,
	}
	if _, err := sqlDB.Exec(`UPDATE monitor_providers SET active = 0 WHERE id = ?`, target.ID); err != nil {
		t.Fatalf("deactivate target: %v", err)
	}
	runner, err := NewOutcomeRunner(OutcomeRunnerConfig{
		Stage:  &CheckStage{},
		Store:  st,
		Now:    func() time.Time { return now },
		Random: defaultRandom{},
	})
	if err != nil {
		t.Fatalf("NewOutcomeRunner: %v", err)
	}
	result, err := runner.finalizeCancellation(context.Background(), attempt)
	if err != nil {
		t.Fatalf("finalizeCancellation: %v", err)
	}
	if result.Settled {
		t.Fatal("cancellation with invalid target was settled")
	}
	var outcome string
	var errorKind sql.NullString
	if err := sqlDB.QueryRow(`SELECT outcome, error_kind FROM check_runs WHERE id = ?`, checkID).Scan(&outcome, &errorKind); err != nil {
		t.Fatalf("query check run: %v", err)
	}
	if outcome != "discarded" || !errorKind.Valid || errorKind.String != "target_inactive" {
		t.Fatalf("check outcome = %q/%+v, want discarded/target_inactive", outcome, errorKind)
	}
}

func newInternalOutcomeStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	database, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "outcome-gate.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	applied, err := db.Migrate(context.Background(), database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if err := database.Ready(context.Background(), applied); err != nil {
		t.Fatalf("database.Ready: %v", err)
	}
	return store.New(database.DB), database.DB
}

func createInternalOutcomeMonitor(t *testing.T, st *store.Store, id string) domain.Monitor {
	t.Helper()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	monitor, err := st.CreateMonitor(context.Background(), domain.Monitor{
		ID:              id,
		Label:           "Outcome gate",
		OriginName:      "Tehran",
		DestinationName: "Mashhad",
		DepartureDate:   "2026-12-15",
		Adults:          1,
		Enabled:         true,
		ExpiresAt:       now.Add(24 * time.Hour),
		CreatedAt:       now,
		UpdatedAt:       now,
		Providers: []domain.MonitorProvider{{
			MonitorID:     id,
			ProviderID:    "p",
			Active:        true,
			OriginID:      "origin",
			DestinationID: "destination",
			ConfigVersion: 1,
			CreatedAt:     now,
			UpdatedAt:     now,
		}},
	})
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	return monitor
}

func mustExec(t *testing.T, sqlDB *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := sqlDB.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func mustCount(t *testing.T, sqlDB *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return count
}

package domain_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/provider/fake"
)

func insertDetailObservation(t *testing.T, database *sql.DB, targetID int64, observedAt time.Time) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO inventory_states
		(monitor_provider_id, config_version, journey_key, offering_key, state,
		 state_since, last_observed_at, last_decisive_at, updated_at)
		VALUES (?, 1, 'journey-detail', 'offering-detail', 'reservable', ?, ?, ?, ?)`,
		targetID, observedAt.UTC().Format(time.RFC3339Nano),
		observedAt.UTC().Format(time.RFC3339Nano), observedAt.UTC().Format(time.RFC3339Nano),
		observedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert detail observation: %v", err)
	}
}

func insertDetailCheck(t *testing.T, database *sql.DB, targetID int64, startedAt time.Time) {
	t.Helper()
	finished := startedAt.Add(time.Second)
	if _, err := database.Exec(`INSERT INTO check_runs
		(monitor_provider_id, config_version, started_at, finished_at, outcome, created_at)
		VALUES (?, 1, ?, ?, 'success', ?)`,
		targetID, startedAt.UTC().Format(time.RFC3339Nano),
		finished.UTC().Format(time.RFC3339Nano), startedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert detail check: %v", err)
	}
}

func TestMonitorServiceDetailIncludesActiveTargetsOnly(t *testing.T) {
	h := newServiceHarness(t)
	ctx := context.Background()

	input := fakeInput()
	input.Providers = append(input.Providers, domain.ProviderInput{
		ProviderID:    "stub",
		OriginID:      "origin-stub",
		DestinationID: "destination-stub",
	})
	monitor, err := h.svc.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	targets := make(map[provider.ID]int64, len(monitor.Providers))
	for _, target := range monitor.Providers {
		targets[target.ProviderID] = target.ID
	}
	if len(targets) != 2 {
		t.Fatalf("targets = %+v, want two", targets)
	}
	insertDetailObservation(t, h.sqlDB, targets[fake.ID], serviceNow)
	insertDetailCheck(t, h.sqlDB, targets[fake.ID], serviceNow.Add(-time.Minute))
	insertDetailObservation(t, h.sqlDB, targets["stub"], serviceNow.Add(-time.Hour))
	insertDetailCheck(t, h.sqlDB, targets["stub"], serviceNow.Add(-time.Hour))

	// Keep only the fake provider active. The stub target remains as history but
	// its stale diagnostics must not be presented as current.
	update := inputFromMonitor(monitor)
	update.Providers = update.Providers[:0]
	for _, candidate := range inputFromMonitor(monitor).Providers {
		if candidate.ProviderID == fake.ID {
			update.Providers = append(update.Providers, candidate)
		}
	}
	updated, err := h.svc.Update(ctx, monitor.ID, update)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(updated.Providers) != 2 {
		t.Fatalf("updated providers = %+v, want active and inactive history", updated.Providers)
	}

	detail, err := h.svc.Detail(ctx, monitor.ID)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if detail.Monitor.ID != monitor.ID || len(detail.Targets) != 2 {
		t.Fatalf("detail = %+v", detail)
	}
	seen := make(map[provider.ID]domain.MonitorTargetDetail, len(detail.Targets))
	for _, target := range detail.Targets {
		seen[target.ProviderID] = target
	}
	active := seen[fake.ID]
	if !active.Active {
		t.Fatalf("fake target inactive: %+v", active)
	}
	if len(active.Observations) != 1 || len(active.RecentChecks) != 1 {
		t.Fatalf("active diagnostics = %+v", active)
	}
	if active.Observations[0].State != provider.StateReservable ||
		active.Observations[0].JourneyKey != "journey-detail" {
		t.Fatalf("active observation = %+v", active.Observations[0])
	}
	if active.RecentChecks[0].Outcome != domain.CheckRunSuccess {
		t.Fatalf("active check = %+v", active.RecentChecks[0])
	}
	inactive := seen["stub"]
	if inactive.Active {
		t.Fatalf("stub target active: %+v", inactive)
	}
	if inactive.Observations == nil || len(inactive.Observations) != 0 ||
		inactive.RecentChecks == nil || len(inactive.RecentChecks) != 0 {
		t.Fatalf("inactive diagnostics were exposed: %+v", inactive)
	}
}

func TestMonitorServiceDetailMissingMonitor(t *testing.T) {
	h := newServiceHarness(t)
	if _, err := h.svc.Detail(context.Background(), "missing"); !errors.Is(err, domain.ErrMonitorNotFound) {
		t.Fatalf("Detail missing error = %v, want ErrMonitorNotFound", err)
	}
}

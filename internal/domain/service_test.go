package domain_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/provider/fake"
	"github.com/am-fallah/neda/internal/store"
)

var serviceNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

type acceptingProvider struct {
	id provider.ID
}

func (p acceptingProvider) Metadata() provider.Metadata {
	return provider.Metadata{
		ID:          p.id,
		DisplayName: "Accepting test provider",
		TestOnly:    true,
		Capabilities: provider.Capabilities{
			RequiresOriginID:      true,
			RequiresDestinationID: true,
			SupportsBookingURL:    true,
			SupportsGender:        true,
			MaxPassengers:         9,
		},
	}
}

func (p acceptingProvider) ValidateConfig(cfg provider.MonitorProviderConfig) error {
	if strings.TrimSpace(cfg.OriginID) == "" || strings.TrimSpace(cfg.DestinationID) == "" {
		return errors.New("route ids are required")
	}
	if cfg.OriginID == cfg.DestinationID {
		return errors.New("route ids must differ")
	}
	return nil
}

func (p acceptingProvider) Check(context.Context, provider.CheckRequest) (provider.Result, error) {
	return provider.Result{}, errors.New("accepting provider is only used for monitor tests")
}

type serviceHarness struct {
	svc       *domain.MonitorService
	store     *store.Store
	sqlDB     *sql.DB
	wakeCount int
	wakeCheck func()
	nextID    int
}

func newServiceHarness(t *testing.T) *serviceHarness {
	t.Helper()
	database, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "domain-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("database.Close: %v", err)
		}
	})
	applied, err := db.Migrate(context.Background(), database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if err := database.Ready(context.Background(), applied); err != nil {
		t.Fatalf("database.Ready: %v", err)
	}
	location, err := domain.TehranLocation()
	if err != nil {
		t.Fatalf("TehranLocation: %v", err)
	}
	registry, err := provider.NewRegistry(fake.New(), acceptingProvider{id: "stub"})
	if err != nil {
		t.Fatalf("provider.NewRegistry: %v", err)
	}
	h := &serviceHarness{store: store.New(database.DB), sqlDB: database.DB}
	h.svc, err = domain.NewMonitorService(h.store, registry, domain.MonitorServiceConfig{
		Now:      func() time.Time { return serviceNow },
		Location: location,
		NearTermSchedule: func(now time.Time) time.Time {
			return now.Add(2 * time.Second)
		},
		NewID: func() (string, error) {
			h.nextID++
			return fmt.Sprintf("monitor-%d", h.nextID), nil
		},
		Wake: func() {
			h.wakeCount++
			if h.wakeCheck != nil {
				h.wakeCheck()
			}
		},
	})
	if err != nil {
		t.Fatalf("NewMonitorService: %v", err)
	}
	return h
}

func fakeInput() domain.MonitorInput {
	return domain.MonitorInput{
		Label:           "Tehran to Mashhad",
		OriginName:      "Tehran",
		DestinationName: "Mashhad",
		DepartureDate:   "2026-10-30",
		Adults:          1,
		Children:        1,
		Infants:         0,
		Providers: []domain.ProviderInput{{
			ProviderID:    fake.ID,
			OriginID:      fake.OriginAvailable,
			DestinationID: fake.DestinationDefault,
		}},
	}
}

func stubInput() domain.MonitorInput {
	input := fakeInput()
	input.Providers = []domain.ProviderInput{{
		ProviderID:    "stub",
		OriginID:      "origin-1",
		DestinationID: "destination-1",
	}}
	return input
}

func inputFromMonitor(monitor domain.Monitor) domain.MonitorInput {
	input := domain.MonitorInput{
		Label:           monitor.Label,
		OriginName:      monitor.OriginName,
		DestinationName: monitor.DestinationName,
		DepartureDate:   monitor.DepartureDate,
		Adults:          monitor.Adults,
		Children:        monitor.Children,
		Infants:         monitor.Infants,
	}
	for _, target := range monitor.Providers {
		if !target.Active {
			continue
		}
		input.Providers = append(input.Providers, domain.ProviderInput{
			ProviderID:    target.ProviderID,
			OriginID:      target.OriginID,
			DestinationID: target.DestinationID,
			BookingSlug:   target.BookingSlug,
			GenderCode:    target.GenderCode,
		})
	}
	return input
}

func TestMonitorServiceCreateGetListPauseResumeDelete(t *testing.T) {
	h := newServiceHarness(t)
	ctx := context.Background()
	created, err := h.svc.Create(ctx, fakeInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID != "monitor-1" || !created.Enabled {
		t.Fatalf("created = %+v", created)
	}
	if created.Status(serviceNow) != domain.StatusActive {
		t.Fatalf("status = %q", created.Status(serviceNow))
	}
	if len(created.Providers) != 1 || created.Providers[0].ConfigVersion != 1 {
		t.Fatalf("providers = %+v", created.Providers)
	}
	wantDue := serviceNow.Add(2 * time.Second)
	if created.Providers[0].NextDueAt == nil || !created.Providers[0].NextDueAt.Equal(wantDue) {
		t.Fatalf("next due = %v, want %v", created.Providers[0].NextDueAt, wantDue)
	}
	if h.wakeCount != 1 {
		t.Fatalf("wake count after create = %d", h.wakeCount)
	}

	if got, err := h.svc.Get(ctx, created.ID); err != nil || got.Label != created.Label {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if list, err := h.svc.List(ctx); err != nil || len(list) != 1 {
		t.Fatalf("List = %+v, %v", list, err)
	}

	paused, err := h.svc.Pause(ctx, created.ID)
	if err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if paused.Enabled || paused.Providers[0].NextDueAt != nil || paused.Providers[0].ConfigVersion != 1 {
		t.Fatalf("paused = %+v", paused)
	}
	if _, err := h.svc.Pause(ctx, created.ID); err != nil {
		t.Fatalf("Pause idempotent: %v", err)
	}

	resumed, err := h.svc.Resume(ctx, created.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if !resumed.Enabled || resumed.Providers[0].NextDueAt == nil || !resumed.Providers[0].NextDueAt.Equal(wantDue) {
		t.Fatalf("resumed = %+v", resumed)
	}
	if _, err := h.svc.Resume(ctx, created.ID); err != nil {
		t.Fatalf("Resume idempotent: %v", err)
	}

	if err := h.svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := h.svc.Get(ctx, created.ID); !errors.Is(err, domain.ErrMonitorNotFound) {
		t.Fatalf("Get after delete = %v", err)
	}
}

func TestMonitorServiceLabelAndBookingOnlyDoNotReset(t *testing.T) {
	h := newServiceHarness(t)
	ctx := context.Background()
	created, err := h.svc.Create(ctx, stubInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	initialDue := *created.Providers[0].NextDueAt
	h.wakeCount = 0

	labelInput := inputFromMonitor(created)
	labelInput.Label = "Renamed"
	updated, err := h.svc.Update(ctx, created.ID, labelInput)
	if err != nil {
		t.Fatalf("Update label: %v", err)
	}
	if updated.Providers[0].ConfigVersion != 1 || updated.Providers[0].NextDueAt == nil || !updated.Providers[0].NextDueAt.Equal(initialDue) {
		t.Fatalf("label update changed state: %+v", updated.Providers[0])
	}
	if h.wakeCount != 0 {
		t.Fatalf("label update woke scheduler %d times", h.wakeCount)
	}

	bookingInput := inputFromMonitor(updated)
	bookingInput.Providers[0].BookingSlug = "opaque-slug"
	updated, err = h.svc.Update(ctx, created.ID, bookingInput)
	if err != nil {
		t.Fatalf("Update booking: %v", err)
	}
	if updated.Providers[0].ConfigVersion != 1 || updated.Providers[0].BookingSlug != "opaque-slug" {
		t.Fatalf("booking update = %+v", updated.Providers[0])
	}
	if updated.Providers[0].NextDueAt == nil || !updated.Providers[0].NextDueAt.Equal(initialDue) {
		t.Fatalf("booking update changed next due: %v", updated.Providers[0].NextDueAt)
	}
	if h.wakeCount != 0 {
		t.Fatalf("booking update woke scheduler %d times", h.wakeCount)
	}
}

func TestMonitorServiceMaterialEditClosesEpisodesAndWakesAfterCommit(t *testing.T) {
	h := newServiceHarness(t)
	ctx := context.Background()
	created, err := h.svc.Create(ctx, fakeInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	targetID := created.Providers[0].ID
	episodeID := insertEpisode(t, h.sqlDB, targetID, created.Providers[0].ConfigVersion)
	wantDue := serviceNow.Add(2 * time.Second)

	wakeObserved := false
	h.wakeCount = 0
	h.wakeCheck = func() {
		monitor, err := h.store.GetMonitor(context.Background(), created.ID)
		if err != nil {
			t.Errorf("wake GetMonitor: %v", err)
			return
		}
		if monitor.Providers[0].ConfigVersion != 2 {
			t.Errorf("wake saw config version %d, want 2", monitor.Providers[0].ConfigVersion)
			return
		}
		if monitor.Providers[0].NextDueAt == nil || !monitor.Providers[0].NextDueAt.Equal(wantDue) {
			t.Errorf("wake saw next due %v, want %v", monitor.Providers[0].NextDueAt, wantDue)
			return
		}
		wakeObserved = true
	}
	input := inputFromMonitor(created)
	input.Adults = 2
	updated, err := h.svc.Update(ctx, created.ID, input)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !wakeObserved || h.wakeCount != 1 {
		t.Fatalf("wake observed=%v count=%d", wakeObserved, h.wakeCount)
	}
	if updated.Providers[0].ConfigVersion != 2 || updated.Providers[0].NextDueAt == nil || !updated.Providers[0].NextDueAt.Equal(wantDue) {
		t.Fatalf("updated provider = %+v", updated.Providers[0])
	}
	assertEpisodeClosed(t, h.sqlDB, episodeID)
	var events int
	if err := h.sqlDB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 0 {
		t.Fatalf("material edit created %d events", events)
	}
}

func TestMonitorServiceExpiredResumeRequiresFutureDate(t *testing.T) {
	h := newServiceHarness(t)
	ctx := context.Background()
	created, err := h.svc.Create(ctx, fakeInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := h.svc.Pause(ctx, created.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if _, err := h.sqlDB.Exec(
		`UPDATE monitors SET expires_at = ?, expired_at = ? WHERE id = ?`,
		serviceNow.Add(-time.Hour).Format(time.RFC3339Nano),
		serviceNow.Add(-time.Minute).Format(time.RFC3339Nano),
		created.ID,
	); err != nil {
		t.Fatalf("mark expired: %v", err)
	}
	if _, err := h.svc.Resume(ctx, created.ID); !errors.Is(err, domain.ErrMonitorExpired) {
		t.Fatalf("Resume expired = %v", err)
	}

	current, err := h.svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	input := inputFromMonitor(current)
	input.DepartureDate = "2026-11-05"
	updated, err := h.svc.Update(ctx, created.ID, input)
	if err != nil {
		t.Fatalf("Update future date: %v", err)
	}
	if updated.Enabled || updated.ExpiredAt != nil {
		t.Fatalf("paused date edit = %+v", updated)
	}
	for _, target := range updated.Providers {
		if target.NextDueAt != nil {
			t.Fatalf("paused target next due = %v", target.NextDueAt)
		}
	}
	resumed, err := h.svc.Resume(ctx, created.ID)
	if err != nil {
		t.Fatalf("Resume after date edit: %v", err)
	}
	if !resumed.Enabled || resumed.Providers[0].NextDueAt == nil {
		t.Fatalf("resumed = %+v", resumed)
	}
}

func TestMonitorServiceValidationRejectsInvalidInput(t *testing.T) {
	h := newServiceHarness(t)
	ctx := context.Background()
	tests := []struct {
		name  string
		input func() domain.MonitorInput
	}{
		{"blank label", func() domain.MonitorInput { in := fakeInput(); in.Label = " "; return in }},
		{"too long label", func() domain.MonitorInput {
			in := fakeInput()
			in.Label = strings.Repeat("x", domain.MaxLabelRunes+1)
			return in
		}},
		{"blank origin", func() domain.MonitorInput { in := fakeInput(); in.OriginName = ""; return in }},
		{"same names", func() domain.MonitorInput { in := fakeInput(); in.DestinationName = " tehran "; return in }},
		{"past date", func() domain.MonitorInput { in := fakeInput(); in.DepartureDate = "2026-10-23"; return in }},
		{"zero adults", func() domain.MonitorInput { in := fakeInput(); in.Adults = 0; return in }},
		{"too many children", func() domain.MonitorInput { in := fakeInput(); in.Children = domain.MaxChildren + 1; return in }},
		{"total passengers", func() domain.MonitorInput {
			in := fakeInput()
			in.Adults = 8
			in.Children = 1
			in.Infants = 1
			return in
		}},
		{"no providers", func() domain.MonitorInput { in := fakeInput(); in.Providers = nil; return in }},
		{"duplicate providers", func() domain.MonitorInput {
			in := fakeInput()
			in.Providers = append(in.Providers, in.Providers[0])
			return in
		}},
		{"unknown provider", func() domain.MonitorInput {
			in := fakeInput()
			in.Providers[0].ProviderID = "missing"
			return in
		}},
		{"fake route IDs", func() domain.MonitorInput {
			in := fakeInput()
			in.Providers[0].OriginID = "1"
			in.Providers[0].DestinationID = "2"
			return in
		}},
		{"same route IDs", func() domain.MonitorInput {
			in := fakeInput()
			in.Providers[0].DestinationID = in.Providers[0].OriginID
			return in
		}},
		{"fake booking slug", func() domain.MonitorInput {
			in := fakeInput()
			in.Providers[0].BookingSlug = "slug"
			return in
		}},
		{"oversized booking slug", func() domain.MonitorInput {
			in := stubInput()
			in.Providers[0].BookingSlug = strings.Repeat("x", domain.MaxBookingSlugLength+1)
			return in
		}},
		{"control in gender", func() domain.MonitorInput {
			in := stubInput()
			in.Providers[0].GenderCode = "M\x00"
			return in
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := h.svc.Create(ctx, test.input()); !errors.Is(err, domain.ErrMonitorInvalid) {
				t.Fatalf("Create error = %v, want ErrMonitorInvalid", err)
			}
		})
	}
	var monitors int
	if err := h.sqlDB.QueryRow(`SELECT COUNT(*) FROM monitors`).Scan(&monitors); err != nil {
		t.Fatalf("count monitors: %v", err)
	}
	if monitors != 0 {
		t.Fatalf("invalid creates persisted %d monitors", monitors)
	}
}

func TestMonitorServiceConstructorValidation(t *testing.T) {
	h := newServiceHarness(t)
	if _, err := domain.NewMonitorService(nil, nil, domain.MonitorServiceConfig{}); err == nil {
		t.Fatal("NewMonitorService(nil repo) succeeded")
	}
	registry, err := provider.NewRegistry(fake.New())
	if err != nil {
		t.Fatalf("provider.NewRegistry: %v", err)
	}
	if _, err := domain.NewMonitorService(h.store, nil, domain.MonitorServiceConfig{}); err == nil {
		t.Fatal("NewMonitorService(nil providers) succeeded")
	}

	clamped, err := domain.NewMonitorService(h.store, registry, domain.MonitorServiceConfig{
		Now:      func() time.Time { return serviceNow },
		Location: mustTehran(t),
		NearTermSchedule: func(time.Time) time.Time {
			return serviceNow.Add(-time.Hour)
		},
		NewID: func() (string, error) { return "clamped-monitor", nil },
	})
	if err != nil {
		t.Fatalf("NewMonitorService(clamped schedule): %v", err)
	}
	created, err := clamped.Create(context.Background(), fakeInput())
	if err != nil {
		t.Fatalf("Create(clamped schedule): %v", err)
	}
	if created.Providers[0].NextDueAt == nil || !created.Providers[0].NextDueAt.Equal(serviceNow) {
		t.Fatalf("clamped next due = %v, want %v", created.Providers[0].NextDueAt, serviceNow)
	}
}

func mustTehran(t *testing.T) *time.Location {
	t.Helper()
	location, err := domain.TehranLocation()
	if err != nil {
		t.Fatalf("TehranLocation: %v", err)
	}
	return location
}

func insertEpisode(t *testing.T, db *sql.DB, targetID, configVersion int64) int64 {
	t.Helper()
	result, err := db.Exec(`INSERT INTO inventory_states
		(monitor_provider_id, config_version, journey_key, offering_key, state, state_since, last_observed_at, updated_at)
		VALUES (?, ?, 'journey', 'offering', 'reservable', 'now', 'now', 'now')`,
		targetID, configVersion,
	)
	if err != nil {
		t.Fatalf("insert inventory state: %v", err)
	}
	stateID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("state id: %v", err)
	}
	result, err = db.Exec(`INSERT INTO availability_episodes (state_id, episode_seq, opened_at) VALUES (?, 1, 'now')`, stateID)
	if err != nil {
		t.Fatalf("insert episode: %v", err)
	}
	episodeID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("episode id: %v", err)
	}
	return episodeID
}

func assertEpisodeClosed(t *testing.T, db *sql.DB, episodeID int64) {
	t.Helper()
	var closedAt sql.NullString
	var reason sql.NullString
	if err := db.QueryRow(`SELECT closed_at, close_reason FROM availability_episodes WHERE id = ?`, episodeID).Scan(&closedAt, &reason); err != nil {
		t.Fatalf("read episode: %v", err)
	}
	if !closedAt.Valid || !reason.Valid || reason.String != "config_changed" {
		t.Fatalf("episode closed_at/reason = %v/%v", closedAt, reason)
	}
}

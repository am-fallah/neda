package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
)

func storeMonitorFixture(t *testing.T) domain.Monitor {
	t.Helper()
	location, err := domain.TehranLocation()
	if err != nil {
		t.Fatalf("TehranLocation: %v", err)
	}
	expiry, err := domain.DepartureExpiry("2026-10-30", location)
	if err != nil {
		t.Fatalf("DepartureExpiry: %v", err)
	}
	due := testNow.Add(time.Minute)
	return domain.Monitor{
		ID:              "monitor-1",
		Label:           "Tehran to Mashhad",
		OriginName:      "Tehran",
		DestinationName: "Mashhad",
		DepartureDate:   "2026-10-30",
		Adults:          1,
		Children:        1,
		Infants:         0,
		Enabled:         true,
		ExpiresAt:       expiry,
		CreatedAt:       testNow,
		UpdatedAt:       testNow,
		Providers: []domain.MonitorProvider{
			{
				MonitorID:     "monitor-1",
				ProviderID:    "fake",
				Active:        true,
				OriginID:      "origin-a",
				DestinationID: "destination-a",
				ConfigVersion: 1,
				NextDueAt:     &due,
				CreatedAt:     testNow,
				UpdatedAt:     testNow,
			},
			{
				MonitorID:     "monitor-1",
				ProviderID:    "other",
				Active:        true,
				OriginID:      "origin-b",
				DestinationID: "destination-b",
				ConfigVersion: 1,
				NextDueAt:     &due,
				CreatedAt:     testNow,
				UpdatedAt:     testNow,
			},
		},
	}
}

func storeInput(monitor domain.Monitor) domain.MonitorInput {
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

func TestMonitorRepositoryCRUDAndCascade(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()
	fixture := storeMonitorFixture(t)

	created, err := st.CreateMonitor(ctx, fixture)
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	if created.ID != fixture.ID || len(created.Providers) != 2 {
		t.Fatalf("created monitor = %+v", created)
	}
	for _, target := range created.Providers {
		if target.ID == 0 || !target.Active || target.ConfigVersion != 1 {
			t.Fatalf("created provider = %+v", target)
		}
	}
	got, err := st.GetMonitor(ctx, fixture.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if got.Label != fixture.Label || !got.ExpiresAt.Equal(fixture.ExpiresAt) {
		t.Fatalf("got monitor = %+v", got)
	}
	monitors, err := st.ListMonitors(ctx)
	if err != nil {
		t.Fatalf("ListMonitors: %v", err)
	}
	if len(monitors) != 1 || monitors[0].ID != fixture.ID {
		t.Fatalf("monitors = %+v", monitors)
	}

	paused, err := st.PauseMonitor(ctx, fixture.ID, testNow.Add(time.Hour))
	if err != nil {
		t.Fatalf("PauseMonitor: %v", err)
	}
	if paused.Enabled {
		t.Fatal("PauseMonitor left enabled")
	}
	for _, target := range paused.Providers {
		if target.NextDueAt != nil {
			t.Fatalf("paused target next due = %v", target.NextDueAt)
		}
		if target.ConfigVersion != 1 {
			t.Fatalf("pause bumped config version to %d", target.ConfigVersion)
		}
	}
	if _, err := st.PauseMonitor(ctx, fixture.ID, testNow.Add(2*time.Hour)); err != nil {
		t.Fatalf("PauseMonitor idempotent: %v", err)
	}

	scheduleAt := testNow.Add(3 * time.Hour)
	resumed, err := st.ResumeMonitor(ctx, fixture.ID, testNow.Add(3*time.Hour), scheduleAt)
	if err != nil {
		t.Fatalf("ResumeMonitor: %v", err)
	}
	if !resumed.Enabled {
		t.Fatal("ResumeMonitor left paused")
	}
	for _, target := range resumed.Providers {
		if target.NextDueAt == nil || !target.NextDueAt.Equal(scheduleAt) {
			t.Fatalf("resumed target next due = %v, want %v", target.NextDueAt, scheduleAt)
		}
	}

	if err := st.DeleteMonitor(ctx, fixture.ID); err != nil {
		t.Fatalf("DeleteMonitor: %v", err)
	}
	if _, err := st.GetMonitor(ctx, fixture.ID); !errors.Is(err, domain.ErrMonitorNotFound) {
		t.Fatalf("GetMonitor after delete = %v", err)
	}
	var count int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM monitor_providers`).Scan(&count); err != nil {
		t.Fatalf("count providers: %v", err)
	}
	if count != 0 {
		t.Fatalf("providers after delete = %d", count)
	}
	if err := st.DeleteMonitor(ctx, fixture.ID); !errors.Is(err, domain.ErrMonitorNotFound) {
		t.Fatalf("second DeleteMonitor = %v", err)
	}
}

func TestMonitorUpdateMaterialClosesEpisodesAndSchedules(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()
	location, err := domain.TehranLocation()
	if err != nil {
		t.Fatalf("TehranLocation: %v", err)
	}
	fixture := storeMonitorFixture(t)
	fixture.Providers = fixture.Providers[:1]
	created, err := st.CreateMonitor(ctx, fixture)
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	targetID := created.Providers[0].ID
	episodeID := insertOpenEpisode(t, sqlDB, targetID, created.Providers[0].ConfigVersion)

	input := storeInput(created)
	input.Adults = 2
	now := testNow.Add(time.Hour)
	scheduleAt := now.Add(2 * time.Second)
	result, err := st.UpdateMonitor(ctx, created.ID, input, now, scheduleAt, location)
	if err != nil {
		t.Fatalf("UpdateMonitor: %v", err)
	}
	if !result.Material {
		t.Fatal("material update not reported")
	}
	if result.Monitor.Adults != 2 || result.Monitor.Providers[0].ConfigVersion != 2 {
		t.Fatalf("updated monitor = %+v", result.Monitor)
	}
	if result.Monitor.Providers[0].NextDueAt == nil || !result.Monitor.Providers[0].NextDueAt.Equal(scheduleAt) {
		t.Fatalf("next due = %v, want %v", result.Monitor.Providers[0].NextDueAt, scheduleAt)
	}
	assertClosedEpisode(t, sqlDB, episodeID, "config_changed")
	var eventCount int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("material edit created %d events", eventCount)
	}

	labelInput := storeInput(result.Monitor)
	labelInput.Label = "Renamed monitor"
	labelResult, err := st.UpdateMonitor(ctx, created.ID, labelInput, now.Add(time.Minute), now.Add(time.Minute+time.Second), location)
	if err != nil {
		t.Fatalf("label UpdateMonitor: %v", err)
	}
	if labelResult.Material {
		t.Fatal("label-only update was material")
	}
	if labelResult.Monitor.Providers[0].ConfigVersion != 2 {
		t.Fatalf("label update version = %d", labelResult.Monitor.Providers[0].ConfigVersion)
	}
	if labelResult.Monitor.Providers[0].NextDueAt == nil || !labelResult.Monitor.Providers[0].NextDueAt.Equal(scheduleAt) {
		t.Fatalf("label update changed next due to %v", labelResult.Monitor.Providers[0].NextDueAt)
	}

	bookingInput := storeInput(labelResult.Monitor)
	bookingInput.Providers[0].BookingSlug = "opaque-slug"
	bookingResult, err := st.UpdateMonitor(ctx, created.ID, bookingInput, now.Add(2*time.Minute), now.Add(2*time.Minute+time.Second), location)
	if err != nil {
		t.Fatalf("booking UpdateMonitor: %v", err)
	}
	if bookingResult.Material || bookingResult.Monitor.Providers[0].ConfigVersion != 2 {
		t.Fatalf("booking-only update = %+v", bookingResult)
	}
	if bookingResult.Monitor.Providers[0].BookingSlug != "opaque-slug" {
		t.Fatalf("booking slug = %q", bookingResult.Monitor.Providers[0].BookingSlug)
	}
}

func TestMonitorProviderRemovalAndReAdd(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()
	location, err := domain.TehranLocation()
	if err != nil {
		t.Fatalf("TehranLocation: %v", err)
	}
	created, err := st.CreateMonitor(ctx, storeMonitorFixture(t))
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	removedTargetID := created.Providers[1].ID
	episodeID := insertOpenEpisode(t, sqlDB, removedTargetID, created.Providers[1].ConfigVersion)

	input := storeInput(created)
	input.Providers = input.Providers[:1]
	now := testNow.Add(time.Hour)
	result, err := st.UpdateMonitor(ctx, created.ID, input, now, now.Add(time.Second), location)
	if err != nil {
		t.Fatalf("UpdateMonitor removal: %v", err)
	}
	if !result.Material || len(result.Monitor.Providers) != 2 {
		t.Fatalf("removal result = %+v", result)
	}
	removed := result.Monitor.Providers[1]
	if removed.Active || removed.ConfigVersion != 2 || removed.NextDueAt != nil {
		t.Fatalf("removed provider = %+v", removed)
	}
	assertClosedEpisode(t, sqlDB, episodeID, "config_changed")

	readdInput := storeInput(result.Monitor)
	readdInput.Providers = append(readdInput.Providers, domain.ProviderInput{
		ProviderID:    removed.ProviderID,
		OriginID:      removed.OriginID,
		DestinationID: removed.DestinationID,
	})
	result, err = st.UpdateMonitor(ctx, created.ID, readdInput, now.Add(time.Minute), now.Add(time.Minute+time.Second), location)
	if err != nil {
		t.Fatalf("UpdateMonitor re-add: %v", err)
	}
	if !result.Material {
		t.Fatal("re-add was not material")
	}
	readded := result.Monitor.Providers[1]
	if !readded.Active || readded.ConfigVersion != 3 || readded.NextDueAt == nil {
		t.Fatalf("re-added provider = %+v", readded)
	}
}

func TestMonitorPausedDateEditAndExpiredResume(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()
	location, err := domain.TehranLocation()
	if err != nil {
		t.Fatalf("TehranLocation: %v", err)
	}
	created, err := st.CreateMonitor(ctx, storeMonitorFixture(t))
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	if _, err := st.PauseMonitor(ctx, created.ID, testNow); err != nil {
		t.Fatalf("PauseMonitor: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx,
		`UPDATE monitors SET expires_at = ?, expired_at = ? WHERE id = ?`,
		formatTestTime(testNow.Add(-time.Hour)), formatTestTime(testNow.Add(-time.Minute)), created.ID,
	); err != nil {
		t.Fatalf("mark expired: %v", err)
	}
	if _, err := st.ResumeMonitor(ctx, created.ID, testNow, testNow.Add(time.Second)); !errors.Is(err, domain.ErrMonitorExpired) {
		t.Fatalf("ResumeMonitor expired = %v, want ErrMonitorExpired", err)
	}

	current, err := st.GetMonitor(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	input := storeInput(current)
	input.DepartureDate = "2026-11-05"
	result, err := st.UpdateMonitor(ctx, created.ID, input, testNow, testNow.Add(2*time.Second), location)
	if err != nil {
		t.Fatalf("UpdateMonitor future date: %v", err)
	}
	if result.Monitor.Enabled {
		t.Fatal("paused monitor became enabled on date edit")
	}
	if result.Monitor.ExpiredAt != nil {
		t.Fatalf("ExpiredAt = %v, want nil", result.Monitor.ExpiredAt)
	}
	for _, target := range result.Monitor.Providers {
		if target.NextDueAt != nil {
			t.Fatalf("paused target next due = %v", target.NextDueAt)
		}
	}
	resumed, err := st.ResumeMonitor(ctx, created.ID, testNow.Add(3*time.Second), testNow.Add(5*time.Second))
	if err != nil {
		t.Fatalf("ResumeMonitor after date edit: %v", err)
	}
	if !resumed.Enabled {
		t.Fatal("monitor still paused after resume")
	}
	for _, target := range resumed.Providers {
		if target.NextDueAt == nil || !target.NextDueAt.Equal(testNow.Add(5*time.Second)) {
			t.Fatalf("resumed next due = %v", target.NextDueAt)
		}
	}
}

func TestCreateMonitorRollsBackOnProviderConflict(t *testing.T) {
	st, sqlDB := newTestStore(t)
	fixture := storeMonitorFixture(t)
	fixture.Providers[1].ProviderID = fixture.Providers[0].ProviderID
	if _, err := st.CreateMonitor(context.Background(), fixture); err == nil {
		t.Fatal("CreateMonitor with duplicate provider succeeded")
	}
	var monitors, providers int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM monitors`).Scan(&monitors); err != nil {
		t.Fatalf("count monitors: %v", err)
	}
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM monitor_providers`).Scan(&providers); err != nil {
		t.Fatalf("count providers: %v", err)
	}
	if monitors != 0 || providers != 0 {
		t.Fatalf("partial create: monitors=%d providers=%d", monitors, providers)
	}
}

func TestUpdateMonitorInvalidDateRollsBack(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	location, err := domain.TehranLocation()
	if err != nil {
		t.Fatalf("TehranLocation: %v", err)
	}
	created, err := st.CreateMonitor(ctx, storeMonitorFixture(t))
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	input := storeInput(created)
	input.Label = "should not persist"
	input.DepartureDate = "2026-10-01"
	if _, err := st.UpdateMonitor(ctx, created.ID, input, testNow, testNow, location); !errors.Is(err, domain.ErrMonitorInvalid) {
		t.Fatalf("UpdateMonitor past date = %v, want ErrMonitorInvalid", err)
	}
	got, err := st.GetMonitor(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if got.Label != created.Label || got.DepartureDate != created.DepartureDate {
		t.Fatalf("partial update persisted: %+v", got)
	}
}

func TestMonitorAddProviderInsertsActiveTarget(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	location, err := domain.TehranLocation()
	if err != nil {
		t.Fatalf("TehranLocation: %v", err)
	}
	fixture := storeMonitorFixture(t)
	fixture.Providers = fixture.Providers[:1]
	created, err := st.CreateMonitor(ctx, fixture)
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	input := storeInput(created)
	input.Providers = append(input.Providers, domain.ProviderInput{
		ProviderID:    "new",
		OriginID:      "origin-new",
		DestinationID: "destination-new",
	})
	result, err := st.UpdateMonitor(ctx, created.ID, input, testNow.Add(time.Hour), testNow.Add(time.Hour+time.Second), location)
	if err != nil {
		t.Fatalf("UpdateMonitor: %v", err)
	}
	if !result.Material || len(result.Monitor.Providers) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if result.Monitor.Providers[0].ConfigVersion != 1 {
		t.Fatalf("existing version = %d", result.Monitor.Providers[0].ConfigVersion)
	}
	newTarget := result.Monitor.Providers[1]
	if newTarget.ID == 0 || !newTarget.Active || newTarget.ConfigVersion != 1 || newTarget.ProviderID != "new" {
		t.Fatalf("new target = %+v", newTarget)
	}
	if newTarget.NextDueAt == nil || !newTarget.NextDueAt.Equal(testNow.Add(time.Hour+time.Second)) {
		t.Fatalf("new target next due = %v", newTarget.NextDueAt)
	}
}

func TestMonitorMaterialEditDoesNotScheduleExpiredMonitor(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()
	location, err := domain.TehranLocation()
	if err != nil {
		t.Fatalf("TehranLocation: %v", err)
	}
	created, err := st.CreateMonitor(ctx, storeMonitorFixture(t))
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx,
		`UPDATE monitors SET expires_at = ?, expired_at = ? WHERE id = ?`,
		formatTestTime(testNow.Add(-time.Hour)), formatTestTime(testNow.Add(-time.Minute)), created.ID,
	); err != nil {
		t.Fatalf("mark expired: %v", err)
	}
	input := storeInput(created)
	input.Adults = 2
	result, err := st.UpdateMonitor(ctx, created.ID, input, testNow, testNow.Add(time.Second), location)
	if err != nil {
		t.Fatalf("UpdateMonitor: %v", err)
	}
	if !result.Material {
		t.Fatal("expired material edit was not material")
	}
	for _, target := range result.Monitor.Providers {
		if target.NextDueAt != nil {
			t.Fatalf("expired target next due = %v, want nil", target.NextDueAt)
		}
	}
}

func TestMonitorNotFound(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	if _, err := st.GetMonitor(ctx, "missing"); !errors.Is(err, domain.ErrMonitorNotFound) {
		t.Fatalf("GetMonitor = %v", err)
	}
	if _, err := st.PauseMonitor(ctx, "missing", testNow); !errors.Is(err, domain.ErrMonitorNotFound) {
		t.Fatalf("PauseMonitor = %v", err)
	}
	if _, err := st.ResumeMonitor(ctx, "missing", testNow, testNow); !errors.Is(err, domain.ErrMonitorNotFound) {
		t.Fatalf("ResumeMonitor = %v", err)
	}
	if err := st.DeleteMonitor(ctx, "missing"); !errors.Is(err, domain.ErrMonitorNotFound) {
		t.Fatalf("DeleteMonitor = %v", err)
	}
}

func insertOpenEpisode(t *testing.T, db *sql.DB, targetID int64, configVersion int64) int64 {
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
		t.Fatalf("inventory state id: %v", err)
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

func assertClosedEpisode(t *testing.T, db *sql.DB, episodeID int64, reason string) {
	t.Helper()
	var closedAt sql.NullString
	var closeReason sql.NullString
	if err := db.QueryRow(`SELECT closed_at, close_reason FROM availability_episodes WHERE id = ?`, episodeID).Scan(&closedAt, &closeReason); err != nil {
		t.Fatalf("read episode: %v", err)
	}
	if !closedAt.Valid || !closeReason.Valid || closeReason.String != reason {
		t.Fatalf("episode closed_at/reason = %v/%v, want set/%q", closedAt, closeReason, reason)
	}
}

func formatTestTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

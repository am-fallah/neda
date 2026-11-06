package scheduler_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

func TestSchedulerPersistsAndNotifiesConfirmedUnavailability(t *testing.T) {
	adapter := newRecordingProvider("unavailable-p", nil)
	h := newIntegrationHarness(t, adapter)
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		return notReservableResult(h.clock.Now().UTC(), "unavailable"), nil
	}
	h.createMonitor("integration-unavailable", true, schedulerNow.Add(24*time.Hour), harnessTargetFor("unavailable-p"))
	target := h.target("integration-unavailable", "unavailable-p")

	if err := h.st.SetUnavailableNotifyAfter(context.Background(), time.Hour, h.clock.Now().UTC()); err != nil {
		t.Fatalf("SetUnavailableNotifyAfter: %v", err)
	}

	for elapsed := time.Duration(0); elapsed < time.Hour; elapsed += 10 * time.Minute {
		if _, err := h.runFor("integration-unavailable", "unavailable-p"); err != nil {
			t.Fatalf("run at %v: %v", elapsed, err)
		}
		if got := h.count(`SELECT COUNT(*) FROM events`); got != 0 {
			t.Fatalf("events before grace = %d, want 0", got)
		}
		h.clock.Advance(10 * time.Minute)
	}
	if _, err := h.runFor("integration-unavailable", "unavailable-p"); err != nil {
		t.Fatalf("run at grace: %v", err)
	}

	var kind string
	var silent int
	var episodeID sql.NullInt64
	if err := h.sqlDB.QueryRow(`SELECT kind, silent, episode_id FROM events`).Scan(&kind, &silent, &episodeID); err != nil {
		t.Fatalf("load unavailable event: %v", err)
	}
	if kind != "unavailable" || silent != 1 || episodeID.Valid {
		t.Fatalf("event = %s/silent=%d/episode=%v", kind, silent, episodeID)
	}
	if got := h.count(`SELECT COUNT(*) FROM notification_deliveries`); got != 2 {
		t.Fatalf("deliveries = %d, want Telegram and Push", got)
	}
	if got := h.latestCheck(target.ID); got.Outcome != "success" || got.NotReservable != 1 {
		t.Fatalf("check = %+v, want success/not_reservable", got)
	}

	// A restart does not lose the notified marker or create another event.
	h.reopen()
	if _, err := h.runFor("integration-unavailable", "unavailable-p"); err != nil {
		t.Fatalf("run after reopen: %v", err)
	}
	if got := h.count(`SELECT COUNT(*) FROM events`); got != 1 {
		t.Fatalf("events after restart = %d, want 1", got)
	}
}

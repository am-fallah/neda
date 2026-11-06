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

func TestEventWriterSaturationIsBoundedAndCounted(t *testing.T) {
	writer := NewEventWriter(nil, nil)
	for index := 0; index < rateLimitEventBuffer+25; index++ {
		writer.RecordRateLimitEvent(store.ProviderRateLimitEvent{
			ProviderID: "p", Kind: store.RateLimitEventRateLimited,
			OccurredAt: time.Now().UTC(), CreatedAt: time.Now().UTC(),
		})
	}
	if got := writer.Dropped(); got != 25 {
		t.Fatalf("dropped = %d, want 25", got)
	}
}

func TestEventWriterPersistenceFailureIsBestEffort(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "events-failure.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	applied, err := db.Migrate(ctx, database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := database.Ready(ctx, applied); err != nil {
		t.Fatalf("ready: %v", err)
	}
	st := store.New(database.DB)
	writer := NewEventWriter(st, nil)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- writer.Run(runCtx) }()
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	writer.RecordRateLimitEvent(store.ProviderRateLimitEvent{
		ProviderID: "p", OccurredAt: time.Now().UTC(), Kind: store.RateLimitEventRateLimited,
		CreatedAt: time.Now().UTC(),
	})
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("writer Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event writer did not stop after persistence failure")
	}
}

func TestEventWriterPersistsWithoutBlocking(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "events.db"))
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
	st := store.New(database.DB)
	writer := NewEventWriter(st, nil)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- writer.Run(runCtx) }()

	now := time.Now().UTC()
	writer.RecordRateLimitEvent(store.ProviderRateLimitEvent{
		ProviderID: "p", Scope: "provider", OccurredAt: now, Kind: store.RateLimitEventRecovered,
		PreviousLevel: 1, EscalationLevel: 0, PreviousAdaptive: 10 * time.Minute,
		CreatedAt: now,
	})

	deadline := time.Now().Add(3 * time.Second)
	for {
		events, err := st.ListProviderRateLimitEvents(ctx, "p", now.Add(-time.Minute), 10)
		if err != nil {
			t.Fatalf("list events: %v", err)
		}
		if len(events) == 1 {
			if events[0].Kind != store.RateLimitEventRecovered || events[0].PreviousLevel != 1 {
				t.Fatalf("event = %+v", events[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("event was not persisted before deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("writer Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event writer did not stop")
	}
}

package delivery_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/delivery"
)

var workerNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(delta time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(delta)
	c.mu.Unlock()
}

type fakeSender struct {
	mu      sync.Mutex
	results []delivery.Result
	calls   int
	records []delivery.Record
}

func (f *fakeSender) Send(_ context.Context, record delivery.Record) delivery.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.records = append(f.records, record)
	if len(f.results) == 0 {
		return delivery.Sent()
	}
	result := f.results[0]
	f.results = f.results[1:]
	return result
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newDeliveryDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "delivery-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	applied, err := db.Migrate(ctx, database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if err := database.Ready(ctx, applied); err != nil {
		t.Fatalf("database.Ready: %v", err)
	}
	return database.DB
}

func enqueue(t *testing.T, database *sql.DB, event notify.Event, targets ...notify.Delivery) notify.EnqueueResult {
	t.Helper()
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := notify.Enqueue(ctx, tx, event, targets)
	if err != nil {
		tx.Rollback()
		t.Fatalf("notify.Enqueue: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return result
}

func healthEvent(id string) notify.Event {
	failures := 3
	return notify.Event{
		ID:           id,
		Kind:         notify.KindProviderDegraded,
		DedupeKey:    "dedupe:" + id,
		ProviderID:   "mrbilit",
		OccurredAt:   workerNow,
		ErrorKind:    "timeout",
		FailureCount: &failures,
		CreatedAt:    workerNow,
	}
}

func enqueueHealth(t *testing.T, database *sql.DB, id string, channels ...notify.Channel) notify.EnqueueResult {
	t.Helper()
	targets := make([]notify.Delivery, 0, len(channels))
	for _, channel := range channels {
		target := "telegram"
		if channel == notify.ChannelPush {
			target = "push:sub-1"
		}
		targets = append(targets, notify.Delivery{Channel: channel, TargetKey: target})
	}
	return enqueue(t, database, healthEvent(id), targets...)
}

func enqueueAvailability(t *testing.T, database *sql.DB, expired bool, id string) notify.EnqueueResult {
	t.Helper()
	ctx := context.Background()
	now := workerNow.Format(time.RFC3339Nano)
	departure := "2026-12-20"
	expires := "2026-12-21T23:59:59+03:30"
	var expiredAt any
	if expired {
		expiredAt = now
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO monitors
		(id,label,origin_name,destination_name,departure_date,adults,children,infants,enabled,expires_at,expired_at,created_at,updated_at)
		VALUES ('m1','Test','Yazd','Mashhad',?,1,0,0,0,?,?,?,?)`,
		departure, expires, expiredAt, now, now); err != nil {
		t.Fatal(err)
	}
	result, err := database.ExecContext(ctx, `INSERT INTO monitor_providers
		(monitor_id,provider_id,active,origin_id,destination_id,config_version,created_at,updated_at)
		VALUES ('m1','mrbilit',1,'219','191',1,?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	state, err := database.ExecContext(ctx, `INSERT INTO inventory_states
		(monitor_provider_id,config_version,journey_key,offering_key,state,state_since,last_observed_at,updated_at)
		VALUES (?,1,'j1','o1','reservable',?,?,?)`, targetID, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	stateID, err := state.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	episode, err := database.ExecContext(ctx, `INSERT INTO availability_episodes
		(state_id,episode_seq,opened_at) VALUES (?,1,?)`, stateID, now)
	if err != nil {
		t.Fatal(err)
	}
	episodeID, err := episode.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	departureAt, _ := time.Parse(time.RFC3339, "2026-12-20T08:00:00+03:30")
	event := notify.Event{
		ID:              id,
		Kind:            notify.KindAvailability,
		DedupeKey:       "dedupe:" + id,
		MonitorID:       "m1",
		ProviderID:      "mrbilit",
		EpisodeID:       &episodeID,
		OccurredAt:      workerNow,
		TrainNumber:     "101",
		Operator:        "Raja",
		OriginName:      "Yazd",
		DestinationName: "Mashhad",
		DepartureAt:     &departureAt,
		WagonName:       "Wagon 1",
		ClassName:       "Economy",
		CreatedAt:       workerNow,
	}
	return enqueue(t, database, event, notify.Delivery{Channel: notify.ChannelTelegram, TargetKey: "telegram"})
}

func workerFor(t *testing.T, database *sql.DB, clock *testClock, senders map[notify.Channel]delivery.Sender, mutate func(*delivery.Config)) *delivery.Worker {
	t.Helper()
	cfg := delivery.Config{
		DB:           database,
		Senders:      senders,
		Now:          clock.Now,
		Lease:        time.Minute,
		Concurrency:  1,
		PollInterval: 100 * time.Millisecond,
		SendTimeout:  time.Second,
		MaxAttempts:  3,
		RetryDelays:  []time.Duration{10 * time.Second},
		Jitter:       func(delay time.Duration) time.Duration { return delay },
	}
	if mutate != nil {
		mutate(&cfg)
	}
	worker, err := delivery.New(cfg)
	if err != nil {
		t.Fatalf("delivery.New: %v", err)
	}
	return worker
}

func deliveryRow(t *testing.T, database *sql.DB, id string) (status string, attempts int, next, errorKind, sentAt, lease sql.NullString) {
	t.Helper()
	err := database.QueryRow(`SELECT status, attempt_count, next_attempt_at, last_error_kind, sent_at, lease_until
		FROM notification_deliveries WHERE id=?`, id).Scan(&status, &attempts, &next, &errorKind, &sentAt, &lease)
	if err != nil {
		t.Fatalf("query delivery: %v", err)
	}
	return
}

func attemptRow(t *testing.T, database *sql.DB, deliveryID string, attemptNo int) (status string, errorKind sql.NullString) {
	t.Helper()
	err := database.QueryRow(`SELECT status, error_kind FROM notification_attempts
		WHERE delivery_id=? AND attempt_no=?`, deliveryID, attemptNo).Scan(&status, &errorKind)
	if err != nil {
		t.Fatalf("query attempt: %v", err)
	}
	return
}

func TestProcessOnceSendsCommittedRows(t *testing.T) {
	database := newDeliveryDB(t)
	clock := &testClock{now: workerNow}
	result := enqueueHealth(t, database, "e1", notify.ChannelTelegram)
	telegram := &fakeSender{}
	worker := workerFor(t, database, clock, map[notify.Channel]delivery.Sender{notify.ChannelTelegram: telegram}, nil)

	processed, err := worker.ProcessOnce(context.Background())
	if err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}
	if processed != 1 || telegram.count() != 1 {
		t.Fatalf("processed/sends = %d/%d", processed, telegram.count())
	}
	status, attempts, next, kind, sentAt, lease := deliveryRow(t, database, deliveryID(t, database, result.EventID))
	if status != "sent" || attempts != 1 || next.Valid || kind.Valid || !sentAt.Valid || lease.Valid {
		t.Fatalf("delivery = %s/%d next=%v kind=%v sent=%v lease=%v", status, attempts, next, kind, sentAt, lease)
	}
	if attemptStatus, attemptKind := attemptRow(t, database, deliveryID(t, database, result.EventID), 1); attemptStatus != "sent" || attemptKind.Valid {
		t.Fatalf("attempt = %s/%v", attemptStatus, attemptKind)
	}
}

func deliveryID(t *testing.T, database *sql.DB, eventID string) string {
	t.Helper()
	var id string
	if err := database.QueryRow(`SELECT id FROM notification_deliveries WHERE event_id=?`, eventID).Scan(&id); err != nil {
		t.Fatalf("query delivery id: %v", err)
	}
	return id
}

func TestRetryThenSuccess(t *testing.T) {
	database := newDeliveryDB(t)
	clock := &testClock{now: workerNow}
	result := enqueueHealth(t, database, "e2", notify.ChannelTelegram)
	telegram := &fakeSender{results: []delivery.Result{delivery.Retry("timeout", 5*time.Second)}}
	worker := workerFor(t, database, clock, map[notify.Channel]delivery.Sender{notify.ChannelTelegram: telegram}, nil)

	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := deliveryID(t, database, result.EventID)
	status, attempts, next, kind, _, lease := deliveryRow(t, database, id)
	if status != "retry_scheduled" || attempts != 1 || !next.Valid || kind.String != "timeout" || lease.Valid {
		t.Fatalf("after retry = %s/%d next=%v kind=%v", status, attempts, next, kind)
	}
	nextAt, err := time.Parse(time.RFC3339Nano, next.String)
	if err != nil || nextAt.Sub(workerNow) != 10*time.Second {
		t.Fatalf("next attempt = %v (%v), want +10s", nextAt, err)
	}
	if attemptStatus, attemptKind := attemptRow(t, database, id, 1); attemptStatus != "failed" || attemptKind.String != "timeout" {
		t.Fatalf("attempt = %s/%v", attemptStatus, attemptKind)
	}

	clock.Advance(10 * time.Second)
	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, attempts, next, kind, sentAt, _ := deliveryRow(t, database, id)
	if status != "sent" || attempts != 2 || next.Valid || kind.Valid || !sentAt.Valid || telegram.count() != 2 {
		t.Fatalf("after success = %s/%d next=%v kind=%v sends=%d", status, attempts, next, kind, telegram.count())
	}
}

func TestPermanentFailureAndMaxAttempts(t *testing.T) {
	t.Run("permanent", func(t *testing.T) {
		database := newDeliveryDB(t)
		clock := &testClock{now: workerNow}
		result := enqueueHealth(t, database, "e3", notify.ChannelTelegram)
		telegram := &fakeSender{results: []delivery.Result{delivery.Failed("telegram_permanent")}}
		worker := workerFor(t, database, clock, map[notify.Channel]delivery.Sender{notify.ChannelTelegram: telegram}, nil)
		if _, err := worker.ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		status, attempts, next, kind, _, _ := deliveryRow(t, database, deliveryID(t, database, result.EventID))
		if status != "failed" || attempts != 1 || next.Valid || kind.String != "telegram_permanent" {
			t.Fatalf("delivery = %s/%d next=%v kind=%v", status, attempts, next, kind)
		}
	})

	t.Run("max-attempts", func(t *testing.T) {
		database := newDeliveryDB(t)
		clock := &testClock{now: workerNow}
		result := enqueueHealth(t, database, "e4", notify.ChannelTelegram)
		telegram := &fakeSender{results: []delivery.Result{delivery.Retry("timeout", 0)}}
		worker := workerFor(t, database, clock, map[notify.Channel]delivery.Sender{notify.ChannelTelegram: telegram}, func(cfg *delivery.Config) {
			cfg.MaxAttempts = 1
		})
		if _, err := worker.ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		status, attempts, next, kind, _, _ := deliveryRow(t, database, deliveryID(t, database, result.EventID))
		if status != "failed" || attempts != 1 || next.Valid || kind.String != "timeout" {
			t.Fatalf("delivery = %s/%d next=%v kind=%v", status, attempts, next, kind)
		}
	})
}

func TestLeaseReclaim(t *testing.T) {
	database := newDeliveryDB(t)
	clock := &testClock{now: workerNow}
	result := enqueueHealth(t, database, "e5", notify.ChannelTelegram)
	id := deliveryID(t, database, result.EventID)
	past := workerNow.Add(-time.Minute).Format(time.RFC3339Nano)
	if _, err := database.Exec(`UPDATE notification_deliveries
		SET status='sending', attempt_count=1, lease_until=?, last_attempt_at=? WHERE id=?`, past, past, id); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO notification_attempts
		(delivery_id,attempt_no,started_at,status) VALUES (?,1,?,'sending')`, id, past); err != nil {
		t.Fatal(err)
	}
	telegram := &fakeSender{}
	worker := workerFor(t, database, clock, map[notify.Channel]delivery.Sender{notify.ChannelTelegram: telegram}, nil)
	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, attempts, _, _, _, lease := deliveryRow(t, database, id)
	if status != "sent" || attempts != 2 || lease.Valid || telegram.count() != 1 {
		t.Fatalf("delivery = %s/%d sends=%d", status, attempts, telegram.count())
	}
	if oldStatus, oldKind := attemptRow(t, database, id, 1); oldStatus != "failed" || oldKind.String != "lease_expired" {
		t.Fatalf("reclaimed attempt = %s/%v", oldStatus, oldKind)
	}
	if newStatus, _ := attemptRow(t, database, id, 2); newStatus != "sent" {
		t.Fatalf("new attempt = %s", newStatus)
	}
}

func TestExpiredMonitorSkipsAvailability(t *testing.T) {
	database := newDeliveryDB(t)
	clock := &testClock{now: workerNow}
	result := enqueueAvailability(t, database, true, "e6")
	telegram := &fakeSender{}
	worker := workerFor(t, database, clock, map[notify.Channel]delivery.Sender{notify.ChannelTelegram: telegram}, nil)
	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, attempts, next, kind, sentAt, _ := deliveryRow(t, database, deliveryID(t, database, result.EventID))
	if status != "skipped" || attempts != 1 || next.Valid || kind.String != "monitor_expired" || sentAt.Valid {
		t.Fatalf("delivery = %s/%d next=%v kind=%v", status, attempts, next, kind)
	}
	if telegram.count() != 0 {
		t.Fatalf("sender calls = %d", telegram.count())
	}
}

func TestChannelIndependence(t *testing.T) {
	database := newDeliveryDB(t)
	clock := &testClock{now: workerNow}
	result := enqueueHealth(t, database, "e7", notify.ChannelTelegram, notify.ChannelPush)
	telegram := &fakeSender{results: []delivery.Result{delivery.Retry("timeout", 0)}}
	push := &fakeSender{}
	worker := workerFor(t, database, clock, map[notify.Channel]delivery.Sender{
		notify.ChannelTelegram: telegram,
		notify.ChannelPush:     push,
	}, func(cfg *delivery.Config) { cfg.Concurrency = 2 })
	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := database.Query(`SELECT channel, status FROM notification_deliveries WHERE event_id=? ORDER BY channel`, result.EventID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	statuses := map[string]string{}
	for rows.Next() {
		var channel, status string
		if err := rows.Scan(&channel, &status); err != nil {
			t.Fatal(err)
		}
		statuses[channel] = status
	}
	if statuses["push"] != "sent" || statuses["telegram"] != "retry_scheduled" {
		t.Fatalf("statuses = %v", statuses)
	}
	if telegram.count() != 1 || push.count() != 1 {
		t.Fatalf("sends = %d/%d", telegram.count(), push.count())
	}
}

func TestUnconfiguredChannelSkips(t *testing.T) {
	database := newDeliveryDB(t)
	clock := &testClock{now: workerNow}
	result := enqueueHealth(t, database, "e8", notify.ChannelPush)
	telegram := &fakeSender{}
	worker := workerFor(t, database, clock, map[notify.Channel]delivery.Sender{notify.ChannelTelegram: telegram}, nil)
	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, _, _, kind, _, _ := deliveryRow(t, database, deliveryID(t, database, result.EventID))
	if status != "skipped" || kind.String != "channel_unavailable" {
		t.Fatalf("delivery = %s/%v", status, kind)
	}
}

func TestNoDuplicateTarget(t *testing.T) {
	database := newDeliveryDB(t)
	clock := &testClock{now: workerNow}
	event := healthEvent("e9")
	targets := []notify.Delivery{
		{Channel: notify.ChannelTelegram, TargetKey: "telegram"},
		{Channel: notify.ChannelTelegram, TargetKey: "telegram"},
	}
	result := enqueue(t, database, event, targets...)
	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries WHERE event_id=?`, result.EventID); count != 1 {
		t.Fatalf("delivery rows = %d", count)
	}
	telegram := &fakeSender{}
	worker := workerFor(t, database, clock, map[notify.Channel]delivery.Sender{notify.ChannelTelegram: telegram}, nil)
	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if telegram.count() != 1 {
		t.Fatalf("sends = %d", telegram.count())
	}
}

func TestMaxAttemptsCleanup(t *testing.T) {
	database := newDeliveryDB(t)
	clock := &testClock{now: workerNow}
	result := enqueueHealth(t, database, "e10", notify.ChannelTelegram)
	id := deliveryID(t, database, result.EventID)
	if _, err := database.Exec(`UPDATE notification_deliveries SET attempt_count=3 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	telegram := &fakeSender{}
	worker := workerFor(t, database, clock, map[notify.Channel]delivery.Sender{notify.ChannelTelegram: telegram}, func(cfg *delivery.Config) {
		cfg.MaxAttempts = 3
	})
	processed, err := worker.ProcessOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 0 || telegram.count() != 0 {
		t.Fatalf("processed/sends = %d/%d", processed, telegram.count())
	}
	status, _, _, kind, _, _ := deliveryRow(t, database, id)
	if status != "failed" || kind.String != "max_attempts" {
		t.Fatalf("delivery = %s/%v", status, kind)
	}
}

func TestCanceledContext(t *testing.T) {
	database := newDeliveryDB(t)
	clock := &testClock{now: workerNow}
	enqueueHealth(t, database, "e11", notify.ChannelTelegram)
	worker := workerFor(t, database, clock, map[notify.Channel]delivery.Sender{notify.ChannelTelegram: &fakeSender{}}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := worker.ProcessOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	database := newDeliveryDB(t)
	clock := &testClock{now: workerNow}
	worker := workerFor(t, database, clock, map[notify.Channel]delivery.Sender{notify.ChannelTelegram: &fakeSender{}}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
}

func countRows(t *testing.T, database *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := database.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	return count
}

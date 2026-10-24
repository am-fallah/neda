package health_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/health"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/provider"
)

var testNow = time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "health-test.db"))
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

func begin(t *testing.T, database *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	return tx
}

func commit(t *testing.T, tx *sql.Tx) {
	t.Helper()
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func rollback(t *testing.T, tx *sql.Tx) {
	t.Helper()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
}

type execer interface {
	Exec(string, ...any) (sql.Result, error)
}

type queryer interface {
	QueryRow(string, ...any) *sql.Row
}

func mustExec(t *testing.T, database execer, query string, args ...any) sql.Result {
	t.Helper()
	result, err := database.Exec(query, args...)
	if err != nil {
		t.Fatalf("exec failed: %v\nquery: %s", err, query)
	}
	return result
}

func mustQueryInt(t *testing.T, database queryer, query string, args ...any) int {
	t.Helper()
	var value int
	if err := database.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("query failed: %v\nquery: %s", err, query)
	}
	return value
}

func insertMonitorTarget(t *testing.T, database *sql.DB, monitorID, providerID string) int64 {
	t.Helper()
	now := testNow.Format(time.RFC3339Nano)
	expires := testNow.Add(24 * time.Hour).Format(time.RFC3339Nano)
	nextDue := testNow.Add(time.Hour).Format(time.RFC3339Nano)
	mustExec(t, database, `INSERT INTO monitors
		(id, label, origin_name, destination_name, departure_date, adults, children, infants,
		 enabled, expires_at, created_at, updated_at)
		VALUES (?, ?, 'Tehran', 'Mashhad', '2026-12-15', 1, 0, 0, 1, ?, ?, ?)`,
		monitorID, "Health "+monitorID, expires, now, now)
	target := mustExec(t, database, `INSERT INTO monitor_providers
		(monitor_id, provider_id, active, origin_id, destination_id, config_version,
		 next_due_at, consecutive_failures, created_at, updated_at)
		VALUES (?, ?, 1, 'origin-fake', 'destination-fake', 1, ?, 5, ?, ?)`,
		monitorID, providerID, nextDue, now, now)
	targetID, err := target.LastInsertId()
	if err != nil {
		t.Fatalf("target LastInsertId: %v", err)
	}
	return targetID
}

var deliverySeq int

func standardDeliveries() []notify.Delivery {
	deliverySeq++
	suffix := fmt.Sprintf("%d", deliverySeq)
	return []notify.Delivery{
		{ID: "health-telegram-" + suffix, Channel: notify.ChannelTelegram, TargetKey: "telegram"},
		{ID: "health-push-" + suffix, Channel: notify.ChannelPush, TargetKey: "push:subscription-1"},
	}
}

func successInput(providerID provider.ID, targetID int64, outcome health.TargetOutcome, at time.Time) health.SuccessInput {
	return health.SuccessInput{
		ProviderID: providerID,
		TargetID:   targetID,
		Outcome:    outcome,
		Now:        at,
		Duration:   1500 * time.Millisecond,
		Deliveries: standardDeliveries(),
	}
}

func failureInput(providerID provider.ID, targetID int64, class provider.Classification, at time.Time) health.FailureInput {
	return health.FailureInput{
		ProviderID:     providerID,
		TargetID:       targetID,
		Classification: class,
		Now:            at,
		Duration:       1500 * time.Millisecond,
		Deliveries:     standardDeliveries(),
	}
}

func applySuccess(t *testing.T, database *sql.DB, input health.SuccessInput) health.Result {
	t.Helper()
	tx := begin(t, database)
	result, err := health.ApplySuccess(context.Background(), tx, input)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("ApplySuccess: %v", err)
	}
	commit(t, tx)
	return result
}

func applyFailure(t *testing.T, database *sql.DB, input health.FailureInput) health.Result {
	t.Helper()
	tx := begin(t, database)
	result, err := health.ApplyFailure(context.Background(), tx, input)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("ApplyFailure: %v", err)
	}
	commit(t, tx)
	return result
}

type healthSnapshot struct {
	LastAttemptAt      sql.NullString
	LastSuccessAt      sql.NullString
	LastFailureAt      sql.NullString
	LastErrorKind      sql.NullString
	LastDurationMS     sql.NullInt64
	ConsecutiveFailure int
	OpenEpisodeID      sql.NullInt64
	UpdatedAt          string
}

func loadHealth(t *testing.T, database *sql.DB, providerID provider.ID) (healthSnapshot, bool) {
	t.Helper()
	var snapshot healthSnapshot
	err := database.QueryRow(`SELECT last_attempt_at, last_success_at, last_failure_at,
		last_error_kind, last_duration_ms, consecutive_failures, open_episode_id, updated_at
		FROM provider_health WHERE provider_id = ?`, string(providerID)).Scan(
		&snapshot.LastAttemptAt, &snapshot.LastSuccessAt, &snapshot.LastFailureAt,
		&snapshot.LastErrorKind, &snapshot.LastDurationMS, &snapshot.ConsecutiveFailure,
		&snapshot.OpenEpisodeID, &snapshot.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return healthSnapshot{}, false
	}
	if err != nil {
		t.Fatalf("load health: %v", err)
	}
	return snapshot, true
}

type targetSnapshot struct {
	LastAttemptAt      sql.NullString
	LastSuccessAt      sql.NullString
	LastOutcome        sql.NullString
	LastErrorKind      sql.NullString
	LastErrorAt        sql.NullString
	NextDueAt          sql.NullString
	ConsecutiveFailure int
	UpdatedAt          string
}

func loadTarget(t *testing.T, database *sql.DB, targetID int64) targetSnapshot {
	t.Helper()
	var snapshot targetSnapshot
	if err := database.QueryRow(`SELECT last_attempt_at, last_success_at, last_outcome,
		last_error_kind, last_error_at, next_due_at, consecutive_failures, updated_at
		FROM monitor_providers WHERE id = ?`, targetID).Scan(
		&snapshot.LastAttemptAt, &snapshot.LastSuccessAt, &snapshot.LastOutcome,
		&snapshot.LastErrorKind, &snapshot.LastErrorAt, &snapshot.NextDueAt,
		&snapshot.ConsecutiveFailure, &snapshot.UpdatedAt,
	); err != nil {
		t.Fatalf("load target: %v", err)
	}
	return snapshot
}

func assertSchedulingUnchanged(t *testing.T, target targetSnapshot) {
	t.Helper()
	if target.NextDueAt.String != testNow.Add(time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("next_due_at changed: %v", target.NextDueAt)
	}
	if target.ConsecutiveFailure != 5 {
		t.Fatalf("target consecutive_failures changed: %d", target.ConsecutiveFailure)
	}
	if target.UpdatedAt != testNow.Format(time.RFC3339Nano) {
		t.Fatalf("target updated_at changed: %s", target.UpdatedAt)
	}
}

type episodeSnapshot struct {
	ID                 int64
	OpenedAt           time.Time
	ClosedAt           *time.Time
	FailureCountAtOpen int
	LastErrorKind      string
}

func loadEpisodes(t *testing.T, database *sql.DB, providerID provider.ID) []episodeSnapshot {
	t.Helper()
	rows, err := database.Query(`SELECT id, opened_at, closed_at, failure_count_at_open, last_error_kind
		FROM provider_health_episodes WHERE provider_id = ? ORDER BY id`, string(providerID))
	if err != nil {
		t.Fatalf("query episodes: %v", err)
	}
	defer rows.Close()
	var episodes []episodeSnapshot
	for rows.Next() {
		var (
			episode   episodeSnapshot
			openedAt  string
			closedAt  sql.NullString
			lastError sql.NullString
		)
		if err := rows.Scan(&episode.ID, &openedAt, &closedAt, &episode.FailureCountAtOpen, &lastError); err != nil {
			t.Fatalf("scan episode: %v", err)
		}
		episode.OpenedAt = mustParseTime(t, openedAt)
		if closedAt.Valid {
			value := mustParseTime(t, closedAt.String)
			episode.ClosedAt = &value
		}
		if lastError.Valid {
			episode.LastErrorKind = lastError.String
		}
		episodes = append(episodes, episode)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate episodes: %v", err)
	}
	return episodes
}

type eventSnapshot struct {
	ID           string
	Kind         string
	DedupeKey    string
	MonitorID    sql.NullString
	ProviderID   string
	EpisodeID    sql.NullInt64
	OccurredAt   time.Time
	ErrorKind    string
	FailureCount int
	CreatedAt    time.Time
}

func loadEvent(t *testing.T, database *sql.DB, dedupeKey string) (eventSnapshot, bool) {
	t.Helper()
	var (
		event      eventSnapshot
		occurredAt string
		createdAt  string
	)
	err := database.QueryRow(`SELECT id, kind, dedupe_key, monitor_id, provider_id, episode_id,
		occurred_at, error_kind, failure_count, created_at
		FROM events WHERE dedupe_key = ?`, dedupeKey).Scan(
		&event.ID, &event.Kind, &event.DedupeKey, &event.MonitorID, &event.ProviderID,
		&event.EpisodeID, &occurredAt, &event.ErrorKind, &event.FailureCount, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return eventSnapshot{}, false
	}
	if err != nil {
		t.Fatalf("load event: %v", err)
	}
	event.OccurredAt = mustParseTime(t, occurredAt)
	event.CreatedAt = mustParseTime(t, createdAt)
	return event, true
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed.UTC()
}

func countRows(t *testing.T, database *sql.DB, query string, args ...any) int {
	t.Helper()
	return mustQueryInt(t, database, query, args...)
}

func infra(kind provider.ErrorKind, status int) provider.Classification {
	return provider.Classification{
		Category:           provider.CategoryInfrastructure,
		Kind:               kind,
		StatusCode:         status,
		CountsTowardOutage: true,
		TargetBackoff:      true,
	}
}

func TestApplySuccessUpsertsHealthAndTargetDiagnostics(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", "fake")
	result := applySuccess(t, database, successInput("fake", targetID, health.OutcomeSuccess, testNow))
	if result.ProviderFailureCount != 0 || result.EpisodeOpened != nil || result.EpisodeClosed != nil || len(result.EventsCreated) != 0 {
		t.Fatalf("result = %+v", result)
	}

	healthRow, found := loadHealth(t, database, "fake")
	if !found {
		t.Fatal("provider health row not created")
	}
	if healthRow.ConsecutiveFailure != 0 ||
		healthRow.LastAttemptAt.String != testNow.Format(time.RFC3339Nano) ||
		healthRow.LastSuccessAt.String != testNow.Format(time.RFC3339Nano) ||
		healthRow.LastFailureAt.Valid || healthRow.LastErrorKind.Valid ||
		healthRow.LastDurationMS.Int64 != 1500 || healthRow.OpenEpisodeID.Valid ||
		healthRow.UpdatedAt != testNow.Format(time.RFC3339Nano) {
		t.Fatalf("health row = %+v", healthRow)
	}
	target := loadTarget(t, database, targetID)
	if target.LastAttemptAt.String != testNow.Format(time.RFC3339Nano) ||
		target.LastSuccessAt.String != testNow.Format(time.RFC3339Nano) ||
		target.LastOutcome.String != "success" ||
		target.LastErrorKind.Valid || target.LastErrorAt.Valid {
		t.Fatalf("target diagnostics = %+v", target)
	}
	assertSchedulingUnchanged(t, target)
	if len(loadEpisodes(t, database, "fake")) != 0 {
		t.Fatal("success created a degraded episode")
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("events = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM inventory_states`); count != 0 {
		t.Fatalf("inventory states = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM check_runs`); count != 0 {
		t.Fatalf("check runs = %d, want 0", count)
	}

	applySuccess(t, database, successInput("fake", targetID, health.OutcomeEmpty, testNow.Add(time.Minute)))
	healthRow, _ = loadHealth(t, database, "fake")
	target = loadTarget(t, database, targetID)
	if healthRow.LastSuccessAt.String != testNow.Add(time.Minute).Format(time.RFC3339Nano) ||
		target.LastOutcome.String != "empty" ||
		target.LastSuccessAt.String != testNow.Add(time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("empty success did not update diagnostics: health=%+v target=%+v", healthRow, target)
	}
	assertSchedulingUnchanged(t, target)
}

func TestApplyInfrastructureFailuresThresholdAndRecovery(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", "fake")
	t0 := testNow
	t1 := t0.Add(time.Minute)
	t2 := t1.Add(time.Minute)
	t3 := t2.Add(time.Minute)
	t4 := t3.Add(time.Minute)
	t5 := t4.Add(time.Minute)

	applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrTimeout, 0), t0))
	healthRow, _ := loadHealth(t, database, "fake")
	if healthRow.ConsecutiveFailure != 1 || healthRow.LastErrorKind.String != "timeout" ||
		healthRow.LastFailureAt.String != t0.Format(time.RFC3339Nano) ||
		healthRow.LastDurationMS.Int64 != 1500 {
		t.Fatalf("failure 1 health = %+v", healthRow)
	}
	if len(loadEpisodes(t, database, "fake")) != 0 {
		t.Fatal("first failure opened an episode")
	}
	target := loadTarget(t, database, targetID)
	if target.LastOutcome.String != "error" || target.LastErrorKind.String != "timeout" ||
		target.LastErrorAt.String != t0.Format(time.RFC3339Nano) ||
		target.LastAttemptAt.String != t0.Format(time.RFC3339Nano) ||
		target.LastSuccessAt.Valid {
		t.Fatalf("target failure diagnostics = %+v", target)
	}
	assertSchedulingUnchanged(t, target)

	applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrNetwork, 0), t1))
	healthRow, _ = loadHealth(t, database, "fake")
	if healthRow.ConsecutiveFailure != 2 || healthRow.LastErrorKind.String != "network" {
		t.Fatalf("failure 2 health = %+v", healthRow)
	}

	applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrRateLimited, 429), t2))
	healthRow, _ = loadHealth(t, database, "fake")
	episodes := loadEpisodes(t, database, "fake")
	if healthRow.ConsecutiveFailure != 3 || len(episodes) != 1 || episodes[0].ClosedAt != nil ||
		episodes[0].FailureCountAtOpen != 3 || episodes[0].LastErrorKind != "rate_limited" ||
		!healthRow.OpenEpisodeID.Valid || healthRow.OpenEpisodeID.Int64 != episodes[0].ID {
		t.Fatalf("failure 3 health/episodes = %+v/%+v", healthRow, episodes)
	}
	event, found := loadEvent(t, database, fmt.Sprintf("health:fake:%d:degraded", episodes[0].ID))
	if !found {
		t.Fatal("degraded event not found")
	}
	if event.Kind != "provider_degraded" || event.ProviderID != "fake" ||
		event.MonitorID.Valid || event.EpisodeID.Valid ||
		event.FailureCount != 3 || event.ErrorKind != "rate_limited" ||
		!event.OccurredAt.Equal(t2) || !event.CreatedAt.Equal(t2) {
		t.Fatalf("degraded event = %+v", event)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries WHERE event_id = ?`, event.ID); count != 2 {
		t.Fatalf("degraded deliveries = %d, want 2", count)
	}

	applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrHTTPStatus, 500), t3))
	healthRow, _ = loadHealth(t, database, "fake")
	episodes = loadEpisodes(t, database, "fake")
	if healthRow.ConsecutiveFailure != 4 || len(episodes) != 1 || episodes[0].LastErrorKind != "http_status" {
		t.Fatalf("failure 4 health/episode = %+v/%+v", healthRow, episodes)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("failure 4 created an event: %d", count)
	}

	res := applySuccess(t, database, successInput("fake", targetID, health.OutcomeEmpty, t4))
	if res.EpisodeClosed == nil || *res.EpisodeClosed != episodes[0].ID || len(res.EventsCreated) != 1 {
		t.Fatalf("recovery result = %+v", res)
	}
	healthRow, _ = loadHealth(t, database, "fake")
	episodes = loadEpisodes(t, database, "fake")
	if healthRow.ConsecutiveFailure != 0 || healthRow.OpenEpisodeID.Valid ||
		episodes[0].ClosedAt == nil || !episodes[0].ClosedAt.Equal(t4) ||
		healthRow.LastErrorKind.String != "http_status" ||
		healthRow.LastFailureAt.String != t3.Format(time.RFC3339Nano) ||
		healthRow.LastSuccessAt.String != t4.Format(time.RFC3339Nano) {
		t.Fatalf("recovery health/episode = %+v/%+v", healthRow, episodes)
	}
	recovery, found := loadEvent(t, database, fmt.Sprintf("health:fake:%d:recovered", episodes[0].ID))
	if !found {
		t.Fatal("recovery event not found")
	}
	if recovery.Kind != "provider_recovered" || recovery.FailureCount != 3 ||
		recovery.ErrorKind != "http_status" || recovery.MonitorID.Valid || recovery.EpisodeID.Valid {
		t.Fatalf("recovery event = %+v", recovery)
	}
	target = loadTarget(t, database, targetID)
	if target.LastOutcome.String != "empty" ||
		target.LastSuccessAt.String != t4.Format(time.RFC3339Nano) ||
		target.LastErrorKind.String != "http_status" ||
		target.LastErrorAt.String != t3.Format(time.RFC3339Nano) {
		t.Fatalf("target recovery diagnostics = %+v", target)
	}
	assertSchedulingUnchanged(t, target)

	applySuccess(t, database, successInput("fake", targetID, health.OutcomeSuccess, t5))
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 2 {
		t.Fatalf("repeated success created recovery event: %d", count)
	}

	for _, at := range []time.Time{t5.Add(time.Minute), t5.Add(2 * time.Minute), t5.Add(3 * time.Minute)} {
		applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrTimeout, 0), at))
	}
	episodes = loadEpisodes(t, database, "fake")
	if len(episodes) != 2 || episodes[1].ClosedAt != nil || episodes[1].FailureCountAtOpen != 3 {
		t.Fatalf("re-degradation episodes = %+v", episodes)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 3 {
		t.Fatalf("re-degradation events = %d, want 3", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 6 {
		t.Fatalf("deliveries = %d, want 6", count)
	}
}

func TestApplyNonCountingFailuresDoNotIncrementOrReset(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", "fake")
	applyFailure(t, database, failureInput("fake", targetID, provider.Classification{
		Category: provider.CategoryConfiguration, Kind: provider.ErrConfiguration,
	}, testNow))
	healthRow, _ := loadHealth(t, database, "fake")
	if healthRow.ConsecutiveFailure != 0 || len(loadEpisodes(t, database, "fake")) != 0 {
		t.Fatalf("configuration failure changed counter/episode: %+v", healthRow)
	}
	if healthRow.LastErrorKind.String != "configuration" {
		t.Fatalf("configuration diagnostics = %+v", healthRow)
	}

	applyFailure(t, database, failureInput("fake", targetID, provider.Classification{
		Category: provider.CategoryRoute, Kind: provider.ErrRouteNotFound, StatusCode: 404,
	}, testNow.Add(time.Minute)))
	applyFailure(t, database, failureInput("fake", targetID, provider.Classification{
		Category: provider.CategoryRequest, Kind: provider.ErrHTTPStatus, StatusCode: 400,
	}, testNow.Add(2*time.Minute)))
	healthRow, _ = loadHealth(t, database, "fake")
	if healthRow.ConsecutiveFailure != 0 || healthRow.LastErrorKind.String != "http_status" {
		t.Fatalf("non-counting failures changed counter: %+v", healthRow)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("non-counting failures created events: %d", count)
	}

	applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrTimeout, 0), testNow.Add(3*time.Minute)))
	applyFailure(t, database, failureInput("fake", targetID, provider.Classification{
		Category: provider.CategoryConfiguration, Kind: provider.ErrConfiguration,
	}, testNow.Add(4*time.Minute)))
	healthRow, _ = loadHealth(t, database, "fake")
	if healthRow.ConsecutiveFailure != 1 {
		t.Fatalf("non-counting failure changed counter after infra: %+v", healthRow)
	}
	applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrNetwork, 0), testNow.Add(5*time.Minute)))
	applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrHTTPStatus, 500), testNow.Add(6*time.Minute)))
	healthRow, _ = loadHealth(t, database, "fake")
	episodes := loadEpisodes(t, database, "fake")
	if healthRow.ConsecutiveFailure != 3 || len(episodes) != 1 || episodes[0].ClosedAt != nil {
		t.Fatalf("interleaved infra threshold health/episode = %+v/%+v", healthRow, episodes)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("threshold events = %d, want 1", count)
	}
	target := loadTarget(t, database, targetID)
	assertSchedulingUnchanged(t, target)
}

func TestApplySuccessResetsCountersWithoutRecoveryEvent(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", "fake")
	applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrTimeout, 0), testNow))
	applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrNetwork, 0), testNow.Add(time.Minute)))
	healthRow, _ := loadHealth(t, database, "fake")
	if healthRow.ConsecutiveFailure != 2 {
		t.Fatalf("counter = %d, want 2", healthRow.ConsecutiveFailure)
	}
	res := applySuccess(t, database, successInput("fake", targetID, health.OutcomeSuccess, testNow.Add(2*time.Minute)))
	if res.EpisodeClosed != nil || len(res.EventsCreated) != 0 {
		t.Fatalf("success without open episode returned %+v", res)
	}
	healthRow, _ = loadHealth(t, database, "fake")
	if healthRow.ConsecutiveFailure != 0 {
		t.Fatalf("success did not reset counter: %+v", healthRow)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("success without episode created events: %d", count)
	}
	applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrTimeout, 0), testNow.Add(3*time.Minute)))
	healthRow, _ = loadHealth(t, database, "fake")
	if healthRow.ConsecutiveFailure != 1 {
		t.Fatalf("failure after success counter = %d, want 1", healthRow.ConsecutiveFailure)
	}
}

func TestApplyHealthRejectsNonApplicableOutcomesAndInvalidInput(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", "fake")
	baseFailure := failureInput("fake", targetID, infra(provider.ErrTimeout, 0), testNow)

	failureCases := []struct {
		name string
		in   health.FailureInput
	}{
		{"abandoned", func() health.FailureInput {
			in := baseFailure
			in.Classification = provider.Classification{Category: provider.CategoryAbandoned, Kind: provider.ErrCanceled}
			return in
		}()},
		{"success", func() health.FailureInput {
			in := baseFailure
			in.Classification = provider.Classification{Category: provider.CategorySuccess}
			return in
		}()},
		{"unknown category", func() health.FailureInput {
			in := baseFailure
			in.Classification = provider.Classification{Category: "bogus", Kind: provider.ErrTimeout}
			return in
		}()},
		{"invalid kind", func() health.FailureInput {
			in := baseFailure
			in.Classification = infra(provider.ErrorKind("bogus"), 0)
			return in
		}()},
		{"zero target", func() health.FailureInput { in := baseFailure; in.TargetID = 0; return in }()},
		{"zero provider", func() health.FailureInput { in := baseFailure; in.ProviderID = ""; return in }()},
		{"zero now", func() health.FailureInput { in := baseFailure; in.Now = time.Time{}; return in }()},
		{"negative duration", func() health.FailureInput { in := baseFailure; in.Duration = -time.Second; return in }()},
		{"bad delivery", func() health.FailureInput {
			in := baseFailure
			in.Deliveries = []notify.Delivery{{ID: "d", Channel: notify.ChannelTelegram, TargetKey: "chat"}}
			return in
		}()},
	}
	for _, test := range failureCases {
		t.Run(test.name, func(t *testing.T) {
			tx := begin(t, database)
			if _, err := health.ApplyFailure(context.Background(), tx, test.in); err == nil {
				t.Fatal("ApplyFailure succeeded, want error")
			}
			rollback(t, tx)
		})
	}
	if _, found := loadHealth(t, database, "fake"); found {
		t.Fatal("invalid failures created provider health")
	}
	if _, err := health.ApplyFailure(context.Background(), nil, baseFailure); err == nil {
		t.Fatal("ApplyFailure(nil tx) succeeded")
	}

	baseSuccess := successInput("fake", targetID, health.OutcomeSuccess, testNow)
	successCases := []struct {
		name string
		in   health.SuccessInput
	}{
		{"invalid outcome", func() health.SuccessInput { in := baseSuccess; in.Outcome = "bogus"; return in }()},
		{"zero target", func() health.SuccessInput { in := baseSuccess; in.TargetID = 0; return in }()},
		{"zero provider", func() health.SuccessInput { in := baseSuccess; in.ProviderID = ""; return in }()},
		{"zero now", func() health.SuccessInput { in := baseSuccess; in.Now = time.Time{}; return in }()},
		{"negative duration", func() health.SuccessInput { in := baseSuccess; in.Duration = -time.Second; return in }()},
		{"bad delivery", func() health.SuccessInput {
			in := baseSuccess
			in.Deliveries = []notify.Delivery{{ID: "d", Channel: notify.ChannelPush, TargetKey: "nope"}}
			return in
		}()},
	}
	for _, test := range successCases {
		t.Run(test.name, func(t *testing.T) {
			tx := begin(t, database)
			if _, err := health.ApplySuccess(context.Background(), tx, test.in); err == nil {
				t.Fatal("ApplySuccess succeeded, want error")
			}
			rollback(t, tx)
		})
	}
	if _, err := health.ApplySuccess(context.Background(), nil, baseSuccess); err == nil {
		t.Fatal("ApplySuccess(nil tx) succeeded")
	}
}

func TestApplyHealthRollsBackWithCallerTransaction(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", "fake")
	tx := begin(t, database)
	for i := 0; i < 3; i++ {
		if _, err := health.ApplyFailure(context.Background(), tx, failureInput("fake", targetID, infra(provider.ErrTimeout, 0), testNow.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatalf("ApplyFailure %d: %v", i, err)
		}
	}
	if count := mustQueryInt(t, tx, `SELECT COUNT(*) FROM provider_health`); count != 1 {
		t.Fatalf("provider health inside tx = %d, want 1", count)
	}
	if count := mustQueryInt(t, tx, `SELECT COUNT(*) FROM provider_health_episodes`); count != 1 {
		t.Fatalf("episodes inside tx = %d, want 1", count)
	}
	if count := mustQueryInt(t, tx, `SELECT COUNT(*) FROM events`); count != 1 {
		t.Fatalf("events inside tx = %d, want 1", count)
	}
	rollback(t, tx)

	if count := countRows(t, database, `SELECT COUNT(*) FROM provider_health`); count != 0 {
		t.Fatalf("provider health after rollback = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM provider_health_episodes`); count != 0 {
		t.Fatalf("episodes after rollback = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events`); count != 0 {
		t.Fatalf("events after rollback = %d, want 0", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 0 {
		t.Fatalf("deliveries after rollback = %d, want 0", count)
	}
}

func TestApplyHealthFaultInjectionLeavesNoPartialEpisode(t *testing.T) {
	database := newTestDB(t)
	firstTarget := insertMonitorTarget(t, database, "m1", "fake")
	for i := 0; i < 3; i++ {
		input := failureInput("fake", firstTarget, infra(provider.ErrTimeout, 0), testNow.Add(time.Duration(i)*time.Minute))
		if i == 2 {
			input.Deliveries = []notify.Delivery{{ID: "health-duplicate-delivery", Channel: notify.ChannelTelegram, TargetKey: "telegram"}}
		}
		applyFailure(t, database, input)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 1 {
		t.Fatalf("baseline deliveries = %d, want 1", count)
	}

	secondTarget := insertMonitorTarget(t, database, "m2", "other")
	for i := 0; i < 2; i++ {
		applyFailure(t, database, failureInput("other", secondTarget, infra(provider.ErrTimeout, 0), testNow.Add(time.Duration(i)*time.Minute)))
	}
	tx := begin(t, database)
	input := failureInput("other", secondTarget, infra(provider.ErrNetwork, 0), testNow.Add(2*time.Minute))
	input.Deliveries = []notify.Delivery{{ID: "health-duplicate-delivery", Channel: notify.ChannelTelegram, TargetKey: "telegram"}}
	if _, err := health.ApplyFailure(context.Background(), tx, input); err == nil {
		t.Fatal("ApplyFailure succeeded with duplicate delivery primary key")
	}
	rollback(t, tx)

	healthRow, found := loadHealth(t, database, "other")
	if !found || healthRow.ConsecutiveFailure != 2 || healthRow.OpenEpisodeID.Valid {
		t.Fatalf("provider health after failed threshold = %+v", healthRow)
	}
	if len(loadEpisodes(t, database, "other")) != 0 {
		t.Fatal("failed threshold left an episode")
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events WHERE provider_id = 'other'`); count != 0 {
		t.Fatalf("failed threshold left events: %d", count)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM notification_deliveries`); count != 1 {
		t.Fatalf("delivery count after rollback = %d, want 1", count)
	}
}

func TestApplyHealthDoesNotTouchAvailabilityOrScheduling(t *testing.T) {
	database := newTestDB(t)
	targetID := insertMonitorTarget(t, database, "m1", "fake")
	now := testNow.Format(time.RFC3339Nano)
	state := mustExec(t, database, `INSERT INTO inventory_states
		(monitor_provider_id, config_version, journey_key, offering_key, state, state_since,
		 last_observed_at, updated_at)
		VALUES (?, 1, 'j1', 'o1', 'reservable', ?, ?, ?)`, targetID, now, now, now)
	stateID, err := state.LastInsertId()
	if err != nil {
		t.Fatalf("state LastInsertId: %v", err)
	}
	episode := mustExec(t, database, `INSERT INTO availability_episodes
		(state_id, episode_seq, opened_at) VALUES (?, 1, ?)`, stateID, now)
	episodeID, err := episode.LastInsertId()
	if err != nil {
		t.Fatalf("episode LastInsertId: %v", err)
	}

	applySuccess(t, database, successInput("fake", targetID, health.OutcomeSuccess, testNow))
	applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrTimeout, 0), testNow.Add(time.Minute)))
	applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrNetwork, 0), testNow.Add(2*time.Minute)))
	applyFailure(t, database, failureInput("fake", targetID, infra(provider.ErrHTTPStatus, 500), testNow.Add(3*time.Minute)))

	var stateValue string
	if err := database.QueryRow(`SELECT state FROM inventory_states WHERE id = ?`, stateID).Scan(&stateValue); err != nil {
		t.Fatalf("state after health: %v", err)
	}
	if stateValue != "reservable" {
		t.Fatalf("health changed availability state: %s", stateValue)
	}
	var closedAt sql.NullString
	if err := database.QueryRow(`SELECT closed_at FROM availability_episodes WHERE id = ?`, episodeID).Scan(&closedAt); err != nil {
		t.Fatalf("episode after health: %v", err)
	}
	if closedAt.Valid {
		t.Fatalf("health closed availability episode: %v", closedAt)
	}
	if count := countRows(t, database, `SELECT COUNT(*) FROM events WHERE kind = 'availability'`); count != 0 {
		t.Fatalf("health created availability events: %d", count)
	}
	target := loadTarget(t, database, targetID)
	assertSchedulingUnchanged(t, target)
	if count := countRows(t, database, `SELECT COUNT(*) FROM check_runs`); count != 0 {
		t.Fatalf("health created check runs: %d", count)
	}
}

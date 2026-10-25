package scheduler_test

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/scheduler"
	"github.com/am-fallah/neda/internal/store"
)

// integrationHarness composes the real scheduler, check stage, outcome runner,
// availability/health appliers, and durable SQLite state for the SCH-008 and
// SCH-009 integration suites. It exposes the database path so tests can close
// and reopen the database to prove restart behavior. It intentionally remains
// separate from newOutcomeHarness: SCH-009 needs a file-backed reopenable
// database, a mutable fake clock, and multi-provider monitors, which would make
// the single-provider outcome fixture's restart semantics harder to review.
type integrationHarness struct {
	t          *testing.T
	path       string
	database   *db.DB
	sqlDB      *sql.DB
	st         *store.Store
	clock      *fakeClock
	registry   *provider.Registry
	adapters   []provider.Provider
	deliveries scheduler.DeliverySource
	timeout    time.Duration
	stage      *scheduler.CheckStage
	runner     *scheduler.OutcomeRunner
	wakes      atomic.Int32
	wake       *scheduler.Waker
	onWake     func()
}

type harnessTarget struct {
	providerID    provider.ID
	active        bool
	originID      string
	destinationID string
	bookingSlug   string
	genderCode    string
	configVersion int64
	departureDate string
	selections    []domain.MonitorProviderSelection
}

func harnessTargetFor(providerID provider.ID) harnessTarget {
	return harnessTarget{providerID: providerID, active: true, configVersion: 1}
}

func newIntegrationHarness(t *testing.T, adapters ...provider.Provider) *integrationHarness {
	t.Helper()
	h := &integrationHarness{
		t:          t,
		path:       filepath.Join(t.TempDir(), "integration.db"),
		clock:      newFakeClock(schedulerNow),
		adapters:   append([]provider.Provider(nil), adapters...),
		deliveries: telegramDeliveries,
		timeout:    time.Second,
		wake:       scheduler.NewWaker(),
	}
	h.registry = checkRegistry(t, adapters...)
	h.open()
	t.Cleanup(h.close)
	return h
}

func (h *integrationHarness) open() {
	h.t.Helper()
	database, err := db.Open(context.Background(), h.path)
	if err != nil {
		h.t.Fatalf("db.Open: %v", err)
	}
	applied, err := db.Migrate(context.Background(), database.DB, migrations.FS)
	if err != nil {
		_ = database.Close()
		h.t.Fatalf("db.Migrate: %v", err)
	}
	if err := database.Ready(context.Background(), applied); err != nil {
		_ = database.Close()
		h.t.Fatalf("database.Ready: %v", err)
	}
	h.database = database
	h.sqlDB = database.DB
	h.st = store.New(database.DB)
	if err := h.st.EnsurePollInterval(context.Background(), h.clock.Now().UTC()); err != nil {
		h.t.Fatalf("EnsurePollInterval: %v", err)
	}
	h.rebuild()
}

func (h *integrationHarness) rebuild() {
	h.t.Helper()
	stage, err := scheduler.NewCheckStage(scheduler.CheckStageConfig{
		Repository: h.st,
		Providers:  h.registry,
		Now:        h.clock.Now,
		Timeout:    h.timeout,
		Logger:     discardLogger(),
	})
	if err != nil {
		h.t.Fatalf("NewCheckStage: %v", err)
	}
	runner, err := scheduler.NewOutcomeRunner(scheduler.OutcomeRunnerConfig{
		Stage:      stage,
		Store:      h.st,
		Deliveries: h.deliveries,
		Wake: func() {
			h.wakes.Add(1)
			if h.onWake != nil {
				h.onWake()
			}
			h.wake.Wake()
		},
		Now:    h.clock.Now,
		Random: midpointRandom{},
		Logger: discardLogger(),
	})
	if err != nil {
		h.t.Fatalf("NewOutcomeRunner: %v", err)
	}
	h.stage = stage
	h.runner = runner
}

// reopen closes the current database handle and opens the same path again. All
// previously committed state must remain durable; rebuilt components must read
// it through the new handle.
func (h *integrationHarness) reopen() {
	h.t.Helper()
	if h.database != nil {
		if err := h.database.Close(); err != nil {
			h.t.Fatalf("close database: %v", err)
		}
		h.database = nil
	}
	h.open()
}

func (h *integrationHarness) close() {
	if h == nil || h.database == nil {
		return
	}
	if err := h.database.Close(); err != nil {
		h.t.Errorf("database.Close: %v", err)
	}
	h.database = nil
}

func (h *integrationHarness) createMonitor(id string, enabled bool, expiresAt time.Time, targets ...harnessTarget) domain.Monitor {
	h.t.Helper()
	now := h.clock.Now().UTC()
	if expiresAt.IsZero() {
		expiresAt = now.Add(24 * time.Hour)
	}
	rows := make([]domain.MonitorProvider, 0, len(targets))
	for _, target := range targets {
		version := target.configVersion
		if version == 0 {
			version = 1
		}
		originID := target.originID
		if originID == "" {
			originID = "origin-" + string(target.providerID)
		}
		destinationID := target.destinationID
		if destinationID == "" {
			destinationID = "destination-" + string(target.providerID)
		}
		rows = append(rows, domain.MonitorProvider{
			MonitorID:     id,
			ProviderID:    target.providerID,
			Active:        target.active,
			OriginID:      originID,
			DestinationID: destinationID,
			BookingSlug:   target.bookingSlug,
			GenderCode:    target.genderCode,
			ConfigVersion: version,
			Selections:    append([]domain.MonitorProviderSelection(nil), target.selections...),
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	}
	departureDate := "2026-12-15"
	for _, target := range targets {
		if target.departureDate != "" {
			departureDate = target.departureDate
		}
	}
	monitor, err := h.st.CreateMonitor(context.Background(), domain.Monitor{
		ID:              id,
		Label:           "Integration " + id,
		OriginName:      "Tehran",
		DestinationName: "Mashhad",
		DepartureDate:   departureDate,
		Adults:          2,
		Children:        1,
		Enabled:         enabled,
		ExpiresAt:       expiresAt,
		CreatedAt:       now,
		UpdatedAt:       now,
		Providers:       rows,
	})
	if err != nil {
		h.t.Fatalf("CreateMonitor(%s): %v", id, err)
	}
	return monitor
}

func (h *integrationHarness) monitor(id string) domain.Monitor {
	h.t.Helper()
	monitor, err := h.st.GetMonitor(context.Background(), id)
	if err != nil {
		h.t.Fatalf("GetMonitor(%s): %v", id, err)
	}
	return monitor
}

func (h *integrationHarness) targetByID(targetID int64) domain.MonitorProvider {
	h.t.Helper()
	monitors, err := h.st.ListMonitors(context.Background())
	if err != nil {
		h.t.Fatalf("ListMonitors: %v", err)
	}
	for _, monitor := range monitors {
		for _, target := range monitor.Providers {
			if target.ID == targetID {
				return target
			}
		}
	}
	h.t.Fatalf("target %d not found", targetID)
	return domain.MonitorProvider{}
}

func (h *integrationHarness) target(monitorID string, providerID provider.ID) domain.MonitorProvider {
	h.t.Helper()
	monitor := h.monitor(monitorID)
	for _, target := range monitor.Providers {
		if target.ProviderID == providerID {
			return target
		}
	}
	h.t.Fatalf("monitor %s has no target for provider %s", monitorID, providerID)
	return domain.MonitorProvider{}
}

func (h *integrationHarness) dispatchTarget(monitorID string, providerID provider.ID) scheduler.Target {
	h.t.Helper()
	monitor := h.monitor(monitorID)
	return checkTargetFor(h.t, monitor, providerID)
}

func (h *integrationHarness) run(target scheduler.Target) (scheduler.Result, error) {
	return h.runner.Run(context.Background(), target)
}

func (h *integrationHarness) runFor(monitorID string, providerID provider.ID) (scheduler.Result, error) {
	return h.run(h.dispatchTarget(monitorID, providerID))
}

func (h *integrationHarness) startScheduler(workers int, spacing time.Duration) *runningScheduler {
	h.t.Helper()
	return startSchedulerWithSpacing(h.t, h.st, h.registry, h.clock, h.runner, workers, zeroRandom(), h.wake, time.Second, spacing)
}

func (h *integrationHarness) startSchedulerWithRunner(runner scheduler.Runner, workers int, spacing time.Duration) *runningScheduler {
	h.t.Helper()
	return startSchedulerWithSpacing(h.t, h.st, h.registry, h.clock, runner, workers, zeroRandom(), h.wake, time.Second, spacing)
}

func (h *integrationHarness) count(query string, args ...any) int {
	h.t.Helper()
	var count int
	if err := h.sqlDB.QueryRow(query, args...).Scan(&count); err != nil {
		h.t.Fatalf("count query %q: %v", query, err)
	}
	return count
}

func (h *integrationHarness) checkCount(targetID int64) int {
	return h.count(`SELECT COUNT(*) FROM check_runs WHERE monitor_provider_id = ?`, targetID)
}

func (h *integrationHarness) waitForCheckOutcome(targetID int64, want string) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var outcome string
		err := h.sqlDB.QueryRow(`SELECT outcome FROM check_runs
			WHERE monitor_provider_id = ? ORDER BY id DESC LIMIT 1`, targetID).Scan(&outcome)
		if err == nil && outcome == want {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for target %d outcome %q; last outcome=%q err=%v", targetID, want, outcome, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (h *integrationHarness) waitForCheckCount(targetID int64, want int) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := h.checkCount(targetID); got == want {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for target %d check count %d; got %d", targetID, want, h.checkCount(targetID))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type integrationCheck struct {
	ID            int64
	Outcome       string
	ErrorKind     sql.NullString
	ConfigVersion int64
	FinishedAt    sql.NullString
	DurationMS    sql.NullInt64
	HTTPStatus    sql.NullInt64
	Journeys      int
	Offerings     int
	Reservable    int
	NotReservable int
	Unknown       int
	Warnings      int
}

func (h *integrationHarness) latestCheck(targetID int64) integrationCheck {
	h.t.Helper()
	var check integrationCheck
	if err := h.sqlDB.QueryRow(`SELECT id, outcome, error_kind, config_version, finished_at,
		duration_ms, http_status, journey_count, offering_count, reservable_count,
		not_reservable_count, unknown_count, warning_count
		FROM check_runs WHERE monitor_provider_id = ? ORDER BY id DESC LIMIT 1`, targetID).Scan(
		&check.ID, &check.Outcome, &check.ErrorKind, &check.ConfigVersion, &check.FinishedAt,
		&check.DurationMS, &check.HTTPStatus, &check.Journeys, &check.Offerings,
		&check.Reservable, &check.NotReservable, &check.Unknown, &check.Warnings,
	); err != nil {
		h.t.Fatalf("query latest check for target %d: %v", targetID, err)
	}
	return check
}

type integrationHealth struct {
	Found             bool
	LastAttemptAt     sql.NullString
	LastSuccessAt     sql.NullString
	LastFailureAt     sql.NullString
	LastErrorKind     sql.NullString
	LastDurationMS    sql.NullInt64
	Consecutive       int
	OpenEpisodeID     sql.NullInt64
	OpenEpisodeCount  int
	ProviderEpisodes  int
	RecoveryEvents    int
	DegradedEvents    int
	HealthEventCount  int
	HealthDeliveryCnt int
}

func (h *integrationHarness) providerHealth(providerID provider.ID) integrationHealth {
	h.t.Helper()
	var health integrationHealth
	err := h.sqlDB.QueryRow(`SELECT last_attempt_at, last_success_at, last_failure_at,
		last_error_kind, last_duration_ms, consecutive_failures, open_episode_id
		FROM provider_health WHERE provider_id = ?`, string(providerID)).Scan(
		&health.LastAttemptAt, &health.LastSuccessAt, &health.LastFailureAt,
		&health.LastErrorKind, &health.LastDurationMS, &health.Consecutive, &health.OpenEpisodeID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		health.OpenEpisodeCount = h.count(`SELECT COUNT(*) FROM provider_health_episodes WHERE provider_id = ? AND closed_at IS NULL`, string(providerID))
		health.ProviderEpisodes = h.count(`SELECT COUNT(*) FROM provider_health_episodes WHERE provider_id = ?`, string(providerID))
		health.DegradedEvents = h.count(`SELECT COUNT(*) FROM events WHERE kind = 'provider_degraded' AND provider_id = ?`, string(providerID))
		health.RecoveryEvents = h.count(`SELECT COUNT(*) FROM events WHERE kind = 'provider_recovered' AND provider_id = ?`, string(providerID))
		health.HealthEventCount = health.DegradedEvents + health.RecoveryEvents
		health.HealthDeliveryCnt = h.count(`SELECT COUNT(*) FROM notification_deliveries d
			JOIN events e ON e.id = d.event_id WHERE e.provider_id = ? AND e.kind IN ('provider_degraded','provider_recovered')`, string(providerID))
		return health
	}
	if err != nil {
		h.t.Fatalf("query provider health %s: %v", providerID, err)
	}
	health.Found = true
	health.OpenEpisodeCount = h.count(`SELECT COUNT(*) FROM provider_health_episodes WHERE provider_id = ? AND closed_at IS NULL`, string(providerID))
	health.ProviderEpisodes = h.count(`SELECT COUNT(*) FROM provider_health_episodes WHERE provider_id = ?`, string(providerID))
	health.DegradedEvents = h.count(`SELECT COUNT(*) FROM events WHERE kind = 'provider_degraded' AND provider_id = ?`, string(providerID))
	health.RecoveryEvents = h.count(`SELECT COUNT(*) FROM events WHERE kind = 'provider_recovered' AND provider_id = ?`, string(providerID))
	health.HealthEventCount = health.DegradedEvents + health.RecoveryEvents
	health.HealthDeliveryCnt = h.count(`SELECT COUNT(*) FROM notification_deliveries d
		JOIN events e ON e.id = d.event_id WHERE e.provider_id = ? AND e.kind IN ('provider_degraded','provider_recovered')`, string(providerID))
	return health
}

func (h *integrationHarness) availabilityCounts(targetID int64, providerID provider.ID) (states, openEpisodes, closedEpisodes, events, deliveries int) {
	h.t.Helper()
	return h.count(`SELECT COUNT(*) FROM inventory_states WHERE monitor_provider_id = ?`, targetID),
		h.count(`SELECT COUNT(*) FROM availability_episodes ae JOIN inventory_states s ON s.id = ae.state_id
			WHERE s.monitor_provider_id = ? AND ae.closed_at IS NULL`, targetID),
		h.count(`SELECT COUNT(*) FROM availability_episodes ae JOIN inventory_states s ON s.id = ae.state_id
			WHERE s.monitor_provider_id = ? AND ae.closed_at IS NOT NULL`, targetID),
		h.count(`SELECT COUNT(*) FROM events WHERE kind = 'availability' AND provider_id = ?`, string(providerID)),
		h.count(`SELECT COUNT(*) FROM notification_deliveries d JOIN events e ON e.id = d.event_id
			WHERE e.kind = 'availability' AND e.provider_id = ?`, string(providerID))
}

func (h *integrationHarness) monitorService(nearTerm time.Duration) *domain.MonitorService {
	h.t.Helper()
	service, err := domain.NewMonitorService(h.st, h.registry, domain.MonitorServiceConfig{
		Now: h.clock.Now,
		NearTermSchedule: func(now time.Time) time.Time {
			return now.Add(nearTerm)
		},
		Wake: func() {
			h.wakes.Add(1)
			h.wake.Wake()
		},
	})
	if err != nil {
		h.t.Fatalf("NewMonitorService: %v", err)
	}
	return service
}

func monitorInput(monitor domain.Monitor) domain.MonitorInput {
	providers := make([]domain.ProviderInput, 0, len(monitor.Providers))
	for _, target := range monitor.Providers {
		if !target.Active {
			continue
		}
		options := make([]provider.SelectedOption, 0, len(target.Selections))
		for _, selection := range target.Selections {
			options = append(options, selection.Option)
		}
		providers = append(providers, domain.ProviderInput{
			ProviderID:    target.ProviderID,
			OriginID:      target.OriginID,
			DestinationID: target.DestinationID,
			BookingSlug:   target.BookingSlug,
			GenderCode:    target.GenderCode,
			Selections:    options,
		})
	}
	return domain.MonitorInput{
		Label:           monitor.Label,
		OriginName:      monitor.OriginName,
		DestinationName: monitor.DestinationName,
		DepartureDate:   monitor.DepartureDate,
		Adults:          monitor.Adults,
		Children:        monitor.Children,
		Infants:         monitor.Infants,
		Providers:       providers,
	}
}

func (h *integrationHarness) updateMonitor(monitorID string, nearTerm time.Duration, mutate func(*domain.MonitorInput)) domain.Monitor {
	h.t.Helper()
	input := monitorInput(h.monitor(monitorID))
	mutate(&input)
	updated, err := h.monitorService(nearTerm).Update(context.Background(), monitorID, input)
	if err != nil {
		h.t.Fatalf("UpdateMonitor(%s): %v", monitorID, err)
	}
	return updated
}

func reservableResult(now time.Time, suffix string) provider.Result {
	return reservableResultWithURL(now, suffix, "")
}

func reservableResultWithURL(now time.Time, suffix, bookingURL string) provider.Result {
	observation := provider.OfferingObservation{
		Offering: provider.Offering{Key: "offering-" + suffix},
		State:    provider.StateReservable,
	}
	if bookingURL != "" {
		parsed, err := url.Parse(bookingURL)
		if err != nil {
			panic(err)
		}
		observation.BookingURL = parsed
	}
	return provider.Result{
		ObservedAt: now,
		Journeys: []provider.JourneyObservation{{
			Journey:   provider.Journey{Key: "journey-" + suffix},
			Offerings: []provider.OfferingObservation{observation},
		}},
	}
}

func notReservableResult(now time.Time, suffix string) provider.Result {
	return provider.Result{
		ObservedAt: now,
		Journeys: []provider.JourneyObservation{{
			Journey: provider.Journey{Key: "journey-" + suffix},
			Offerings: []provider.OfferingObservation{{
				Offering: provider.Offering{Key: "offering-" + suffix},
				State:    provider.StateNotReservable,
				Reason:   "integration_sold_out",
			}},
		}},
	}
}

func emptyResult(now time.Time) provider.Result {
	return provider.Result{ObservedAt: now}
}

func timeoutError() (provider.Result, error) {
	return provider.Result{}, &provider.Error{Kind: provider.ErrTimeout, Op: "integration.Check"}
}

func networkError() (provider.Result, error) {
	return provider.Result{}, &provider.Error{Kind: provider.ErrNetwork, Op: "integration.Check"}
}

func rateLimitedError() (provider.Result, error) {
	return provider.Result{}, &provider.Error{
		Kind:       provider.ErrRateLimited,
		Op:         "integration.Check",
		StatusCode: 429,
		RetryAfter: 2 * time.Second,
	}
}

func httpStatusError(status int) (provider.Result, error) {
	return provider.Result{}, &provider.Error{
		Kind:       provider.ErrHTTPStatus,
		Op:         "integration.Check",
		StatusCode: status,
	}
}

func invalidResponseError() (provider.Result, error) {
	return provider.Result{}, &provider.Error{Kind: provider.ErrInvalidResponse, Op: "integration.Check"}
}

func configurationError() (provider.Result, error) {
	return provider.Result{}, &provider.Error{Kind: provider.ErrConfiguration, Op: "integration.Check"}
}

func routeNotFoundError() (provider.Result, error) {
	return provider.Result{}, &provider.Error{Kind: provider.ErrRouteNotFound, Op: "integration.Check"}
}

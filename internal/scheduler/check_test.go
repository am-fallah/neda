package scheduler_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/provider/fake"
	"github.com/am-fallah/neda/internal/scheduler"
	"github.com/am-fallah/neda/internal/store"
)

type checkMonitorSpec struct {
	providerID    provider.ID
	originID      string
	destinationID string
	bookingSlug   string
	genderCode    string
	configVersion int64
}

func createCheckMonitor(t *testing.T, st *store.Store, id string, spec checkMonitorSpec) domain.Monitor {
	t.Helper()
	if spec.configVersion == 0 {
		spec.configVersion = 1
	}
	if spec.originID == "" {
		spec.originID = "origin-" + string(spec.providerID)
	}
	if spec.destinationID == "" {
		if spec.providerID == fake.ID {
			spec.destinationID = fake.DestinationDefault
		} else {
			spec.destinationID = "destination-" + string(spec.providerID)
		}
	}
	monitor, err := st.CreateMonitor(context.Background(), domain.Monitor{
		ID:              id,
		Label:           "Check " + id,
		OriginName:      "Tehran",
		DestinationName: "Mashhad",
		DepartureDate:   "2026-12-15",
		Adults:          2,
		Children:        1,
		Enabled:         true,
		ExpiresAt:       schedulerNow.Add(24 * time.Hour),
		CreatedAt:       schedulerNow,
		UpdatedAt:       schedulerNow,
		Providers: []domain.MonitorProvider{{
			MonitorID:     id,
			ProviderID:    spec.providerID,
			Active:        true,
			OriginID:      spec.originID,
			DestinationID: spec.destinationID,
			BookingSlug:   spec.bookingSlug,
			GenderCode:    spec.genderCode,
			ConfigVersion: spec.configVersion,
			CreatedAt:     schedulerNow,
			UpdatedAt:     schedulerNow,
		}},
	})
	if err != nil {
		t.Fatalf("CreateMonitor(%s): %v", id, err)
	}
	return monitor
}

func checkTargetFor(t *testing.T, monitor domain.Monitor, providerID provider.ID) scheduler.Target {
	t.Helper()
	targetID := targetIDFor(t, monitor, providerID)
	for _, target := range monitor.Providers {
		if target.ID == targetID {
			return scheduler.Target{
				ID:            target.ID,
				MonitorID:     monitor.ID,
				ProviderID:    providerID,
				ConfigVersion: target.ConfigVersion,
				NextDueAt:     schedulerNow,
			}
		}
	}
	t.Fatalf("monitor %s has no target for provider %s", monitor.ID, providerID)
	return scheduler.Target{}
}

type recordingProvider struct {
	metadata    provider.Metadata
	inner       provider.Provider
	validateErr error
	checkFn     func(context.Context, provider.CheckRequest) (provider.Result, error)
	onCheck     func(context.Context, provider.CheckRequest)

	mu            sync.Mutex
	validateCalls int
	checkCalls    int
	lastRequest   provider.CheckRequest
}

func newRecordingProvider(id provider.ID, inner provider.Provider) *recordingProvider {
	metadata := provider.Metadata{
		ID:          id,
		DisplayName: "Recording " + string(id),
		TestOnly:    true,
	}
	if inner != nil {
		metadata = inner.Metadata()
	}
	return &recordingProvider{metadata: metadata, inner: inner}
}

func (p *recordingProvider) Metadata() provider.Metadata { return p.metadata }

func (p *recordingProvider) ValidateConfig(cfg provider.MonitorProviderConfig) error {
	p.mu.Lock()
	p.validateCalls++
	validateErr := p.validateErr
	inner := p.inner
	p.mu.Unlock()
	if validateErr != nil {
		return validateErr
	}
	if inner != nil {
		return inner.ValidateConfig(cfg)
	}
	return nil
}

func (p *recordingProvider) Check(ctx context.Context, req provider.CheckRequest) (provider.Result, error) {
	p.mu.Lock()
	p.checkCalls++
	p.lastRequest = req
	checkFn := p.checkFn
	onCheck := p.onCheck
	inner := p.inner
	p.mu.Unlock()

	if onCheck != nil {
		onCheck(ctx, req)
	}
	if checkFn != nil {
		return checkFn(ctx, req)
	}
	if inner != nil {
		return inner.Check(ctx, req)
	}
	return provider.Result{ObservedAt: schedulerNow}, nil
}

func (p *recordingProvider) counts() (validate, check int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.validateCalls, p.checkCalls
}

func (p *recordingProvider) request() provider.CheckRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastRequest
}

func checkRegistry(t *testing.T, adapters ...provider.Provider) *provider.Registry {
	t.Helper()
	registry, err := provider.NewRegistry(adapters...)
	if err != nil {
		t.Fatalf("provider.NewRegistry: %v", err)
	}
	return registry
}

func newCheckStage(t *testing.T, st *store.Store, registry *provider.Registry, timeout time.Duration) *scheduler.CheckStage {
	t.Helper()
	stage, err := scheduler.NewCheckStage(scheduler.CheckStageConfig{
		Repository: st,
		Providers:  registry,
		Now:        func() time.Time { return schedulerNow },
		Timeout:    timeout,
		Logger:     discardLogger(),
	})
	if err != nil {
		t.Fatalf("scheduler.NewCheckStage: %v", err)
	}
	return stage
}

func assertTargetUntouched(t *testing.T, st *store.Store, monitorID string) {
	t.Helper()
	monitor, err := st.GetMonitor(context.Background(), monitorID)
	if err != nil {
		t.Fatalf("GetMonitor(%s): %v", monitorID, err)
	}
	for _, target := range monitor.Providers {
		if target.NextDueAt != nil ||
			target.LastAttemptAt != nil ||
			target.LastSuccessAt != nil ||
			target.LastOutcome != "" ||
			target.LastErrorKind != "" ||
			target.LastErrorAt != nil ||
			target.ConsecutiveFailures != 0 ||
			!target.UpdatedAt.Equal(schedulerNow) {
			t.Fatalf("check stage changed target state: %+v", target)
		}
	}
}

func countCheckRuns(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM check_runs`).Scan(&count); err != nil {
		t.Fatalf("count check runs: %v", err)
	}
	return count
}

func TestCheckStageStartsRunningRowBeforeProviderIO(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	monitor := createCheckMonitor(t, st, "check-visible", checkMonitorSpec{
		providerID: "fake",
		originID:   fake.OriginAvailable,
	})
	target := checkTargetFor(t, monitor, "fake")

	type observed struct {
		outcome       string
		configVersion int64
		startedAt     string
		err           error
	}
	var during observed
	adapter := newRecordingProvider("fake", fake.NewWithClock(func() time.Time { return schedulerNow }))
	adapter.onCheck = func(context.Context, provider.CheckRequest) {
		queryCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		during.err = sqlDB.QueryRowContext(queryCtx, `SELECT outcome, config_version, started_at
			FROM check_runs ORDER BY id DESC LIMIT 1`).Scan(
			&during.outcome, &during.configVersion, &during.startedAt,
		)
	}
	stage := newCheckStage(t, st, checkRegistry(t, adapter), 0)

	attempt, err := stage.RunCheck(context.Background(), target)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if during.err != nil {
		t.Fatalf("running row was not visible during provider I/O: %v", during.err)
	}
	if during.outcome != "running" {
		t.Fatalf("outcome during I/O = %q, want running", during.outcome)
	}
	if during.configVersion != 1 {
		t.Fatalf("config_version during I/O = %d, want 1", during.configVersion)
	}
	if attempt.CheckRunID <= 0 {
		t.Fatalf("CheckRunID = %d, want positive", attempt.CheckRunID)
	}
	if !attempt.Classification.IsSuccess() {
		t.Fatalf("classification = %+v, want success", attempt.Classification)
	}
	if count := countCheckRuns(t, sqlDB); count != 1 {
		t.Fatalf("check_runs count = %d, want 1", count)
	}
	var outcome string
	var finished sql.NullString
	if err := sqlDB.QueryRow(`SELECT outcome, finished_at FROM check_runs WHERE id = ?`, attempt.CheckRunID).
		Scan(&outcome, &finished); err != nil {
		t.Fatalf("query running row: %v", err)
	}
	if outcome != "running" || finished.Valid {
		t.Fatalf("row outcome=%q finished=%v, want running/NULL", outcome, finished)
	}
	assertTargetUntouched(t, st, monitor.ID)
}

func TestCheckStageBuildsNormalizedRequest(t *testing.T) {
	t.Run("fake", func(t *testing.T) {
		st, _ := newSchedulerStore(t)
		monitor := createCheckMonitor(t, st, "check-request-fake", checkMonitorSpec{
			providerID: "fake",
			originID:   fake.OriginAvailable,
		})
		target := checkTargetFor(t, monitor, "fake")
		adapter := newRecordingProvider("fake", fake.NewWithClock(func() time.Time { return schedulerNow }))
		stage := newCheckStage(t, st, checkRegistry(t, adapter), 0)

		attempt, err := stage.RunCheck(context.Background(), target)
		if err != nil {
			t.Fatalf("RunCheck: %v", err)
		}
		request := adapter.request()
		if request.MonitorID != monitor.ID {
			t.Fatalf("MonitorID = %q, want %q", request.MonitorID, monitor.ID)
		}
		if request.DepartureDate != monitor.DepartureDate {
			t.Fatalf("DepartureDate = %q, want %q", request.DepartureDate, monitor.DepartureDate)
		}
		if request.Route.OriginName != monitor.OriginName ||
			request.Route.DestinationName != monitor.DestinationName ||
			request.Route.OriginID != fake.OriginAvailable ||
			request.Route.DestinationID != fake.DestinationDefault {
			t.Fatalf("route = %+v", request.Route)
		}
		if request.Passengers.Adults != 2 || request.Passengers.Children != 1 || request.Passengers.Infants != 0 {
			t.Fatalf("passengers = %+v", request.Passengers)
		}
		if request.Config.OriginID != fake.OriginAvailable ||
			request.Config.DestinationID != fake.DestinationDefault ||
			request.Config.BookingSlug != "" ||
			request.Config.GenderCode != "" {
			t.Fatalf("config = %+v", request.Config)
		}
		if attempt.ConfigVersion != 1 {
			t.Fatalf("attempt ConfigVersion = %d, want 1", attempt.ConfigVersion)
		}
		if attempt.BookingSlug != "" {
			t.Fatalf("attempt BookingSlug = %q, want empty", attempt.BookingSlug)
		}
	})

	t.Run("booking and gender retained", func(t *testing.T) {
		st, sqlDB := newSchedulerStore(t)
		monitor := createCheckMonitor(t, st, "check-request-stub", checkMonitorSpec{
			providerID:    "stub",
			originID:      "origin-stub",
			destinationID: "destination-stub",
			bookingSlug:   "yazd-mashhad",
			genderCode:    "M",
			configVersion: 4,
		})
		target := checkTargetFor(t, monitor, "stub")
		// Simulate a stale scheduler dispatch snapshot. The stage must build
		// and retain the request from the freshly loaded persisted row.
		target.ConfigVersion = 1
		adapter := newRecordingProvider("stub", nil)
		stage := newCheckStage(t, st, checkRegistry(t, adapter), 0)

		attempt, err := stage.RunCheck(context.Background(), target)
		if err != nil {
			t.Fatalf("RunCheck: %v", err)
		}
		request := adapter.request()
		if request.Config.BookingSlug != "yazd-mashhad" || request.Config.GenderCode != "M" {
			t.Fatalf("config = %+v, want retained booking slug and gender", request.Config)
		}
		if attempt.BookingSlug != "yazd-mashhad" {
			t.Fatalf("attempt BookingSlug = %q, want yazd-mashhad", attempt.BookingSlug)
		}
		if attempt.ConfigVersion != 4 {
			t.Fatalf("attempt ConfigVersion = %d, want 4", attempt.ConfigVersion)
		}
		if attempt.CheckRunID <= 0 {
			t.Fatalf("CheckRunID = %d, want positive", attempt.CheckRunID)
		}
		var storedVersion int64
		if err := sqlDB.QueryRow(`SELECT config_version FROM check_runs WHERE id = ?`, attempt.CheckRunID).
			Scan(&storedVersion); err != nil {
			t.Fatalf("query stored config version: %v", err)
		}
		if storedVersion != 4 {
			t.Fatalf("stored config_version = %d, want 4", storedVersion)
		}
	})
}

func TestCheckStageClassifiesProviderOutcomes(t *testing.T) {
	tests := []struct {
		name        string
		origin      string
		category    provider.Category
		kind        provider.ErrorKind
		status      int
		retryAfter  time.Duration
		backoff     bool
		counts      bool
		emptyResult bool
	}{
		{name: "available", origin: fake.OriginAvailable, category: provider.CategorySuccess},
		{name: "empty", origin: fake.OriginEmpty, category: provider.CategorySuccess, emptyResult: true},
		{name: "sold out", origin: fake.OriginSoldOut, category: provider.CategorySuccess},
		{name: "unknown", origin: fake.OriginUnknown, category: provider.CategorySuccess},
		{name: "timeout", origin: fake.OriginTimeout, category: provider.CategoryInfrastructure, kind: provider.ErrTimeout, backoff: true, counts: true},
		{name: "network", origin: fake.OriginNetwork, category: provider.CategoryInfrastructure, kind: provider.ErrNetwork, backoff: true, counts: true},
		{name: "rate limited", origin: fake.OriginRateLimited, category: provider.CategoryInfrastructure, kind: provider.ErrRateLimited, status: 429, retryAfter: 2 * time.Second, backoff: true, counts: true},
		{name: "http 500", origin: fake.OriginHTTP500, category: provider.CategoryInfrastructure, kind: provider.ErrHTTPStatus, status: 500, backoff: true, counts: true},
		{name: "http 404", origin: fake.OriginHTTP404, category: provider.CategoryRequest, kind: provider.ErrHTTPStatus, status: 404},
		{name: "invalid response", origin: fake.OriginInvalidResponse, category: provider.CategoryInfrastructure, kind: provider.ErrInvalidResponse, backoff: true, counts: true},
		{name: "route not found", origin: fake.OriginRouteNotFound, category: provider.CategoryRoute, kind: provider.ErrRouteNotFound},
		{name: "provider canceled", origin: fake.OriginProviderCanceled, category: provider.CategoryInfrastructure, kind: provider.ErrInvalidResponse, backoff: true, counts: true},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, sqlDB := newSchedulerStore(t)
			id := fmt.Sprintf("check-outcome-%02d", i)
			monitor := createCheckMonitor(t, st, id, checkMonitorSpec{
				providerID: "fake",
				originID:   test.origin,
			})
			target := checkTargetFor(t, monitor, "fake")
			adapter := newRecordingProvider("fake", fake.NewWithClock(func() time.Time { return schedulerNow }))
			stage := newCheckStage(t, st, checkRegistry(t, adapter), 0)

			attempt, err := stage.RunCheck(context.Background(), target)
			if err != nil {
				t.Fatalf("RunCheck: %v", err)
			}
			if attempt.CheckRunID <= 0 {
				t.Fatalf("CheckRunID = %d, want positive", attempt.CheckRunID)
			}
			if attempt.Classification.Category != test.category {
				t.Fatalf("category = %q, want %q (classification %+v)", attempt.Classification.Category, test.category, attempt.Classification)
			}
			if attempt.Classification.Kind != test.kind {
				t.Fatalf("kind = %q, want %q", attempt.Classification.Kind, test.kind)
			}
			if attempt.Classification.StatusCode != test.status {
				t.Fatalf("status = %d, want %d", attempt.Classification.StatusCode, test.status)
			}
			if attempt.Classification.RetryAfter != test.retryAfter {
				t.Fatalf("retry after = %s, want %s", attempt.Classification.RetryAfter, test.retryAfter)
			}
			if attempt.Classification.TargetBackoff != test.backoff {
				t.Fatalf("TargetBackoff = %v, want %v", attempt.Classification.TargetBackoff, test.backoff)
			}
			if attempt.Classification.CountsTowardOutage != test.counts {
				t.Fatalf("CountsTowardOutage = %v, want %v", attempt.Classification.CountsTowardOutage, test.counts)
			}
			if test.category == provider.CategorySuccess {
				if attempt.Err != nil {
					t.Fatalf("success Err = %v, want nil", attempt.Err)
				}
				if test.emptyResult && len(attempt.Result.Journeys) != 0 {
					t.Fatalf("empty result journeys = %d, want 0", len(attempt.Result.Journeys))
				}
			} else if attempt.Err == nil {
				t.Fatal("error classification returned nil Err")
			}
			if count := countCheckRuns(t, sqlDB); count != 1 {
				t.Fatalf("check_runs count = %d, want 1", count)
			}
			assertTargetUntouched(t, st, monitor.ID)
		})
	}
}

func TestCheckStageCallerCancellationIsAbandoned(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	monitor := createCheckMonitor(t, st, "check-cancel", checkMonitorSpec{
		providerID:    "stub",
		originID:      "origin-stub",
		destinationID: "destination-stub",
	})
	target := checkTargetFor(t, monitor, "stub")

	started := make(chan struct{})
	adapter := newRecordingProvider("stub", nil)
	adapter.checkFn = func(ctx context.Context, _ provider.CheckRequest) (provider.Result, error) {
		close(started)
		<-ctx.Done()
		return provider.Result{}, ctx.Err()
	}
	stage := newCheckStage(t, st, checkRegistry(t, adapter), 0)

	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		attempt scheduler.CheckAttempt
		err     error
	}
	results := make(chan result, 1)
	go func() {
		attempt, err := stage.RunCheck(ctx, target)
		results <- result{attempt: attempt, err: err}
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider was not called")
	}
	cancel()
	var got result
	select {
	case got = <-results:
	case <-time.After(2 * time.Second):
		t.Fatal("RunCheck did not return after cancellation")
	}
	if got.err != nil {
		t.Fatalf("RunCheck error = %v, want nil", got.err)
	}
	if !got.attempt.Classification.IsAbandoned() {
		t.Fatalf("classification = %+v, want abandoned", got.attempt.Classification)
	}
	if got.attempt.Classification.Category != provider.CategoryAbandoned ||
		got.attempt.Classification.Kind != provider.ErrCanceled {
		t.Fatalf("classification = %+v, want abandoned/canceled", got.attempt.Classification)
	}
	if got.attempt.CheckRunID <= 0 {
		t.Fatalf("CheckRunID = %d, want positive", got.attempt.CheckRunID)
	}
	if count := countCheckRuns(t, sqlDB); count != 1 {
		t.Fatalf("check_runs count = %d, want 1", count)
	}
	var outcome string
	if err := sqlDB.QueryRow(`SELECT outcome FROM check_runs WHERE id = ?`, got.attempt.CheckRunID).Scan(&outcome); err != nil {
		t.Fatalf("query check run: %v", err)
	}
	if outcome != "running" {
		t.Fatalf("outcome = %q, want running", outcome)
	}
	assertTargetUntouched(t, st, monitor.ID)
}

func TestCheckStageDeadlineIsTimeoutNotAbandoned(t *testing.T) {
	slowProvider := func(ctx context.Context, _ provider.CheckRequest) (provider.Result, error) {
		for {
			select {
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			case <-time.After(5 * time.Millisecond):
			}
		}
	}

	t.Run("injected stage timeout", func(t *testing.T) {
		st, sqlDB := newSchedulerStore(t)
		monitor := createCheckMonitor(t, st, "check-deadline-stage", checkMonitorSpec{
			providerID:    "stub",
			originID:      "origin-stub",
			destinationID: "destination-stub",
		})
		target := checkTargetFor(t, monitor, "stub")
		adapter := newRecordingProvider("stub", nil)
		adapter.checkFn = slowProvider
		stage := newCheckStage(t, st, checkRegistry(t, adapter), 40*time.Millisecond)

		start := time.Now()
		attempt, err := stage.RunCheck(context.Background(), target)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("RunCheck: %v", err)
		}
		if elapsed < 30*time.Millisecond || elapsed > 5*time.Second {
			t.Fatalf("elapsed = %s, want about 40ms", elapsed)
		}
		if !attempt.Classification.IsInfrastructure() || attempt.Classification.Kind != provider.ErrTimeout {
			t.Fatalf("classification = %+v, want infrastructure timeout", attempt.Classification)
		}
		if attempt.Classification.IsAbandoned() {
			t.Fatal("deadline was classified as abandoned")
		}
		if count := countCheckRuns(t, sqlDB); count != 1 {
			t.Fatalf("check_runs count = %d, want 1", count)
		}
	})

	t.Run("shorter caller deadline", func(t *testing.T) {
		st, _ := newSchedulerStore(t)
		monitor := createCheckMonitor(t, st, "check-deadline-caller", checkMonitorSpec{
			providerID:    "stub",
			originID:      "origin-stub",
			destinationID: "destination-stub",
		})
		target := checkTargetFor(t, monitor, "stub")
		adapter := newRecordingProvider("stub", nil)
		adapter.checkFn = slowProvider
		stage := newCheckStage(t, st, checkRegistry(t, adapter), time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		defer cancel()
		attempt, err := stage.RunCheck(ctx, target)
		if err != nil {
			t.Fatalf("RunCheck: %v", err)
		}
		if !attempt.Classification.IsInfrastructure() || attempt.Classification.Kind != provider.ErrTimeout {
			t.Fatalf("classification = %+v, want infrastructure timeout", attempt.Classification)
		}
		if attempt.Classification.IsAbandoned() {
			t.Fatal("deadline was classified as abandoned")
		}
	})
}

func TestCheckStageRetainsStartedAttemptWhenConfigValidationFails(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"typed provider error", &provider.Error{Kind: provider.ErrConfiguration, Op: "stub.ValidateConfig"}},
		{"untyped error", errors.New("invalid provider configuration")},
		{"misclassified typed error", &provider.Error{Kind: provider.ErrNetwork, Op: "stub.ValidateConfig"}},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, sqlDB := newSchedulerStore(t)
			monitor := createCheckMonitor(t, st, fmt.Sprintf("check-config-failure-%d", i), checkMonitorSpec{
				providerID:    "stub",
				originID:      "origin-stub",
				destinationID: "destination-stub",
			})
			target := checkTargetFor(t, monitor, "stub")
			adapter := newRecordingProvider("stub", nil)
			adapter.validateErr = test.err
			stage := newCheckStage(t, st, checkRegistry(t, adapter), 0)

			attempt, err := stage.RunCheck(context.Background(), target)
			if err != nil {
				t.Fatalf("RunCheck: %v", err)
			}
			if attempt.CheckRunID <= 0 {
				t.Fatalf("CheckRunID = %d, want positive", attempt.CheckRunID)
			}
			if attempt.Err == nil {
				t.Fatal("attempt Err is nil, want configuration error")
			}
			if attempt.Classification.Category != provider.CategoryConfiguration ||
				attempt.Classification.Kind != provider.ErrConfiguration ||
				attempt.Classification.TargetBackoff ||
				attempt.Classification.CountsTowardOutage {
				t.Fatalf("classification = %+v, want non-infrastructure configuration", attempt.Classification)
			}
			validateCalls, checkCalls := adapter.counts()
			if validateCalls != 1 || checkCalls != 0 {
				t.Fatalf("calls: validate=%d check=%d, want 1/0", validateCalls, checkCalls)
			}
			if count := countCheckRuns(t, sqlDB); count != 1 {
				t.Fatalf("check_runs count = %d, want 1", count)
			}
			var outcome string
			if err := sqlDB.QueryRow(`SELECT outcome FROM check_runs WHERE id = ?`, attempt.CheckRunID).Scan(&outcome); err != nil {
				t.Fatalf("query check run: %v", err)
			}
			if outcome != "running" {
				t.Fatalf("outcome = %q, want running", outcome)
			}
			assertTargetUntouched(t, st, monitor.ID)
		})
	}
}

func TestCheckStageMissingTargetSkipsProvider(t *testing.T) {
	t.Run("target deleted", func(t *testing.T) {
		st, sqlDB := newSchedulerStore(t)
		monitor := createCheckMonitor(t, st, "check-missing-target", checkMonitorSpec{
			providerID:    "stub",
			originID:      "origin-stub",
			destinationID: "destination-stub",
		})
		target := checkTargetFor(t, monitor, "stub")
		if _, err := sqlDB.Exec(`DELETE FROM monitor_providers WHERE id = ?`, target.ID); err != nil {
			t.Fatalf("delete target: %v", err)
		}
		adapter := newRecordingProvider("stub", nil)
		stage := newCheckStage(t, st, checkRegistry(t, adapter), 0)

		attempt, err := stage.RunCheck(context.Background(), target)
		if err == nil {
			t.Fatal("RunCheck succeeded, want missing-target error")
		}
		if attempt.CheckRunID != 0 {
			t.Fatalf("CheckRunID = %d, want 0", attempt.CheckRunID)
		}
		_, checkCalls := adapter.counts()
		if checkCalls != 0 {
			t.Fatalf("provider Check calls = %d, want 0", checkCalls)
		}
		if count := countCheckRuns(t, sqlDB); count != 0 {
			t.Fatalf("check_runs count = %d, want 0", count)
		}
	})

	t.Run("monitor deleted", func(t *testing.T) {
		st, sqlDB := newSchedulerStore(t)
		monitor := createCheckMonitor(t, st, "check-missing-monitor", checkMonitorSpec{
			providerID:    "stub",
			originID:      "origin-stub",
			destinationID: "destination-stub",
		})
		target := checkTargetFor(t, monitor, "stub")
		if _, err := sqlDB.Exec(`DELETE FROM monitors WHERE id = ?`, monitor.ID); err != nil {
			t.Fatalf("delete monitor: %v", err)
		}
		adapter := newRecordingProvider("stub", nil)
		stage := newCheckStage(t, st, checkRegistry(t, adapter), 0)

		attempt, err := stage.RunCheck(context.Background(), target)
		if err == nil {
			t.Fatal("RunCheck succeeded, want missing-monitor error")
		}
		if attempt.CheckRunID != 0 {
			t.Fatalf("CheckRunID = %d, want 0", attempt.CheckRunID)
		}
		_, checkCalls := adapter.counts()
		if checkCalls != 0 {
			t.Fatalf("provider Check calls = %d, want 0", checkCalls)
		}
		if count := countCheckRuns(t, sqlDB); count != 0 {
			t.Fatalf("check_runs count = %d, want 0", count)
		}
	})
}

func TestCheckStageWritesNoUnrelatedState(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	monitor := createCheckMonitor(t, st, "check-no-other-writes", checkMonitorSpec{
		providerID:    "stub",
		originID:      "origin-stub",
		destinationID: "destination-stub",
	})
	target := checkTargetFor(t, monitor, "stub")

	var calls int
	adapter := newRecordingProvider("stub", nil)
	adapter.checkFn = func(context.Context, provider.CheckRequest) (provider.Result, error) {
		calls++
		if calls == 1 {
			return provider.Result{ObservedAt: schedulerNow}, nil
		}
		return provider.Result{}, &provider.Error{Kind: provider.ErrNetwork, Op: "stub.Check"}
	}
	stage := newCheckStage(t, st, checkRegistry(t, adapter), 0)

	for i := 0; i < 2; i++ {
		attempt, err := stage.RunCheck(context.Background(), target)
		if err != nil {
			t.Fatalf("RunCheck %d: %v", i, err)
		}
		if attempt.CheckRunID <= 0 {
			t.Fatalf("CheckRunID %d = %d, want positive", i, attempt.CheckRunID)
		}
	}
	if count := countCheckRuns(t, sqlDB); count != 2 {
		t.Fatalf("check_runs count = %d, want 2", count)
	}
	for _, table := range []string{"inventory_states", "availability_episodes", "events"} {
		var count int
		query := map[string]string{
			"inventory_states":      `SELECT COUNT(*) FROM inventory_states`,
			"availability_episodes": `SELECT COUNT(*) FROM availability_episodes`,
			"events":                `SELECT COUNT(*) FROM events`,
		}[table]
		if err := sqlDB.QueryRow(query).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", table, count)
		}
	}
	assertTargetUntouched(t, st, monitor.ID)
}

func TestCheckStageDefaultTimeoutIsTwentySeconds(t *testing.T) {
	st, _ := newSchedulerStore(t)
	monitor := createCheckMonitor(t, st, "check-default-timeout", checkMonitorSpec{
		providerID:    "stub",
		originID:      "origin-stub",
		destinationID: "destination-stub",
	})
	target := checkTargetFor(t, monitor, "stub")

	var remaining time.Duration
	var hasDeadline bool
	adapter := newRecordingProvider("stub", nil)
	adapter.checkFn = func(ctx context.Context, _ provider.CheckRequest) (provider.Result, error) {
		deadline, ok := ctx.Deadline()
		hasDeadline = ok
		if ok {
			remaining = time.Until(deadline)
		}
		return provider.Result{ObservedAt: schedulerNow}, nil
	}
	stage := newCheckStage(t, st, checkRegistry(t, adapter), 0)

	if _, err := stage.RunCheck(context.Background(), target); err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if !hasDeadline {
		t.Fatal("provider context has no deadline")
	}
	if remaining <= 15*time.Second || remaining > 20*time.Second {
		t.Fatalf("remaining deadline = %s, want about 20s", remaining)
	}
}

func TestCheckStagePreCanceledContextStartsNothing(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	monitor := createCheckMonitor(t, st, "check-pre-canceled", checkMonitorSpec{
		providerID:    "stub",
		originID:      "origin-stub",
		destinationID: "destination-stub",
	})
	target := checkTargetFor(t, monitor, "stub")
	adapter := newRecordingProvider("stub", nil)
	stage := newCheckStage(t, st, checkRegistry(t, adapter), 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempt, err := stage.RunCheck(ctx, target)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if attempt.CheckRunID != 0 {
		t.Fatalf("CheckRunID = %d, want 0", attempt.CheckRunID)
	}
	if !attempt.Classification.IsAbandoned() {
		t.Fatalf("classification = %+v, want abandoned", attempt.Classification)
	}
	_, checkCalls := adapter.counts()
	if checkCalls != 0 {
		t.Fatalf("provider Check calls = %d, want 0", checkCalls)
	}
	if count := countCheckRuns(t, sqlDB); count != 0 {
		t.Fatalf("check_runs count = %d, want 0", count)
	}
}

func TestCheckStagePreExpiredDeadlineIsTimeout(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	monitor := createCheckMonitor(t, st, "check-pre-expired", checkMonitorSpec{
		providerID:    "stub",
		originID:      "origin-stub",
		destinationID: "destination-stub",
	})
	target := checkTargetFor(t, monitor, "stub")
	adapter := newRecordingProvider("stub", nil)
	stage := newCheckStage(t, st, checkRegistry(t, adapter), 0)

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	attempt, err := stage.RunCheck(ctx, target)
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if attempt.CheckRunID != 0 {
		t.Fatalf("CheckRunID = %d, want 0", attempt.CheckRunID)
	}
	if !attempt.Classification.IsInfrastructure() || attempt.Classification.Kind != provider.ErrTimeout {
		t.Fatalf("classification = %+v, want infrastructure timeout", attempt.Classification)
	}
	if attempt.Classification.IsAbandoned() {
		t.Fatal("pre-expired deadline was classified as abandoned")
	}
	_, checkCalls := adapter.counts()
	if checkCalls != 0 {
		t.Fatalf("provider Check calls = %d, want 0", checkCalls)
	}
	if count := countCheckRuns(t, sqlDB); count != 0 {
		t.Fatalf("check_runs count = %d, want 0", count)
	}
}

func TestCheckStageUnregisteredProviderStartsNothing(t *testing.T) {
	st, sqlDB := newSchedulerStore(t)
	monitor := createCheckMonitor(t, st, "check-unregistered", checkMonitorSpec{
		providerID:    "stub",
		originID:      "origin-stub",
		destinationID: "destination-stub",
	})
	target := checkTargetFor(t, monitor, "stub")

	other := newRecordingProvider("other", nil)
	stage := newCheckStage(t, st, checkRegistry(t, other), 0)
	if _, err := stage.RunCheck(context.Background(), target); err == nil {
		t.Fatal("RunCheck succeeded with unregistered provider")
	}
	if count := countCheckRuns(t, sqlDB); count != 0 {
		t.Fatalf("check_runs count = %d, want 0", count)
	}
}

func TestNewCheckStageValidatesConfig(t *testing.T) {
	st, _ := newSchedulerStore(t)
	registry := checkRegistry(t)

	if _, err := scheduler.NewCheckStage(scheduler.CheckStageConfig{}); !errors.Is(err, scheduler.ErrNilCheckRepository) {
		t.Fatalf("nil repository error = %v, want ErrNilCheckRepository", err)
	}
	if _, err := scheduler.NewCheckStage(scheduler.CheckStageConfig{Repository: st}); !errors.Is(err, scheduler.ErrNilProviders) {
		t.Fatalf("nil providers error = %v, want ErrNilProviders", err)
	}
	if _, err := scheduler.NewCheckStage(scheduler.CheckStageConfig{
		Repository: st,
		Providers:  registry,
		Timeout:    -time.Second,
	}); err == nil {
		t.Fatal("negative timeout succeeded, want error")
	}
}

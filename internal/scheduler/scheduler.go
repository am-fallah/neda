// Package scheduler implements Neda's single-process, due-time scheduler and
// the unwired SCH-004 provider-check stage.
//
// The scheduler owns due selection, monitor-wide serialization, the bounded
// worker pool, startup recovery/stagger, and the timer-plus-wake loop. The
// CheckStage owns one attempt-start row plus provider I/O, but it is not
// registered as the production Runner. The package does not write
// availability/health state, finalize check outcomes, or deliver
// notifications; those responsibilities belong to SCH-010 and later work.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
)

const (
	// DefaultWorkers is the default fixed worker-pool size.
	DefaultWorkers = 2
	// MaxWorkers bounds the configurable worker-pool size.
	MaxWorkers = 16
	// DefaultShutdownGrace bounds how long Run waits for in-flight runners.
	DefaultShutdownGrace = 5 * time.Second
	// DefaultProviderSpacing is the provisional start-to-start spacing per
	// provider. It is not a provider-published limit.
	DefaultProviderSpacing = 5 * time.Second

	// storeErrorRetry prevents a tight loop when due selection fails.
	storeErrorRetry = time.Second

	// unsettledRetryDelay is a protective pause before re-evaluating a target
	// whose runner reported an unsettled result. It is loop protection, not the
	// target backoff policy owned by SCH-003.
	unsettledRetryDelay = time.Second
)

var (
	// ErrNilRepository reports a missing schedule repository.
	ErrNilRepository = errors.New("scheduler: nil repository")
	// ErrNilProviders reports a missing provider lookup.
	ErrNilProviders = errors.New("scheduler: nil provider lookup")
	// ErrNilInterval reports a missing poll-interval source.
	ErrNilInterval = errors.New("scheduler: nil poll interval source")
)

// Target is one due monitor/provider pair.
type Target struct {
	ID            int64
	MonitorID     string
	ProviderID    provider.ID
	ConfigVersion int64
	NextDueAt     time.Time
}

// Result reports how the scheduler should treat a completed attempt.
type Result struct {
	// Settled means this due generation must not be dispatched again in this
	// process. The runner sets it after writing the applicable scheduling
	// update (success, empty, or provider failure) or for a terminal abandoned
	// attempt. It remains false for SCH-010's discarded outcome, where no
	// scheduling update was written and the target should be re-evaluated
	// after persisted state changes (for example, a booking-slug change).
	Settled bool
}

// Runner executes exactly one check lifecycle for a target. SCH-010 will
// compose the SCH-004 CheckStage into the production implementation.
//
// The runner owns any target scheduling update produced by the check and
// reports whether the due generation was consumed. Returned errors are
// diagnostic only; they are not provider verdicts and do not change
// availability, health, or scheduling by themselves.
type Runner interface {
	Run(ctx context.Context, target Target) (Result, error)
}

// ProviderLookup reports whether a provider is registered. The scheduler does
// not call provider methods and does not interpret provider results.
type ProviderLookup interface {
	Lookup(id provider.ID) (provider.Provider, bool)
}

// IntervalSource supplies the current global polling interval. In production
// this reads the SQLite settings row; tests inject a fixed value.
type IntervalSource interface {
	PollInterval(ctx context.Context) (time.Duration, error)
}

// Repository is the persistence boundary used by the scheduler.
type Repository interface {
	ListScheduleTargets(ctx context.Context, now time.Time) ([]domain.ScheduleTarget, error)
	RecoverRunningChecks(ctx context.Context, now time.Time) (int64, error)
	StaggerDueTargets(ctx context.Context, now time.Time, planner func(domain.ScheduleTarget) time.Time) (int64, error)
}

// Clock abstracts time and timers for deterministic tests.
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
}

// Timer is the subset of *time.Timer used by the scheduler.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// Random is the minimal randomness source used by jitter. Tests inject a
// scripted implementation.
type Random interface {
	Int64N(n int64) int64
}

// Config controls the scheduler. Zero values receive safe defaults except for
// the repository, provider lookup, and interval source, which are required.
type Config struct {
	Repository Repository
	Providers  ProviderLookup
	Interval   IntervalSource
	Runner     Runner // nil keeps the scheduler safely idle after recovery
	Workers    int
	// ProviderSpacing is the start-to-start minimum per provider. Nil uses
	// DefaultProviderSpacing; a non-nil zero explicitly disables spacing for
	// deterministic tests. Production uses the nil default.
	ProviderSpacing *time.Duration
	Wake            <-chan struct{}
	Clock           Clock
	Random          Random
	Logger          *slog.Logger
	ShutdownGrace   time.Duration
}

// Scheduler is a single-process due-time scheduler.
type Scheduler struct {
	repo            Repository
	providers       ProviderLookup
	interval        IntervalSource
	runner          Runner
	workers         int
	providerSpacing time.Duration
	wake            <-chan struct{}
	clock           Clock
	random          Random
	logger          *slog.Logger
	shutdownGrace   time.Duration
}

// New validates dependencies and applies defaults.
func New(cfg Config) (*Scheduler, error) {
	if cfg.Repository == nil {
		return nil, ErrNilRepository
	}
	if cfg.Providers == nil {
		return nil, ErrNilProviders
	}
	if cfg.Interval == nil {
		return nil, ErrNilInterval
	}
	if cfg.Workers == 0 {
		cfg.Workers = DefaultWorkers
	}
	if cfg.Workers < 1 || cfg.Workers > MaxWorkers {
		return nil, fmt.Errorf("scheduler: workers must be between 1 and %d", MaxWorkers)
	}
	if cfg.ProviderSpacing == nil {
		defaultSpacing := DefaultProviderSpacing
		cfg.ProviderSpacing = &defaultSpacing
	}
	if *cfg.ProviderSpacing < 0 {
		return nil, errors.New("scheduler: provider spacing must not be negative")
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	if cfg.Random == nil {
		cfg.Random = defaultRandom{}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.ShutdownGrace == 0 {
		cfg.ShutdownGrace = DefaultShutdownGrace
	}
	if cfg.ShutdownGrace < 0 {
		return nil, errors.New("scheduler: shutdown grace must not be negative")
	}
	return &Scheduler{
		repo:            cfg.Repository,
		providers:       cfg.Providers,
		interval:        cfg.Interval,
		runner:          cfg.Runner,
		workers:         cfg.Workers,
		providerSpacing: *cfg.ProviderSpacing,
		wake:            cfg.Wake,
		clock:           cfg.Clock,
		random:          cfg.Random,
		logger:          cfg.Logger,
		shutdownGrace:   cfg.ShutdownGrace,
	}, nil
}

// Run performs startup recovery, optionally staggers due targets, and then
// serves the due-time loop until ctx is cancelled. It returns nil for a
// graceful cancellation. A nil Runner performs recovery only and then remains
// safely idle, so production can start the scheduler before SCH-010 composes
// the SCH-004 check stage into a runner without dispatching no-op jobs or
// busy-looping.
func (s *Scheduler) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("scheduler: nil context")
	}
	if ctx.Err() != nil {
		return nil
	}

	now := s.clock.Now().UTC()
	recovered, err := s.repo.RecoverRunningChecks(ctx, now)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("scheduler: recover running checks: %w", err)
	}
	if recovered > 0 {
		s.logger.Info("scheduler recovered interrupted checks", "count", recovered)
	}

	if s.runner == nil {
		s.logger.Info("scheduler idle: no check runner configured")
		<-ctx.Done()
		return nil
	}

	interval, err := s.interval.PollInterval(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("scheduler: read poll interval: %w", err)
	}
	if interval <= 0 {
		return errors.New("scheduler: poll interval must be positive")
	}
	staggered, err := s.repo.StaggerDueTargets(ctx, now, func(domain.ScheduleTarget) time.Time {
		return now.Add(NearTermDelay(interval, s.random))
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("scheduler: stagger due targets: %w", err)
	}
	if staggered > 0 {
		s.logger.Info("scheduler staggered due targets", "count", staggered)
	}

	return s.runLoop(ctx)
}

type completion struct {
	target Target
	result Result
}

func (s *Scheduler) runLoop(ctx context.Context) error {
	jobs := make(chan Target, s.workers)
	done := make(chan completion, s.workers)

	var workers sync.WaitGroup
	workers.Add(s.workers)
	for worker := 0; worker < s.workers; worker++ {
		go func() {
			defer workers.Done()
			for target := range jobs {
				if ctx.Err() != nil {
					return
				}
				result, _ := s.execute(ctx, target)
				select {
				case done <- completion{target: target, result: result}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	loopErr := s.loop(ctx, jobs, done)
	close(jobs)

	finished := make(chan struct{})
	go func() {
		workers.Wait()
		close(finished)
	}()
	shutdown := time.NewTimer(s.shutdownGrace)
	defer shutdown.Stop()
	select {
	case <-finished:
	case <-shutdown.C:
		s.logger.Warn("scheduler shutdown grace expired", "grace", s.shutdownGrace.String())
	}
	return loopErr
}

func (s *Scheduler) execute(ctx context.Context, target Target) (result Result, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = Result{}
			err = fmt.Errorf("scheduler: runner panic: %v", recovered)
		}
		if err != nil {
			s.logger.Error("scheduler check failed",
				"monitor_id", target.MonitorID,
				"target_id", target.ID,
				"provider_id", string(target.ProviderID),
				"error", err,
			)
		}
	}()
	return s.runner.Run(ctx, target)
}

func (s *Scheduler) loop(ctx context.Context, jobs chan<- Target, done <-chan completion) error {
	running := 0
	inflight := make(map[string]bool)
	settled := make(map[int64]generation)
	hold := make(map[int64]time.Time)
	providers := make(map[provider.ID]*providerRuntime)

	for {
		// Apply all completed checks before selecting new work.
	drain:
		for {
			select {
			case completed := <-done:
				s.complete(completed, &running, inflight, settled, hold, providers)
			default:
				break drain
			}
		}

		if ctx.Err() != nil {
			return nil
		}

		now := s.clock.Now().UTC()
		targets, err := s.repo.ListScheduleTargets(ctx, now)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.logger.Error("scheduler due selection failed", "error", err)
			completed, got, ok := s.waitUntil(ctx, now.Add(storeErrorRetry), true, done)
			if !ok {
				return nil
			}
			if got {
				s.complete(completed, &running, inflight, settled, hold, providers)
			}
			continue
		}
		sortTargets(targets)

		present := make(map[int64]bool, len(targets))
		for _, target := range targets {
			present[target.ID] = true
		}
		for id := range settled {
			if !present[id] {
				delete(settled, id)
			}
		}
		for id := range hold {
			if !present[id] {
				delete(hold, id)
			}
		}

		var next time.Time
		hasNext := false
		for _, candidate := range targets {
			if candidate.NextDueAt == nil {
				continue
			}
			if _, ok := s.providers.Lookup(candidate.ProviderID); !ok {
				continue
			}
			due := candidate.NextDueAt.UTC()
			if due.After(now) {
				if !hasNext || due.Before(next) {
					next = due
					hasNext = true
				}
				continue
			}
			if until, ok := hold[candidate.ID]; ok {
				if until.After(now) {
					if !hasNext || until.Before(next) {
						next = until
						hasNext = true
					}
					continue
				}
				delete(hold, candidate.ID)
			}
			if running >= s.workers || inflight[candidate.MonitorID] {
				continue
			}
			if generation, ok := settled[candidate.ID]; ok &&
				generation.configVersion == candidate.ConfigVersion &&
				generation.due.Equal(due) {
				continue
			}
			state := providerState(providers, candidate.ProviderID)
			if state.inFlight {
				continue
			}
			if slot, ok := state.nextSlot(s.providerSpacing); ok && slot.After(now) {
				if !hasNext || slot.Before(next) {
					next = slot
					hasNext = true
				}
				continue
			}
			target := Target{
				ID:            candidate.ID,
				MonitorID:     candidate.MonitorID,
				ProviderID:    candidate.ProviderID,
				ConfigVersion: candidate.ConfigVersion,
				NextDueAt:     due,
			}
			// Sample the dispatch time here, after due selection, so slow
			// repository reads cannot shorten the start-to-start spacing.
			dispatchAt := s.clock.Now().UTC()
			select {
			case jobs <- target:
				delete(settled, candidate.ID)
				inflight[candidate.MonitorID] = true
				state.dispatch(dispatchAt)
				running++
				s.logger.Debug("scheduler dispatched check",
					"monitor_id", target.MonitorID,
					"target_id", target.ID,
					"provider_id", string(target.ProviderID),
				)
			case <-ctx.Done():
				return nil
			}
		}

		var completed completion
		var got bool
		var ok bool
		switch {
		case running >= s.workers:
			// Capacity is the only blocker; a completion frees a worker.
			completed, got, ok = s.waitUntil(ctx, time.Time{}, false, done)
		case hasNext:
			completed, got, ok = s.waitUntil(ctx, next, true, done)
		default:
			completed, got, ok = s.waitUntil(ctx, time.Time{}, false, done)
		}
		if !ok {
			return nil
		}
		if got {
			s.complete(completed, &running, inflight, settled, hold, providers)
		}
	}
}

func (s *Scheduler) complete(completed completion, running *int, inflight map[string]bool, settled map[int64]generation, hold map[int64]time.Time, providers map[provider.ID]*providerRuntime) {
	if *running > 0 {
		*running--
	}
	delete(inflight, completed.target.MonitorID)
	providerState(providers, completed.target.ProviderID).complete()
	if completed.result.Settled {
		delete(hold, completed.target.ID)
		settled[completed.target.ID] = generation{
			configVersion: completed.target.ConfigVersion,
			due:           completed.target.NextDueAt,
		}
		return
	}
	// The runner did not consume this due generation (for example, a discarded
	// attempt). Re-read persisted state after a short protective pause rather
	// than spinning on the same due time.
	delete(settled, completed.target.ID)
	hold[completed.target.ID] = s.clock.Now().Add(unsettledRetryDelay)
}

type generation struct {
	configVersion int64
	due           time.Time
}

// waitUntil blocks until the next timer, a wake, a completion, or cancellation.
// It returns (completion, receivedCompletion, stillRunning).
func (s *Scheduler) waitUntil(ctx context.Context, until time.Time, hasUntil bool, done <-chan completion) (completion, bool, bool) {
	var timer Timer
	var timerC <-chan time.Time
	if hasUntil {
		delay := until.Sub(s.clock.Now())
		if delay < 0 {
			delay = 0
		}
		timer = s.clock.NewTimer(delay)
		timerC = timer.C()
		defer timer.Stop()
	}

	select {
	case <-ctx.Done():
		return completion{}, false, false
	case <-s.wake:
		return completion{}, false, true
	case completed := <-done:
		return completed, true, true
	case <-timerC:
		return completion{}, false, true
	}
}

func sortTargets(targets []domain.ScheduleTarget) {
	sort.Slice(targets, func(i, j int) bool {
		left, right := targets[i], targets[j]
		switch {
		case left.NextDueAt == nil && right.NextDueAt != nil:
			return false
		case left.NextDueAt != nil && right.NextDueAt == nil:
			return true
		case left.NextDueAt != nil && right.NextDueAt != nil && !left.NextDueAt.Equal(*right.NextDueAt):
			return left.NextDueAt.Before(*right.NextDueAt)
		default:
			return left.ID < right.ID
		}
	})
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTimer(duration time.Duration) Timer {
	return &realTimer{timer: time.NewTimer(duration)}
}

type realTimer struct {
	timer *time.Timer
}

func (t *realTimer) C() <-chan time.Time { return t.timer.C }

func (t *realTimer) Stop() bool { return t.timer.Stop() }

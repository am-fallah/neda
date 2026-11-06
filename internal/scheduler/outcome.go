package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/am-fallah/neda/internal/availability"
	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/health"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/selection"
	"github.com/am-fallah/neda/internal/store"
)

const outcomeFinalizeTimeout = 5 * time.Second

// DeliverySource supplies the destination identities for one committed
// outcome. It runs inside the outcome transaction and must not perform network
// or long-running work. A nil source means no configured channels; events are
// still persisted without delivery rows until notification-channel tasks add a
// source. A non-nil source error fails the outcome transaction so a valid check
// is never committed with a partially resolved outbox.
type DeliverySource func(ctx context.Context, tx *sql.Tx) ([]notify.Delivery, error)

// RateLimitEventSink receives best-effort adaptive/cooldown transition events
// after the outcome transaction commits. Implementations must be non-blocking;
// failures must never affect the outcome.
type RateLimitEventSink interface {
	RecordRateLimitEvent(event store.ProviderRateLimitEvent)
}

// OutcomeRunnerConfig controls the SCH-010 per-check outcome orchestrator.
type OutcomeRunnerConfig struct {
	Stage           *CheckStage
	Store           *store.Store
	Monitoring      MonitoringSwitch
	Deliveries      DeliverySource
	Wake            func()
	Now             func() time.Time
	Random          Random
	Adaptive        AdaptivePolicy
	Logger          *slog.Logger
	RateLimitEvents RateLimitEventSink
}

// OutcomeRunner composes the SCH-004 check stage with the caller-owned
// outcome transaction. It is the production scheduler Runner once wired. The
// scheduler loop itself still never interprets provider results or writes
// state.
type OutcomeRunner struct {
	stage           *CheckStage
	store           *store.Store
	monitoring      MonitoringSwitch
	deliveries      DeliverySource
	wake            func()
	now             func() time.Time
	random          Random
	adaptive        AdaptivePolicy
	logger          *slog.Logger
	rateLimitEvents RateLimitEventSink
}

var _ Runner = (*OutcomeRunner)(nil)

var (
	ErrNilOutcomeStage = errors.New("scheduler: nil outcome check stage")
	ErrNilOutcomeStore = errors.New("scheduler: nil outcome store")
)

// outcomeFailure keeps scheduler/operator logs sanitized while preserving the
// underlying error for local inspection through errors.Is/As.
type outcomeFailure struct {
	code string
	err  error
}

func (e *outcomeFailure) Error() string { return "scheduler: " + e.code }
func (e *outcomeFailure) Unwrap() error { return e.err }

func newOutcomeFailure(code string, err error) error {
	return &outcomeFailure{code: code, err: err}
}

// NewOutcomeRunner validates dependencies and applies safe defaults.
func NewOutcomeRunner(cfg OutcomeRunnerConfig) (*OutcomeRunner, error) {
	if cfg.Stage == nil {
		return nil, ErrNilOutcomeStage
	}
	if cfg.Store == nil {
		return nil, ErrNilOutcomeStore
	}
	if cfg.Wake == nil {
		cfg.Wake = func() {}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Random == nil {
		cfg.Random = defaultRandom{}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Adaptive.MaxInterval < 0 {
		return nil, errors.New("scheduler: adaptive max interval must not be negative")
	}
	if cfg.Adaptive.RecoverySuccesses < 0 {
		return nil, errors.New("scheduler: adaptive recovery successes must not be negative")
	}
	if cfg.Adaptive.MaxEscalations < 0 {
		return nil, errors.New("scheduler: adaptive max escalations must not be negative")
	}
	return &OutcomeRunner{
		stage:           cfg.Stage,
		store:           cfg.Store,
		monitoring:      cfg.Monitoring,
		deliveries:      cfg.Deliveries,
		wake:            cfg.Wake,
		now:             cfg.Now,
		random:          cfg.Random,
		adaptive:        cfg.Adaptive,
		logger:          cfg.Logger,
		rateLimitEvents: cfg.RateLimitEvents,
	}, nil
}

// Run executes one check attempt and applies exactly one final outcome.
// Provider I/O remains outside every transaction. Any post-start failure to
// finalize returns Settled=true so the scheduler does not immediately start a
// second attempt while the original row remains running for startup recovery.
func (r *OutcomeRunner) Run(ctx context.Context, target Target) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("scheduler: nil outcome context")
	}
	attempt, err := r.stage.RunCheck(ctx, target)
	if err != nil {
		r.logOutcomeError("check stage failed", attempt, "check_stage_failed")
		return Result{Settled: false}, err
	}
	if attempt.CheckRunID == 0 {
		if attempt.MonitoringDisabled {
			// No running row was committed because the global switch turned off
			// (or could not be read) before provider I/O. Leave the due generation
			// unconsumed; the scheduler's off gate prevents a retry loop.
			r.logger.Info("check skipped before provider I/O: global monitoring is off",
				"monitor_id", attempt.MonitorID,
				"target_id", attempt.TargetID,
				"provider_id", string(attempt.ProviderID),
			)
			return Result{Settled: false}, nil
		}
		// No running row was committed (for example, a pre-canceled context).
		return Result{Settled: true}, nil
	}

	// A check that was already executing when the switch turned off is not
	// allowed to mutate availability/health state or enqueue notifications. It
	// is finalized as discarded with a stable reason. A switch read failure fails
	// closed for the same reason.
	if r.monitoring != nil {
		enabled, err := r.monitoring.MonitoringEnabled(ctx)
		if err != nil {
			r.logOutcomeError("monitoring switch read failed", attempt, "monitoring_state_unavailable")
			return r.finalizeDiscarded(ctx, attempt, "monitoring_state_unavailable")
		}
		if !enabled {
			return r.finalizeDiscarded(ctx, attempt, "monitoring_disabled")
		}
	}

	if attempt.Err == nil {
		if validationErr := provider.ValidateResult(attempt.Result); validationErr != nil {
			attempt.Err = &provider.Error{Kind: provider.ErrInvalidResponse, Op: "ValidateResult", Err: validationErr}
			attempt.Classification = provider.Classify(context.Background(), attempt.Err)
			attempt.Result = provider.Result{}
		}
	}
	if attempt.Err != nil && (attempt.Classification.Category == "" || !attempt.Classification.Kind.Valid()) {
		attempt.Classification = provider.Classify(context.Background(), attempt.Err)
	}

	now := r.now().UTC()
	gateCtx, cancel := outcomeContext(ctx)
	gate, err := loadOutcomeGate(gateCtx, attempt, r.store, now)
	cancel()
	if err != nil {
		// A gate read failure is not an outcome-transaction failure. Leave the
		// due generation unsettled so a transient read error does not stall the
		// target until restart; the original running row remains diagnostic-only
		// and startup recovery will abandon it.
		r.logOutcomeError("outcome validity gate failed", attempt, "validity_gate_failed")
		return Result{Settled: false}, newOutcomeFailure("validity_gate_failed", err)
	}
	if !gate.valid {
		return r.finalizeDiscarded(ctx, attempt, gate.reason)
	}

	// Target invalidity takes precedence over cancellation. A deadline is
	// classified as a timeout by CheckStage and is never abandoned here, even
	// if shutdown cancellation is also observable.
	if ctx.Err() == context.Canceled && attempt.Classification.Kind != provider.ErrTimeout {
		return r.finalizeCancellation(ctx, attempt)
	}

	if attempt.Classification.IsAbandoned() {
		return r.finalizeCancellation(ctx, attempt)
	}
	if attempt.Classification.IsSuccess() {
		if attempt.Err != nil {
			r.logOutcomeError("outcome classification is inconsistent", attempt, "classification_inconsistent")
			return Result{Settled: true}, errors.New("scheduler: outcome classification is inconsistent")
		}
		return r.finalizeSuccess(ctx, attempt)
	}
	// A provider may return a result just as the overall deadline expires. The
	// classification, not the nil error, determines that this is a timeout.
	if attempt.Classification.Category == "" {
		attempt.Classification = provider.Classify(context.Background(),
			&provider.Error{Kind: provider.ErrInvalidResponse, Op: "Outcome"})
	}
	return r.finalizeError(ctx, attempt)
}

func (r *OutcomeRunner) finalizeSuccess(ctx context.Context, attempt CheckAttempt) (Result, error) {
	// Success deliberately uses the caller context. If shutdown begins before
	// this transaction commits, the running row is left for startup
	// abandonment rather than committing a success during cancellation. The
	// error branch may detach only to finalize an already-classified timeout.
	outcomeResult := attempt.Result
	if attempt.SelectionRequired {
		options := make([]provider.SelectedOption, 0, len(attempt.Selections))
		for _, item := range attempt.Selections {
			options = append(options, item.Option)
		}
		outcomeResult, _ = selection.Filter(attempt.Result, options)
	}
	outcome := store.CheckOutcomeSuccess
	healthOutcome := health.OutcomeSuccess
	if len(outcomeResult.Journeys) == 0 {
		outcome = store.CheckOutcomeEmpty
		healthOutcome = health.OutcomeEmpty
	}

	var discarded bool
	var discardReason string
	var events []string
	var hasOutbox bool
	var limitChange *RateLimitChange
	var limitSnapshot domain.ProviderRateLimit
	err := r.store.WithSQLTx(ctx, func(tx *sql.Tx, txStore *store.Store) error {
		txNow := r.now().UTC()
		gate, err := loadOutcomeGate(ctx, attempt, txStore, txNow)
		if err != nil {
			return err
		}
		if !gate.valid {
			discarded = true
			discardReason = gate.reason
			return r.finalizeCheckTx(ctx, tx, attempt, store.CheckOutcomeDiscarded, discardReason, txNow, outcomeCounts{}, nil)
		}

		counts := countOutcomeResult(outcomeResult)
		if err := r.finalizeCheckTx(ctx, tx, attempt, outcome, "", txNow, counts, nil); err != nil {
			return err
		}
		deliveries, err := r.loadDeliveries(ctx, tx)
		if err != nil {
			return err
		}
		hasOutbox = len(deliveries) > 0
		interval, err := txStore.PollInterval(ctx)
		if err != nil {
			return err
		}
		grace, err := txStore.UnavailableNotifyAfter(ctx)
		if err != nil {
			return err
		}
		availabilityResult, err := availability.Apply(ctx, tx, availability.Input{
			MonitorID:        attempt.MonitorID,
			TargetID:         attempt.TargetID,
			ProviderID:       attempt.ProviderID,
			ConfigVersion:    attempt.ConfigVersion,
			ObservedAt:       outcomeResult.ObservedAt,
			Now:              txNow,
			Result:           outcomeResult,
			Deliveries:       deliveries,
			UnavailableGrace: grace,
			UnavailableGap:   availability.GapTolerance(interval),
		})
		if err != nil {
			return fmt.Errorf("scheduler: apply availability: %w", err)
		}
		events = append(events, availabilityResult.EventsCreated...)

		healthResult, err := health.ApplySuccess(ctx, tx, health.SuccessInput{
			ProviderID: attempt.ProviderID,
			TargetID:   attempt.TargetID,
			Outcome:    healthOutcome,
			Now:        txNow,
			Duration:   attempt.Duration,
			Deliveries: deliveries,
		})
		if err != nil {
			return fmt.Errorf("scheduler: apply provider health success: %w", err)
		}
		events = append(events, healthResult.EventsCreated...)

		limit, found, err := txStore.GetProviderRateLimit(ctx, attempt.ProviderID, domain.ProviderRateLimitScopeProvider)
		if err != nil {
			return fmt.Errorf("scheduler: load provider rate limit: %w", err)
		}
		if found && (limit.Consecutive429s > 0 || limit.AdaptiveInterval > 0) {
			var change RateLimitChange
			limit, change = ApplyProviderRateLimitSuccess(txNow, interval, limit, r.adaptive)
			if err := txStore.UpsertProviderRateLimit(ctx, limit, txNow); err != nil {
				return fmt.Errorf("scheduler: persist provider rate-limit recovery: %w", err)
			}
			limitChange = &change
			limitSnapshot = limit
		}
		effectiveInterval := EffectivePollingInterval(interval, limit)
		var cooldownUntil time.Time
		if limit.CooldownUntil != nil {
			cooldownUntil = *limit.CooldownUntil
		}
		decision, err := DecideTargetScheduleWithProvider(
			txNow, interval, effectiveInterval, cooldownUntil,
			gate.target.ConsecutiveFailures, attempt.Classification, r.random,
		)
		if err != nil {
			return err
		}
		if !decision.Apply {
			return errors.New("scheduler: success outcome did not produce a target schedule")
		}
		return txStore.ApplyTargetSchedule(ctx, attempt.TargetID, decision.ConsecutiveFailures, decision.NextDueAt, txNow)
	})
	if err != nil {
		r.logOutcomeError("check outcome transaction failed", attempt, "outcome_transaction_failed")
		return Result{Settled: true}, newOutcomeFailure("outcome_transaction_failed", err)
	}
	if discarded {
		r.logOutcomeCommitted(attempt, string(store.CheckOutcomeDiscarded), 0)
		return Result{Settled: false}, nil
	}
	if limitChange != nil {
		r.logRateLimitChange(attempt, "recovered", *limitChange, limitSnapshot)
		r.emitRateLimitSuccessEvent(attempt, *limitChange, limitSnapshot)
	}
	if len(events) > 0 && hasOutbox {
		r.wake()
	}
	r.logOutcomeCommitted(attempt, string(outcome), len(events))
	return Result{Settled: true}, nil
}

func (r *OutcomeRunner) finalizeError(ctx context.Context, attempt CheckAttempt) (Result, error) {
	// Only an already-classified timeout may detach from a canceled caller
	// context. Non-timeout provider errors during shutdown must roll back and
	// leave the running row for startup abandonment rather than committing an
	// error outcome after cancellation.
	txCtx := ctx
	if attempt.Classification.Kind == provider.ErrTimeout {
		var cancel context.CancelFunc
		txCtx, cancel = outcomeContext(ctx)
		defer cancel()
	}

	var discarded bool
	var discardReason string
	var events []string
	var hasOutbox bool
	var limitChange *RateLimitChange
	var limitSnapshot domain.ProviderRateLimit
	err := r.store.WithSQLTx(txCtx, func(tx *sql.Tx, txStore *store.Store) error {
		txNow := r.now().UTC()
		gate, err := loadOutcomeGate(txCtx, attempt, txStore, txNow)
		if err != nil {
			return err
		}
		if !gate.valid {
			discarded = true
			discardReason = gate.reason
			return r.finalizeCheckTx(txCtx, tx, attempt, store.CheckOutcomeDiscarded, discardReason, txNow, outcomeCounts{}, nil)
		}

		var status *int
		if attempt.Classification.StatusCode > 0 {
			value := attempt.Classification.StatusCode
			status = &value
		}
		if err := r.finalizeCheckTx(txCtx, tx, attempt, store.CheckOutcomeError, string(attempt.Classification.Kind), txNow, outcomeCounts{}, status); err != nil {
			return err
		}
		deliveries, err := r.loadDeliveries(txCtx, tx)
		if err != nil {
			return err
		}
		hasOutbox = len(deliveries) > 0
		healthResult, err := health.ApplyFailure(txCtx, tx, health.FailureInput{
			ProviderID:     attempt.ProviderID,
			TargetID:       attempt.TargetID,
			Classification: attempt.Classification,
			Now:            txNow,
			Duration:       attempt.Duration,
			Deliveries:     deliveries,
		})
		if err != nil {
			return fmt.Errorf("scheduler: apply provider health failure: %w", err)
		}
		events = append(events, healthResult.EventsCreated...)

		interval, err := txStore.PollInterval(txCtx)
		if err != nil {
			return err
		}
		limit, found, err := txStore.GetProviderRateLimit(txCtx, attempt.ProviderID, domain.ProviderRateLimitScopeProvider)
		if err != nil {
			return fmt.Errorf("scheduler: load provider rate limit: %w", err)
		}
		if attempt.Classification.Kind == provider.ErrRateLimited {
			var change RateLimitChange
			limit, change = ApplyProviderRateLimit429(
				txNow, interval, attempt.Classification.RetryAfter, limit, r.random, r.adaptive,
			)
			if err := txStore.UpsertProviderRateLimit(txCtx, limit, txNow); err != nil {
				return fmt.Errorf("scheduler: persist provider rate-limit escalation: %w", err)
			}
			limitChange = &change
			limitSnapshot = limit
		} else if !found {
			limit.ProviderID = attempt.ProviderID
			limit.Scope = domain.ProviderRateLimitScopeProvider
		}
		effectiveInterval := EffectivePollingInterval(interval, limit)
		var cooldownUntil time.Time
		if limit.CooldownUntil != nil {
			cooldownUntil = *limit.CooldownUntil
		}
		decision, err := DecideTargetScheduleWithProvider(
			txNow, interval, effectiveInterval, cooldownUntil,
			gate.target.ConsecutiveFailures, attempt.Classification, r.random,
		)
		if err != nil {
			return err
		}
		if !decision.Apply {
			return errors.New("scheduler: error outcome did not produce a target schedule")
		}
		return txStore.ApplyTargetSchedule(txCtx, attempt.TargetID, decision.ConsecutiveFailures, decision.NextDueAt, txNow)
	})
	if err != nil {
		r.logOutcomeError("check outcome transaction failed", attempt, "outcome_transaction_failed")
		return Result{Settled: true}, newOutcomeFailure("outcome_transaction_failed", err)
	}
	if discarded {
		r.logOutcomeCommitted(attempt, string(store.CheckOutcomeDiscarded), 0)
		return Result{Settled: false}, nil
	}
	if limitChange != nil {
		r.logRateLimitChange(attempt, "escalated", *limitChange, limitSnapshot)
		r.emitRateLimit429Event(attempt, *limitChange, limitSnapshot)
	}
	if len(events) > 0 && hasOutbox {
		r.wake()
	}
	r.logOutcomeCommitted(attempt, string(store.CheckOutcomeError), len(events))
	return Result{Settled: true}, nil
}

func (r *OutcomeRunner) finalizeCancellation(ctx context.Context, attempt CheckAttempt) (Result, error) {
	txCtx, cancel := outcomeContext(ctx)
	defer cancel()

	var discarded bool
	var discardReason string
	err := r.store.WithSQLTx(txCtx, func(tx *sql.Tx, txStore *store.Store) error {
		txNow := r.now().UTC()
		gate, err := loadOutcomeGate(txCtx, attempt, txStore, txNow)
		if err != nil {
			return err
		}
		if !gate.valid {
			discarded = true
			discardReason = gate.reason
			return r.finalizeCheckTx(txCtx, tx, attempt, store.CheckOutcomeDiscarded, discardReason, txNow, outcomeCounts{}, nil)
		}
		return r.finalizeCheckTx(txCtx, tx, attempt, store.CheckOutcomeAbandoned, string(provider.ErrCanceled), txNow, outcomeCounts{}, nil)
	})
	if err != nil {
		if errors.Is(err, store.ErrNoRunningCheck) {
			r.logOutcomeError("check outcome skipped: no running row", attempt, "no_running_check")
			return Result{Settled: !discarded}, nil
		}
		r.logOutcomeError("check cancellation finalization failed", attempt, "finalization_failed")
		if discarded {
			return Result{Settled: true}, newOutcomeFailure("finalization_failed", err)
		}
		return Result{Settled: true}, nil
	}
	if discarded {
		r.logOutcomeCommitted(attempt, string(store.CheckOutcomeDiscarded), 0)
		return Result{Settled: false}, nil
	}
	r.logOutcomeCommitted(attempt, string(store.CheckOutcomeAbandoned), 0)
	return Result{Settled: true}, nil
}

func (r *OutcomeRunner) finalizeDiscarded(ctx context.Context, attempt CheckAttempt, reason string) (Result, error) {
	finalizeCtx, cancel := outcomeContext(ctx)
	defer cancel()

	var limitChange *RateLimitChange
	var limitSnapshot domain.ProviderRateLimit
	err := r.store.WithSQLTx(finalizeCtx, func(tx *sql.Tx, txStore *store.Store) error {
		txNow := r.now().UTC()
		// Re-evaluate the gate at the start of the outcome transaction even
		// though a pre-gate invalidity decision is final: the check row is the
		// only target/health/availability write, and a changed/expired target
		// must not become applicable between the read and the conditional
		// finalization.
		if _, err := loadOutcomeGate(finalizeCtx, attempt, txStore, txNow); err != nil {
			return err
		}
		// A real 429 is a provider-level signal even when this attempt is
		// discarded. Preserve the rate-limit penalty without touching target
		// scheduling, health, availability, or outbox state.
		if attempt.Classification.Kind == provider.ErrRateLimited {
			interval, err := txStore.PollInterval(finalizeCtx)
			if err != nil {
				return err
			}
			limit, _, err := txStore.GetProviderRateLimit(finalizeCtx, attempt.ProviderID, domain.ProviderRateLimitScopeProvider)
			if err != nil {
				return err
			}
			var change RateLimitChange
			limit, change = ApplyProviderRateLimit429(
				txNow, interval, attempt.Classification.RetryAfter, limit, r.random, r.adaptive,
			)
			if err := txStore.UpsertProviderRateLimit(finalizeCtx, limit, txNow); err != nil {
				return err
			}
			limitChange = &change
			limitSnapshot = limit
		}
		return r.finalizeCheckTx(finalizeCtx, tx, attempt, store.CheckOutcomeDiscarded, reason, txNow, outcomeCounts{}, nil)
	})
	if err != nil {
		if errors.Is(err, store.ErrNoRunningCheck) {
			r.logOutcomeError("check outcome skipped: no running row", attempt, "no_running_check")
			return Result{Settled: false}, nil
		}
		r.logOutcomeError("check finalization failed", attempt, "finalization_failed")
		return Result{Settled: true}, newOutcomeFailure("finalization_failed", err)
	}
	if limitChange != nil {
		r.logRateLimitChange(attempt, "escalated", *limitChange, limitSnapshot)
		r.emitRateLimit429Event(attempt, *limitChange, limitSnapshot)
	}
	r.logOutcomeCommitted(attempt, string(store.CheckOutcomeDiscarded), 0)
	return Result{Settled: false}, nil
}

func (r *OutcomeRunner) finalizeCheckTx(
	ctx context.Context,
	tx *sql.Tx,
	attempt CheckAttempt,
	outcome store.CheckOutcome,
	reason string,
	now time.Time,
	counts outcomeCounts,
	httpStatus *int,
) error {
	duration := attempt.Duration
	if duration < 0 {
		duration = 0
	}
	return r.store.FinalizeCheckRun(ctx, tx, store.CheckFinalization{
		CheckRunID:         attempt.CheckRunID,
		Outcome:            outcome,
		FinishedAt:         finishedAt(attempt, now),
		ErrorKind:          reason,
		HTTPStatus:         httpStatus,
		Duration:           duration,
		JourneyCount:       counts.journeys,
		OfferingCount:      counts.offerings,
		ReservableCount:    counts.reservable,
		NotReservableCount: counts.notReservable,
		UnknownCount:       counts.unknown,
		WarningCount:       counts.warnings,
		ProviderRequest:    attempt.ProviderRequest,
		ProviderStartedAt:  attempt.ProviderStartedAt,
		ProviderDuration:   attempt.ProviderDuration,
		ProviderErrorKind:  string(attempt.ProviderErrorKind),
		QueueWait:          attempt.QueueWait,
		ExpectedSpacing:    attempt.ExpectedSpacing,
		ConfiguredInterval: attempt.ConfiguredInterval,
		AdaptiveInterval:   attempt.AdaptiveInterval,
	})
}

func (r *OutcomeRunner) loadDeliveries(ctx context.Context, tx *sql.Tx) ([]notify.Delivery, error) {
	if r.deliveries == nil {
		return nil, nil
	}
	deliveries, err := r.deliveries(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("scheduler: load delivery destinations: %w", err)
	}
	return deliveries, nil
}

func (r *OutcomeRunner) logOutcomeCommitted(attempt CheckAttempt, outcome string, events int) {
	r.logger.Info("check outcome committed",
		"monitor_id", attempt.MonitorID,
		"target_id", attempt.TargetID,
		"provider_id", string(attempt.ProviderID),
		"outcome", outcome,
		"events", events,
		"duration_ms", attempt.Duration.Milliseconds(),
	)
}

func (r *OutcomeRunner) emitRateLimit429Event(attempt CheckAttempt, change RateLimitChange, state domain.ProviderRateLimit) {
	// Every observed logical 429 is recorded. The previous/current levels make
	// escalation versus maintenance explicit without labeling every 429 as an
	// escalation.
	r.emitRateLimitEvent(attempt, store.RateLimitEventRateLimited, change, state)
	if change.CooldownExtended {
		r.emitRateLimitEvent(attempt, store.RateLimitEventCooldownExtended, change, state)
	}
}

func (r *OutcomeRunner) emitRateLimitSuccessEvent(attempt CheckAttempt, change RateLimitChange, state domain.ProviderRateLimit) {
	switch {
	case change.FullyRecovered:
		r.emitRateLimitEvent(attempt, store.RateLimitEventRecovered, change, state)
	case change.Recovered:
		r.emitRateLimitEvent(attempt, store.RateLimitEventRecoveryStep, change, state)
	case change.RecoveryProgress:
		r.emitRateLimitEvent(attempt, store.RateLimitEventRecoveryProgress, change, state)
	}
}

func (r *OutcomeRunner) emitRateLimitEvent(attempt CheckAttempt, kind string, change RateLimitChange, state domain.ProviderRateLimit) {
	if r.rateLimitEvents == nil {
		return
	}
	scope := state.Scope
	if scope == "" {
		scope = domain.ProviderRateLimitScopeProvider
	}
	occurredAt := r.now().UTC()
	event := store.ProviderRateLimitEvent{
		ProviderID:            attempt.ProviderID,
		Scope:                 scope,
		OccurredAt:            occurredAt,
		Kind:                  kind,
		PreviousLevel:         change.PreviousLevel,
		EscalationLevel:       change.Level,
		PreviousAdaptive:      change.PreviousInterval,
		Adaptive:              change.NewInterval,
		PreviousCooldownUntil: change.PreviousCooldownUntil,
		CooldownUntil:         state.CooldownUntil,
		RetryAfter:            change.RetryAfter,
		CreatedAt:             occurredAt,
	}
	if attempt.CheckRunID > 0 {
		checkRunID := attempt.CheckRunID
		event.CheckRunID = &checkRunID
	}
	func() {
		defer func() { _ = recover() }()
		r.rateLimitEvents.RecordRateLimitEvent(event)
	}()
}

func (r *OutcomeRunner) logRateLimitChange(attempt CheckAttempt, action string, change RateLimitChange, state domain.ProviderRateLimit) {
	cooldown := ""
	if state.CooldownUntil != nil {
		cooldown = state.CooldownUntil.UTC().Format(time.RFC3339)
	}
	action = rateLimitAction(change)
	message := "provider rate limit " + action
	r.logger.Info(message,
		"provider_id", string(attempt.ProviderID),
		"scope", state.Scope,
		"escalation_level", change.Level,
		"previous_adaptive_interval", change.PreviousInterval.String(),
		"adaptive_interval", state.AdaptiveInterval.String(),
		"retry_after", change.RetryAfter.String(),
		"cooldown_until", cooldown,
	)
}

func rateLimitAction(change RateLimitChange) string {
	switch {
	case change.FullyRecovered:
		return "recovered"
	case change.Recovered:
		return "recovery_step"
	case change.RecoveryProgress:
		return "recovery_progress"
	case change.Escalated:
		return "escalated"
	default:
		return "maintained"
	}
}

func (r *OutcomeRunner) logOutcomeError(message string, attempt CheckAttempt, code string) {
	r.logger.Error(message,
		"monitor_id", attempt.MonitorID,
		"target_id", attempt.TargetID,
		"provider_id", string(attempt.ProviderID),
		"code", code,
	)
}

type outcomeGate struct {
	valid  bool
	reason string
	target domain.MonitorProvider
}

type monitorLoader interface {
	GetMonitor(ctx context.Context, id string) (domain.Monitor, error)
}

func loadOutcomeGate(ctx context.Context, attempt CheckAttempt, loader monitorLoader, now time.Time) (outcomeGate, error) {
	monitor, err := loader.GetMonitor(ctx, attempt.MonitorID)
	if err != nil {
		if errors.Is(err, domain.ErrMonitorNotFound) {
			return outcomeGate{reason: "target_missing"}, nil
		}
		return outcomeGate{}, err
	}
	var target domain.MonitorProvider
	found := false
	for _, candidate := range monitor.Providers {
		if candidate.ID == attempt.TargetID {
			target = candidate
			found = true
			break
		}
	}
	if !found || target.MonitorID != attempt.MonitorID {
		return outcomeGate{reason: "target_missing"}, nil
	}
	switch {
	case !target.Active:
		return outcomeGate{reason: "target_inactive"}, nil
	case !monitor.Enabled:
		return outcomeGate{reason: "monitor_disabled"}, nil
	case monitor.ExpiredAt != nil:
		return outcomeGate{reason: "monitor_expired"}, nil
	case !monitor.ExpiresAt.After(now):
		return outcomeGate{reason: "monitor_expired"}, nil
	case target.ConfigVersion != attempt.ConfigVersion:
		return outcomeGate{reason: "config_changed"}, nil
	case target.BookingSlug != attempt.BookingSlug:
		return outcomeGate{reason: "booking_config_changed"}, nil
	default:
		return outcomeGate{valid: true, target: target}, nil
	}
}

type outcomeCounts struct {
	journeys      int
	offerings     int
	reservable    int
	notReservable int
	unknown       int
	warnings      int
}

func countOutcomeResult(result provider.Result) outcomeCounts {
	counts := outcomeCounts{
		journeys: len(result.Journeys),
		warnings: len(result.Warnings),
	}
	for _, journey := range result.Journeys {
		counts.offerings += len(journey.Offerings)
		for _, offering := range journey.Offerings {
			switch offering.State {
			case provider.StateReservable:
				counts.reservable++
			case provider.StateNotReservable:
				counts.notReservable++
			case provider.StateUnknown:
				counts.unknown++
			}
		}
	}
	return counts
}

func finishedAt(attempt CheckAttempt, fallback time.Time) time.Time {
	if !attempt.FinishedAt.IsZero() {
		return attempt.FinishedAt.UTC()
	}
	return fallback.UTC()
}

func outcomeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx != nil && ctx.Err() != nil {
		return context.WithTimeout(context.WithoutCancel(ctx), outcomeFinalizeTimeout)
	}
	return ctx, func() {}
}

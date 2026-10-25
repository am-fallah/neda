package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
)

// DefaultCheckTimeout is the single overall provider-check deadline owned by
// SCH-004. Provider adapters and any future HTTP retry policy must honor this
// caller-supplied context; SCH-004 does not implement HTTP retries or
// per-attempt timeouts.
const DefaultCheckTimeout = 20 * time.Second

// ErrNilCheckRepository reports a missing check-stage repository.
var ErrNilCheckRepository = errors.New("scheduler: nil check repository")

// CheckRepository is the persistence boundary used by CheckStage. It is
// intentionally narrower than Repository: the check stage loads one monitor
// and starts one attempt row.
type CheckRepository interface {
	GetMonitor(ctx context.Context, id string) (domain.Monitor, error)
	StartCheckRun(ctx context.Context, targetID, configVersion int64, startedAt time.Time) (int64, error)
}

// CheckStageConfig controls the SCH-004 provider-check stage. Timeout exists so
// deterministic tests can use a shorter deadline; production uses
// DefaultCheckTimeout when it is zero.
type CheckStageConfig struct {
	Repository CheckRepository
	Providers  ProviderLookup
	Now        func() time.Time
	Timeout    time.Duration
	Logger     *slog.Logger
}

// CheckStage executes the provider-I/O half of one check attempt. It is not a
// Runner itself: SCH-010's OutcomeRunner wraps it and owns finalization and the
// post-I/O validity gate.
type CheckStage struct {
	repo      CheckRepository
	providers ProviderLookup
	now       func() time.Time
	timeout   time.Duration
	logger    *slog.Logger
}

// CheckAttempt is the handoff from SCH-004 to SCH-010. CheckRunID is non-zero
// only when the attempt-start transaction committed. Result and Err are
// mutually exclusive in practice; Classification always describes the
// PRV-010 outcome for the caller.
type CheckAttempt struct {
	CheckRunID        int64
	TargetID          int64
	MonitorID         string
	ProviderID        provider.ID
	ConfigVersion     int64
	BookingSlug       string
	StartedAt         time.Time
	FinishedAt        time.Time
	Duration          time.Duration
	Request           provider.CheckRequest
	Result            provider.Result
	Selections        []domain.MonitorProviderSelection
	SelectionRequired bool
	Err               error
	Classification    provider.Classification
}

// NewCheckStage validates dependencies and applies safe defaults.
func NewCheckStage(cfg CheckStageConfig) (*CheckStage, error) {
	if cfg.Repository == nil {
		return nil, ErrNilCheckRepository
	}
	if cfg.Providers == nil {
		return nil, ErrNilProviders
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultCheckTimeout
	}
	if cfg.Timeout < 0 {
		return nil, errors.New("scheduler: check timeout must not be negative")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &CheckStage{
		repo:      cfg.Repository,
		providers: cfg.Providers,
		now:       cfg.Now,
		timeout:   cfg.Timeout,
		logger:    cfg.Logger,
	}, nil
}

// RunCheck loads the persisted target, commits exactly one running check_runs
// row, and then performs provider I/O outside every database transaction. It
// returns a non-nil error only when the attempt could not be started or its
// configuration could not be loaded. Provider results, typed errors, and
// caller cancellation are returned on CheckAttempt with a nil error so SCH-010
// can select the final outcome.
func (s *CheckStage) RunCheck(ctx context.Context, target Target) (CheckAttempt, error) {
	if ctx == nil {
		return CheckAttempt{}, errors.New("scheduler: nil check context")
	}
	if ctx.Err() != nil {
		return unstartedAttempt(target, ctx, ctx.Err()), nil
	}

	adapter, ok := s.providers.Lookup(target.ProviderID)
	if !ok || adapter == nil {
		return s.fail(target, fmt.Errorf("scheduler: provider %q is not registered", target.ProviderID))
	}

	monitor, err := s.repo.GetMonitor(ctx, target.MonitorID)
	if err != nil {
		return s.fail(target, fmt.Errorf("scheduler: load check target: %w", err))
	}
	persisted, ok := findCheckTarget(monitor, target.ID)
	if !ok {
		return s.fail(target, fmt.Errorf("scheduler: target %d not found in monitor %q", target.ID, target.MonitorID))
	}
	if persisted.MonitorID != target.MonitorID || persisted.ProviderID != target.ProviderID {
		return s.fail(target, fmt.Errorf("scheduler: target %d identity does not match the dispatched target", target.ID))
	}

	// Build the request from the freshly loaded row and retain its version.
	// The scheduler's dispatch snapshot may be stale; SCH-010 compares the
	// retained version and booking slug with persisted state after I/O.
	request := checkRequest(monitor, persisted)
	startedAt := s.now().UTC()
	checkRunID, err := s.repo.StartCheckRun(ctx, persisted.ID, persisted.ConfigVersion, startedAt)
	if err != nil {
		return s.fail(target, fmt.Errorf("scheduler: start check run: %w", err))
	}

	metadata := adapter.Metadata()
	attempt := CheckAttempt{
		CheckRunID:        checkRunID,
		TargetID:          persisted.ID,
		MonitorID:         persisted.MonitorID,
		ProviderID:        persisted.ProviderID,
		ConfigVersion:     persisted.ConfigVersion,
		BookingSlug:       persisted.BookingSlug,
		StartedAt:         startedAt,
		Request:           request,
		Selections:        append([]domain.MonitorProviderSelection(nil), persisted.Selections...),
		SelectionRequired: metadata.Capabilities.RequiresOptionSelection,
	}

	if attempt.SelectionRequired && len(attempt.Selections) == 0 {
		attempt.Err = configurationFailure(errors.New("selected train/class options are missing"))
		attempt.Classification = provider.Classify(ctx, attempt.Err)
		attempt.FinishedAt, attempt.Duration = s.finish(startedAt)
		return attempt, nil
	}

	if err := ctx.Err(); err != nil {
		attempt.Err = err
		attempt.Classification = provider.Classify(ctx, err)
		attempt.FinishedAt, attempt.Duration = s.finish(startedAt)
		return attempt, nil
	}

	// Configuration validation is part of the started attempt. A failure is
	// retained for SCH-010 to finalize as a configuration error; it does not
	// open a second attempt and does not call the provider.
	if err := adapter.ValidateConfig(request.Config); err != nil {
		attempt.Err = configurationFailure(err)
		attempt.Classification = provider.Classify(ctx, attempt.Err)
		attempt.FinishedAt, attempt.Duration = s.finish(startedAt)
		return attempt, nil
	}

	if err := ctx.Err(); err != nil {
		attempt.Err = err
		attempt.Classification = provider.Classify(ctx, err)
		attempt.FinishedAt, attempt.Duration = s.finish(startedAt)
		return attempt, nil
	}

	// SCH-004 is the sole owner of the overall deadline. Classify with this
	// context before canceling it so deadline expiry maps to a timeout failure
	// and caller cancellation maps to abandoned.
	checkCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	attempt.Result, attempt.Err = adapter.Check(checkCtx, request)
	attempt.Classification = provider.Classify(checkCtx, attempt.Err)
	attempt.FinishedAt, attempt.Duration = s.finish(startedAt)
	return attempt, nil
}

func (s *CheckStage) fail(target Target, err error) (CheckAttempt, error) {
	s.logger.Error("check stage failed",
		"monitor_id", target.MonitorID,
		"target_id", target.ID,
		"provider_id", string(target.ProviderID),
		"error", err,
	)
	return CheckAttempt{}, err
}

func (s *CheckStage) finish(startedAt time.Time) (time.Time, time.Duration) {
	finishedAt := s.now().UTC()
	duration := finishedAt.Sub(startedAt)
	if duration < 0 {
		duration = 0
	}
	return finishedAt, duration
}

// configurationFailure normalizes any ValidateConfig failure to a
// configuration classification. A typed provider.Error already marked as
// configuration keeps its explicit metadata; other errors are wrapped so an
// adapter bug cannot turn a validation failure into an infrastructure outage.
// PRV-010 still gives caller cancellation and deadline expiry precedence.
func configurationFailure(err error) error {
	var providerErr *provider.Error
	if errors.As(err, &providerErr) && providerErr.Kind == provider.ErrConfiguration {
		return err
	}
	return &provider.Error{Kind: provider.ErrConfiguration, Op: "ValidateConfig", Err: err}
}

// unstartedAttempt reports a caller cancellation or deadline that occurred
// before the attempt-start row was committed. CheckRunID remains zero; there is
// no row for SCH-010 to finalize.
func unstartedAttempt(target Target, ctx context.Context, err error) CheckAttempt {
	return CheckAttempt{
		TargetID:       target.ID,
		MonitorID:      target.MonitorID,
		ProviderID:     target.ProviderID,
		ConfigVersion:  target.ConfigVersion,
		Err:            err,
		Classification: provider.Classify(ctx, err),
	}
}

// findCheckTarget returns the persisted target row selected by the scheduler.
// Inactive rows are intentionally included: eligibility is re-evaluated by
// SCH-010's post-I/O gate, not by the check stage.
func findCheckTarget(monitor domain.Monitor, targetID int64) (domain.MonitorProvider, bool) {
	for _, target := range monitor.Providers {
		if target.ID == targetID {
			return target, true
		}
	}
	return domain.MonitorProvider{}, false
}

func checkRequest(monitor domain.Monitor, target domain.MonitorProvider) provider.CheckRequest {
	return provider.CheckRequest{
		MonitorID:     monitor.ID,
		DepartureDate: monitor.DepartureDate,
		Route: provider.Route{
			OriginName:      monitor.OriginName,
			DestinationName: monitor.DestinationName,
			OriginID:        target.OriginID,
			DestinationID:   target.DestinationID,
		},
		Passengers: provider.Passengers{
			Adults:   monitor.Adults,
			Children: monitor.Children,
			Infants:  monitor.Infants,
		},
		Config: provider.MonitorProviderConfig{
			OriginID:      target.OriginID,
			DestinationID: target.DestinationID,
			BookingSlug:   target.BookingSlug,
			GenderCode:    target.GenderCode,
		},
	}
}

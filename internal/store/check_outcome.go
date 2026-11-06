package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CheckOutcome is one final check_runs outcome. Running is deliberately not a
// valid final outcome.
type CheckOutcome string

const (
	CheckOutcomeSuccess   CheckOutcome = "success"
	CheckOutcomeEmpty     CheckOutcome = "empty"
	CheckOutcomeError     CheckOutcome = "error"
	CheckOutcomeDiscarded CheckOutcome = "discarded"
	CheckOutcomeAbandoned CheckOutcome = "abandoned"
)

// ErrNoRunningCheck reports that a conditional check-run finalization found no
// row with outcome='running'. The caller must roll back the outcome
// transaction and must not apply a second final outcome.
var ErrNoRunningCheck = errors.New("store: no running check run")

// CheckFinalization is the complete final state of one check_runs attempt.
// Counts are zero for non-success outcomes. Duration may be zero when the
// attempt never completed provider I/O.
type CheckFinalization struct {
	CheckRunID         int64
	Outcome            CheckOutcome
	FinishedAt         time.Time
	ErrorKind          string
	HTTPStatus         *int
	Duration           time.Duration
	JourneyCount       int
	OfferingCount      int
	ReservableCount    int
	NotReservableCount int
	UnknownCount       int
	WarningCount       int

	// Logical provider request diagnostics. ProviderRequest is true only when
	// adapter.Check was actually invoked. ProviderStartedAt and
	// ProviderDuration describe that call, excluding database and validation
	// work. ProviderErrorKind preserves the final classification even when the
	// target outcome is discarded (for example a discarded 429).
	ProviderRequest    bool
	ProviderStartedAt  time.Time
	ProviderDuration   time.Duration
	ProviderErrorKind  string
	QueueWait          time.Duration
	ExpectedSpacing    time.Duration
	ConfiguredInterval time.Duration
	AdaptiveInterval   time.Duration
}

const finalizeCheckRunQuery = `UPDATE check_runs SET
	outcome = ?, finished_at = ?, error_kind = ?, http_status = ?, duration_ms = ?,
	journey_count = ?, offering_count = ?, reservable_count = ?,
	not_reservable_count = ?, unknown_count = ?, warning_count = ?,
	provider_request = ?, provider_started_at = ?, provider_duration_ms = ?,
	provider_error_kind = ?, queue_wait_ms = ?, expected_spacing_ms = ?,
	configured_interval_ms = ?, adaptive_interval_ms = ?
WHERE id = ? AND outcome = 'running'`

// FinalizeCheckRun conditionally transitions one running check_runs row to a
// final outcome inside the caller's transaction. It never begins, commits, or
// rolls back. A zero-row update returns ErrNoRunningCheck so the caller can
// roll back and avoid a duplicate final outcome.
func (s *Store) FinalizeCheckRun(ctx context.Context, tx *sql.Tx, input CheckFinalization) error {
	if tx == nil {
		return errors.New("store: nil check finalization transaction")
	}
	if err := input.validate(); err != nil {
		return err
	}
	if input.FinishedAt.IsZero() {
		return errors.New("store: check finish time must be set")
	}
	if input.Duration < 0 {
		return errors.New("store: check duration must not be negative")
	}
	if input.JourneyCount < 0 || input.OfferingCount < 0 || input.ReservableCount < 0 ||
		input.NotReservableCount < 0 || input.UnknownCount < 0 || input.WarningCount < 0 {
		return errors.New("store: check counts must not be negative")
	}

	var httpStatus any
	if input.HTTPStatus != nil {
		if *input.HTTPStatus <= 0 {
			return errors.New("store: check http status must be positive when set")
		}
		httpStatus = *input.HTTPStatus
	}
	var providerStartedAt any
	if input.ProviderRequest {
		if input.ProviderStartedAt.IsZero() {
			return errors.New("store: provider request start time must be set")
		}
		providerStartedAt = formatTime(input.ProviderStartedAt)
	}
	if input.ProviderDuration < 0 || input.QueueWait < 0 || input.ExpectedSpacing < 0 ||
		input.ConfiguredInterval < 0 || input.AdaptiveInterval < 0 {
		return errors.New("store: provider timing fields must not be negative")
	}
	result, err := tx.ExecContext(ctx, finalizeCheckRunQuery,
		string(input.Outcome),
		formatTime(input.FinishedAt),
		nullableString(input.ErrorKind),
		httpStatus,
		input.Duration.Milliseconds(),
		input.JourneyCount,
		input.OfferingCount,
		input.ReservableCount,
		input.NotReservableCount,
		input.UnknownCount,
		input.WarningCount,
		boolToInt(input.ProviderRequest),
		providerStartedAt,
		nullableDurationMillis(input.ProviderDuration, input.ProviderRequest),
		nullableString(input.ProviderErrorKind),
		nullableDurationMillis(input.QueueWait, input.ProviderRequest),
		nullableDurationMillis(input.ExpectedSpacing, input.ProviderRequest),
		nullableDurationMillis(input.ConfiguredInterval, input.ProviderRequest),
		nullableDurationMillis(input.AdaptiveInterval, input.ProviderRequest),
		input.CheckRunID,
	)
	if err != nil {
		return fmt.Errorf("store: finalize check run: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: finalize check run rows: %w", err)
	}
	if updated != 1 {
		return ErrNoRunningCheck
	}
	return nil
}

func nullableDurationMillis(value time.Duration, present bool) any {
	if !present {
		return nil
	}
	return value.Milliseconds()
}

func (in CheckFinalization) validate() error {
	if in.CheckRunID <= 0 {
		return errors.New("store: check run id must be positive")
	}
	switch in.Outcome {
	case CheckOutcomeSuccess, CheckOutcomeEmpty, CheckOutcomeError,
		CheckOutcomeDiscarded, CheckOutcomeAbandoned:
	default:
		return fmt.Errorf("store: invalid check outcome %q", in.Outcome)
	}
	return nil
}

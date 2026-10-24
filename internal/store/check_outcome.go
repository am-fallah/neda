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
}

const finalizeCheckRunQuery = `UPDATE check_runs SET
	outcome = ?, finished_at = ?, error_kind = ?, http_status = ?, duration_ms = ?,
	journey_count = ?, offering_count = ?, reservable_count = ?,
	not_reservable_count = ?, unknown_count = ?, warning_count = ?
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

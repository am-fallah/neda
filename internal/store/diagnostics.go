package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
)

// API-008 read model: current inventory observations and recent check
// diagnostics for one monitor provider target. These queries are read-only and
// never mutate scheduling, availability, episodes, events, health, or outbox
// state.

const inventoryObservationSelect = `SELECT journey_key, offering_key, state,
	state_since, last_observed_at, last_decisive_at, last_missing_at, last_reason,
	last_capacity, train_number, operator, origin_name, destination_name,
	departure_at, arrival_at, wagon_name, class_name, price_amount,
	price_currency, last_booking_url, updated_at
FROM inventory_states
WHERE monitor_provider_id = ? AND config_version = ?
ORDER BY CASE state
	WHEN 'reservable' THEN 0
	WHEN 'not_reservable' THEN 1
	ELSE 2
END, journey_key, offering_key, id
LIMIT ?`

const recentCheckRunSelect = `SELECT id, config_version, started_at, finished_at,
	outcome, error_kind, http_status, duration_ms, journey_count, offering_count,
	reservable_count, not_reservable_count, unknown_count, warning_count, created_at
FROM check_runs
WHERE monitor_provider_id = ?
ORDER BY julianday(started_at) DESC, id DESC
LIMIT ?`

// ListInventoryObservations returns the last persisted availability snapshot
// for each offering at the target's current config version. It applies a
// bounded limit and reports whether more rows exist. An observation whose latest
// successful check did not return the offering keeps its last confirmed state
// but is marked Missing; callers must present that as unknown current
// availability, not as a confirmed NotReservable or Reservable state.
func (s *Store) ListInventoryObservations(ctx context.Context, targetID, configVersion int64, limit int) (domain.InventoryObservationPage, error) {
	if targetID <= 0 {
		return domain.InventoryObservationPage{}, fmt.Errorf("store: inventory target id must be positive")
	}
	if configVersion < 1 {
		return domain.InventoryObservationPage{}, fmt.Errorf("store: inventory config version must be positive")
	}
	if err := validateDiagnosticLimit(limit, domain.MaxObservationLimit); err != nil {
		return domain.InventoryObservationPage{}, err
	}
	db, err := s.executor()
	if err != nil {
		return domain.InventoryObservationPage{}, err
	}

	rows, err := db.QueryContext(ctx, inventoryObservationSelect, targetID, configVersion, limit+1)
	if err != nil {
		return domain.InventoryObservationPage{}, wrap("list inventory observations", err)
	}
	defer rows.Close()

	observations := make([]domain.InventoryObservation, 0, limit)
	for rows.Next() {
		observation, err := scanInventoryObservation(rows)
		if err != nil {
			return domain.InventoryObservationPage{}, err
		}
		observations = append(observations, observation)
	}
	if err := rows.Err(); err != nil {
		return domain.InventoryObservationPage{}, wrap("iterate inventory observations", err)
	}
	if err := rows.Close(); err != nil {
		return domain.InventoryObservationPage{}, wrap("close inventory observations", err)
	}

	page := domain.InventoryObservationPage{Observations: observations}
	if len(observations) > limit {
		page.Observations = observations[:limit]
		page.Truncated = true
	}
	if len(page.Observations) == 0 {
		page.Observations = []domain.InventoryObservation{}
	}
	return page, nil
}

// ListRecentCheckRuns returns the most recent check attempts for one target in
// descending start order. It applies a bounded limit and reports whether older
// rows exist. Only sanitized check diagnostics are returned.
func (s *Store) ListRecentCheckRuns(ctx context.Context, targetID int64, limit int) (domain.CheckRunDiagnosticPage, error) {
	if targetID <= 0 {
		return domain.CheckRunDiagnosticPage{}, fmt.Errorf("store: check target id must be positive")
	}
	if err := validateDiagnosticLimit(limit, domain.MaxRecentCheckLimit); err != nil {
		return domain.CheckRunDiagnosticPage{}, err
	}
	db, err := s.executor()
	if err != nil {
		return domain.CheckRunDiagnosticPage{}, err
	}

	rows, err := db.QueryContext(ctx, recentCheckRunSelect, targetID, limit+1)
	if err != nil {
		return domain.CheckRunDiagnosticPage{}, wrap("list recent check runs", err)
	}
	defer rows.Close()

	checks := make([]domain.CheckRunDiagnostic, 0, limit)
	for rows.Next() {
		check, err := scanCheckRunDiagnostic(rows)
		if err != nil {
			return domain.CheckRunDiagnosticPage{}, err
		}
		checks = append(checks, check)
	}
	if err := rows.Err(); err != nil {
		return domain.CheckRunDiagnosticPage{}, wrap("iterate recent check runs", err)
	}
	if err := rows.Close(); err != nil {
		return domain.CheckRunDiagnosticPage{}, wrap("close recent check runs", err)
	}

	page := domain.CheckRunDiagnosticPage{Checks: checks}
	if len(checks) > limit {
		page.Checks = checks[:limit]
		page.Truncated = true
	}
	if len(page.Checks) == 0 {
		page.Checks = []domain.CheckRunDiagnostic{}
	}
	return page, nil
}

func scanInventoryObservation(rows *sql.Rows) (domain.InventoryObservation, error) {
	var (
		observation                           domain.InventoryObservation
		state, stateSince, lastObservedAt     string
		lastDecisive, lastMissing, lastReason sql.NullString
		lastCapacity                          sql.NullInt64
		trainNumber, operator                 sql.NullString
		originName, destinationName           sql.NullString
		departureAt, arrivalAt                sql.NullString
		wagonName, className                  sql.NullString
		priceAmount                           sql.NullInt64
		priceCurrency, bookingURL             sql.NullString
		updatedAt                             string
	)
	if err := rows.Scan(
		&observation.JourneyKey, &observation.OfferingKey, &state,
		&stateSince, &lastObservedAt, &lastDecisive, &lastMissing, &lastReason,
		&lastCapacity, &trainNumber, &operator, &originName, &destinationName,
		&departureAt, &arrivalAt, &wagonName, &className, &priceAmount,
		&priceCurrency, &bookingURL, &updatedAt,
	); err != nil {
		return domain.InventoryObservation{}, wrap("scan inventory observation", err)
	}
	observation.State = provider.State(state)
	if !observation.State.Valid() {
		return domain.InventoryObservation{}, fmt.Errorf("store: inventory state %q is invalid", state)
	}
	var err error
	if observation.StateSince, err = parseTime(stateSince); err != nil {
		return domain.InventoryObservation{}, err
	}
	if observation.LastObservedAt, err = parseTime(lastObservedAt); err != nil {
		return domain.InventoryObservation{}, err
	}
	if observation.LastDecisiveAt, err = parseOptionalTime(lastDecisive); err != nil {
		return domain.InventoryObservation{}, err
	}
	if observation.LastMissingAt, err = parseOptionalTime(lastMissing); err != nil {
		return domain.InventoryObservation{}, err
	}
	if observation.LastMissingAt != nil {
		// A presence observation clears last_missing_at; a non-null marker that
		// is not older than the last observation means the latest successful
		// check did not return this offering.
		observation.Missing = !observation.LastMissingAt.Before(observation.LastObservedAt)
	}
	observation.LastReason = optionalString(lastReason)
	if lastCapacity.Valid {
		value := int(lastCapacity.Int64)
		observation.LastCapacity = &value
	}
	observation.TrainNumber = optionalString(trainNumber)
	observation.Operator = optionalString(operator)
	observation.OriginName = optionalString(originName)
	observation.DestinationName = optionalString(destinationName)
	if observation.DepartureAt, err = parseOptionalTime(departureAt); err != nil {
		return domain.InventoryObservation{}, err
	}
	if observation.ArrivalAt, err = parseOptionalTime(arrivalAt); err != nil {
		return domain.InventoryObservation{}, err
	}
	observation.WagonName = optionalString(wagonName)
	observation.ClassName = optionalString(className)
	if priceAmount.Valid {
		value := priceAmount.Int64
		observation.PriceAmount = &value
	}
	observation.PriceCurrency = optionalString(priceCurrency)
	observation.BookingURL = optionalString(bookingURL)
	if observation.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return domain.InventoryObservation{}, err
	}
	return observation, nil
}

func scanCheckRunDiagnostic(rows *sql.Rows) (domain.CheckRunDiagnostic, error) {
	var (
		check                  domain.CheckRunDiagnostic
		startedAt, outcome     string
		finishedAt, errorKind  sql.NullString
		httpStatus, durationMS sql.NullInt64
		createdAt              string
	)
	if err := rows.Scan(
		&check.ID, &check.ConfigVersion, &startedAt, &finishedAt, &outcome,
		&errorKind, &httpStatus, &durationMS, &check.JourneyCount,
		&check.OfferingCount, &check.ReservableCount, &check.NotReservableCount,
		&check.UnknownCount, &check.WarningCount, &createdAt,
	); err != nil {
		return domain.CheckRunDiagnostic{}, wrap("scan check run diagnostic", err)
	}
	check.Outcome = domain.CheckRunOutcome(outcome)
	if !check.Outcome.Valid() {
		return domain.CheckRunDiagnostic{}, fmt.Errorf("store: check outcome %q is invalid", outcome)
	}
	if (check.Outcome == domain.CheckRunRunning) == finishedAt.Valid {
		return domain.CheckRunDiagnostic{}, fmt.Errorf("store: check run %d has inconsistent outcome/finished_at", check.ID)
	}
	var err error
	if check.StartedAt, err = parseTime(startedAt); err != nil {
		return domain.CheckRunDiagnostic{}, err
	}
	if check.FinishedAt, err = parseOptionalTime(finishedAt); err != nil {
		return domain.CheckRunDiagnostic{}, err
	}
	check.ErrorKind = optionalString(errorKind)
	if httpStatus.Valid {
		value := int(httpStatus.Int64)
		check.HTTPStatus = &value
	}
	if durationMS.Valid {
		value := durationMS.Int64
		check.DurationMS = &value
	}
	if check.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.CheckRunDiagnostic{}, err
	}
	return check, nil
}

func validateDiagnosticLimit(limit, max int) error {
	if limit < 1 || limit > max {
		return fmt.Errorf("store: diagnostic limit must be between 1 and %d", max)
	}
	return nil
}

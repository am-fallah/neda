// Package availability owns the availability state machine, episodes, and
// event generation. It is transaction-scoped: callers pass an existing
// *sql.Tx and SCH-010 owns commit/rollback and check-run finalization.
package availability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/provider"
)

// Input is one successful provider result applied to a monitor target.
type Input struct {
	MonitorID     string
	TargetID      int64
	ProviderID    provider.ID
	ConfigVersion int64
	ObservedAt    time.Time
	Now           time.Time
	Result        provider.Result
	Deliveries    []notify.Delivery
}

// Result reports the state/episode/event changes made by Apply.
type Result struct {
	StatesUpserted int
	EpisodesOpened []int64
	EpisodesClosed []int64
	EventsCreated  []string
	MissingStates  int
	Warnings       []string
}

type stateKey struct {
	journeyKey  string
	offeringKey string
}

type observation struct {
	journey    provider.Journey
	offering   provider.Offering
	state      provider.State
	reason     string
	capacity   *int
	bookingURL string
}

type currentState struct {
	id             int64
	state          provider.State
	stateSince     time.Time
	lastDecisiveAt *time.Time
}

const selectStateQuery = `SELECT id, state, state_since, last_decisive_at
FROM inventory_states
WHERE monitor_provider_id = ? AND config_version = ? AND journey_key = ? AND offering_key = ?`

const insertStateQuery = `INSERT INTO inventory_states
	(monitor_provider_id, config_version, journey_key, offering_key, state, state_since,
	 last_observed_at, last_decisive_at, last_missing_at, last_reason, last_capacity,
	 train_number, operator, origin_name, destination_name, departure_at, arrival_at,
	 wagon_name, class_name, price_amount, price_currency, last_booking_url, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const updateStateQuery = `UPDATE inventory_states SET
	state = ?, state_since = ?, last_observed_at = ?,
	last_decisive_at = COALESCE(?, last_decisive_at), last_missing_at = NULL,
	last_reason = ?, last_capacity = ?, train_number = ?, operator = ?,
	origin_name = ?, destination_name = ?, departure_at = ?, arrival_at = ?,
	wagon_name = ?, class_name = ?, price_amount = ?, price_currency = ?,
	last_booking_url = ?, updated_at = ?
WHERE id = ?`

const openEpisodeQuery = `SELECT id FROM availability_episodes WHERE state_id = ? AND closed_at IS NULL`

const insertEpisodeQuery = `INSERT INTO availability_episodes
	(state_id, episode_seq, opened_at) VALUES (?, ?, ?)`

const closeEpisodeQuery = `UPDATE availability_episodes
	SET closed_at = ?, close_reason = 'not_reservable'
	WHERE id = ? AND closed_at IS NULL`

const listTargetStatesQuery = `SELECT id, journey_key, offering_key
FROM inventory_states WHERE monitor_provider_id = ? AND config_version = ?`

const markMissingQuery = `UPDATE inventory_states
	SET last_missing_at = ?, updated_at = ? WHERE id = ?`

// Apply advances the availability state machine for a successful provider
// result. It never begins, commits, or rolls back the caller's transaction.
// Event and delivery rows are inserted through NTF-000 inside that same
// transaction. Error/empty/missing observations never close an episode.
func Apply(ctx context.Context, tx *sql.Tx, input Input) (Result, error) {
	if tx == nil {
		return Result{}, errors.New("availability: nil transaction")
	}
	if err := input.validate(); err != nil {
		return Result{}, err
	}
	groups, order, err := buildObservations(input.Result)
	if err != nil {
		return Result{}, err
	}

	input.ObservedAt = input.ObservedAt.UTC()
	input.Now = input.Now.UTC()
	var result Result
	observed := make(map[stateKey]bool, len(order))
	conflictWarning := false

	for _, key := range order {
		obs, conflict := resolveObservation(groups[key])
		observed[key] = true
		if conflict && !conflictWarning {
			result.Warnings = append(result.Warnings, "conflicting_duplicate_keys")
			conflictWarning = true
		}

		current, found, err := loadState(ctx, tx, input, key)
		if err != nil {
			return Result{}, err
		}
		newState := obs.state
		if conflict {
			newState = provider.StateUnknown
		}
		stateID, err := upsertState(ctx, tx, input, key, obs, current, found, newState)
		if err != nil {
			return Result{}, err
		}
		result.StatesUpserted++

		switch newState {
		case provider.StateReservable:
			shouldOpen := !found ||
				current.state == provider.StateNotReservable
			if found && current.state == provider.StateUnknown {
				_, open, err := openEpisodeID(ctx, tx, stateID)
				if err != nil {
					return Result{}, err
				}
				shouldOpen = !open
			}
			if shouldOpen {
				episodeID, err := openEpisode(ctx, tx, stateID, input.ObservedAt)
				if err != nil {
					return Result{}, err
				}
				result.EpisodesOpened = append(result.EpisodesOpened, episodeID)
				enqueued, err := enqueueAvailabilityEvent(ctx, tx, input, obs, episodeID)
				if err != nil {
					return Result{}, err
				}
				if enqueued.Created {
					result.EventsCreated = append(result.EventsCreated, enqueued.EventID)
				}
			}
		case provider.StateNotReservable:
			if found && (current.state == provider.StateReservable || current.state == provider.StateUnknown) {
				episodeID, closed, err := closeOpenEpisode(ctx, tx, stateID, input.ObservedAt)
				if err != nil {
					return Result{}, err
				}
				if closed {
					result.EpisodesClosed = append(result.EpisodesClosed, episodeID)
				}
			}
		}
	}

	missing, err := markMissingStates(ctx, tx, input, observed)
	if err != nil {
		return Result{}, err
	}
	result.MissingStates = missing
	return result, nil
}

func (in Input) validate() error {
	if in.TargetID <= 0 {
		return errors.New("availability: target id must be positive")
	}
	if strings.TrimSpace(in.MonitorID) == "" {
		return errors.New("availability: monitor id is required")
	}
	if strings.TrimSpace(string(in.ProviderID)) == "" {
		return errors.New("availability: provider id is required")
	}
	if in.ConfigVersion < 1 {
		return errors.New("availability: config version must be positive")
	}
	if in.ObservedAt.IsZero() {
		return errors.New("availability: observed_at is required")
	}
	if in.Now.IsZero() {
		return errors.New("availability: now is required")
	}
	for _, delivery := range in.Deliveries {
		if err := delivery.Validate(); err != nil {
			return fmt.Errorf("availability: %w", err)
		}
	}
	return nil
}

func buildObservations(result provider.Result) (map[stateKey][]observation, []stateKey, error) {
	groups := make(map[stateKey][]observation)
	order := make([]stateKey, 0)
	for _, journey := range result.Journeys {
		if strings.TrimSpace(journey.Journey.Key) == "" {
			return nil, nil, errors.New("availability: journey key is required")
		}
		for _, item := range journey.Offerings {
			if strings.TrimSpace(item.Offering.Key) == "" {
				return nil, nil, errors.New("availability: offering key is required")
			}
			if !item.State.Valid() {
				return nil, nil, fmt.Errorf("availability: invalid offering state %q", item.State)
			}
			if item.Capacity != nil && *item.Capacity < 0 {
				return nil, nil, errors.New("availability: capacity must not be negative")
			}
			key := stateKey{journeyKey: journey.Journey.Key, offeringKey: item.Offering.Key}
			if _, exists := groups[key]; !exists {
				order = append(order, key)
			}
			bookingURL := ""
			if item.BookingURL != nil {
				bookingURL = item.BookingURL.String()
			}
			groups[key] = append(groups[key], observation{
				journey:    journey.Journey,
				offering:   item.Offering,
				state:      item.State,
				reason:     item.Reason,
				capacity:   item.Capacity,
				bookingURL: bookingURL,
			})
		}
	}
	return groups, order, nil
}

// resolveObservation collapses duplicate keys. Identical states use the first
// observation; conflicting states force Unknown and suppress the event.
func resolveObservation(observations []observation) (observation, bool) {
	first := observations[0]
	conflict := false
	for _, other := range observations[1:] {
		if other.state != first.state {
			conflict = true
			break
		}
	}
	if conflict {
		first.state = provider.StateUnknown
		first.reason = "conflicting_duplicate_keys"
		first.capacity = nil
		first.bookingURL = ""
	}
	return first, conflict
}

func loadState(ctx context.Context, tx *sql.Tx, input Input, key stateKey) (currentState, bool, error) {
	var (
		current      currentState
		state        string
		stateSince   string
		lastDecisive sql.NullString
	)
	err := tx.QueryRowContext(ctx, selectStateQuery,
		input.TargetID, input.ConfigVersion, key.journeyKey, key.offeringKey,
	).Scan(&current.id, &state, &stateSince, &lastDecisive)
	if errors.Is(err, sql.ErrNoRows) {
		return currentState{}, false, nil
	}
	if err != nil {
		return currentState{}, false, fmt.Errorf("availability: load state: %w", err)
	}
	current.state = provider.State(state)
	if !current.state.Valid() {
		return currentState{}, false, fmt.Errorf("availability: stored invalid state %q", state)
	}
	current.stateSince, err = parseTime(stateSince)
	if err != nil {
		return currentState{}, false, err
	}
	if lastDecisive.Valid {
		parsed, err := parseTime(lastDecisive.String)
		if err != nil {
			return currentState{}, false, err
		}
		current.lastDecisiveAt = &parsed
	}
	return current, true, nil
}

func upsertState(ctx context.Context, tx *sql.Tx, input Input, key stateKey, obs observation, current currentState, found bool, newState provider.State) (int64, error) {
	stateSince := input.ObservedAt
	if found && current.state == newState {
		stateSince = current.stateSince
	}
	var decisive any
	if newState == provider.StateReservable || newState == provider.StateNotReservable {
		decisive = formatTime(input.ObservedAt)
	}

	var trainNumber, operator, originName, destinationName any
	var departureAt, arrivalAt any
	var wagonName, className any
	var priceAmount any
	var priceCurrency any
	if obs.offering.Price != nil {
		amount := obs.offering.Price.Amount
		priceAmount = amount
		priceCurrency = nullableString(obs.offering.Price.Currency)
	}
	trainNumber = nullableString(obs.journey.TrainNumber)
	operator = nullableString(obs.journey.Operator)
	originName = nullableString(obs.journey.OriginName)
	destinationName = nullableString(obs.journey.DestinationName)
	departureAt = nullableTime(obs.journey.DepartureAt)
	arrivalAt = nullableTime(obs.journey.ArrivalAt)
	wagonName = nullableString(obs.offering.WagonName)
	className = nullableString(obs.offering.ClassName)

	if !found {
		result, err := tx.ExecContext(ctx, insertStateQuery,
			input.TargetID, input.ConfigVersion, key.journeyKey, key.offeringKey,
			string(newState), formatTime(stateSince), formatTime(input.ObservedAt),
			decisive, nullableString(obs.reason), nullableInt(obs.capacity),
			trainNumber, operator, originName, destinationName, departureAt, arrivalAt,
			wagonName, className, priceAmount, priceCurrency,
			nullableString(obs.bookingURL), formatTime(input.Now),
		)
		if err != nil {
			return 0, fmt.Errorf("availability: insert state: %w", err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("availability: insert state id: %w", err)
		}
		return id, nil
	}

	result, err := tx.ExecContext(ctx, updateStateQuery,
		string(newState), formatTime(stateSince), formatTime(input.ObservedAt),
		decisive, nullableString(obs.reason), nullableInt(obs.capacity),
		trainNumber, operator, originName, destinationName, departureAt, arrivalAt,
		wagonName, className, priceAmount, priceCurrency,
		nullableString(obs.bookingURL), formatTime(input.Now), current.id,
	)
	if err != nil {
		return 0, fmt.Errorf("availability: update state: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("availability: update state rows: %w", err)
	}
	if updated != 1 {
		return 0, errors.New("availability: state disappeared during update")
	}
	return current.id, nil
}

func openEpisodeID(ctx context.Context, tx *sql.Tx, stateID int64) (int64, bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx, openEpisodeQuery, stateID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("availability: find open episode: %w", err)
	}
	return id, true, nil
}

func openEpisode(ctx context.Context, tx *sql.Tx, stateID int64, openedAt time.Time) (int64, error) {
	var next int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(episode_seq), 0) + 1 FROM availability_episodes WHERE state_id = ?`,
		stateID,
	).Scan(&next); err != nil {
		return 0, fmt.Errorf("availability: next episode sequence: %w", err)
	}
	result, err := tx.ExecContext(ctx, insertEpisodeQuery, stateID, next, formatTime(openedAt))
	if err != nil {
		return 0, fmt.Errorf("availability: open episode: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("availability: open episode id: %w", err)
	}
	return id, nil
}

func closeOpenEpisode(ctx context.Context, tx *sql.Tx, stateID int64, closedAt time.Time) (int64, bool, error) {
	id, open, err := openEpisodeID(ctx, tx, stateID)
	if err != nil || !open {
		return 0, false, err
	}
	result, err := tx.ExecContext(ctx, closeEpisodeQuery, formatTime(closedAt), id)
	if err != nil {
		return 0, false, fmt.Errorf("availability: close episode: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("availability: close episode rows: %w", err)
	}
	if updated != 1 {
		return 0, false, errors.New("availability: open episode disappeared during close")
	}
	return id, true, nil
}

func enqueueAvailabilityEvent(ctx context.Context, tx *sql.Tx, input Input, obs observation, episodeID int64) (notify.EnqueueResult, error) {
	var priceAmount *int64
	priceCurrency := ""
	if obs.offering.Price != nil {
		amount := obs.offering.Price.Amount
		priceAmount = &amount
		priceCurrency = obs.offering.Price.Currency
	}
	episode := episodeID
	event := notify.Event{
		ID:              fmt.Sprintf("availability-episode-%d", episodeID),
		Kind:            notify.KindAvailability,
		DedupeKey:       fmt.Sprintf("availability:episode:%d", episodeID),
		MonitorID:       input.MonitorID,
		ProviderID:      string(input.ProviderID),
		EpisodeID:       &episode,
		OccurredAt:      input.ObservedAt,
		TrainNumber:     obs.journey.TrainNumber,
		Operator:        obs.journey.Operator,
		OriginName:      obs.journey.OriginName,
		DestinationName: obs.journey.DestinationName,
		DepartureAt:     obs.journey.DepartureAt,
		ArrivalAt:       obs.journey.ArrivalAt,
		WagonName:       obs.offering.WagonName,
		ClassName:       obs.offering.ClassName,
		PriceAmount:     priceAmount,
		PriceCurrency:   priceCurrency,
		BookingURL:      obs.bookingURL,
		CreatedAt:       input.Now,
	}
	enqueued, err := notify.Enqueue(ctx, tx, event, input.Deliveries)
	if err != nil {
		return notify.EnqueueResult{}, fmt.Errorf("availability: enqueue event: %w", err)
	}
	return enqueued, nil
}

func markMissingStates(ctx context.Context, tx *sql.Tx, input Input, observed map[stateKey]bool) (int, error) {
	rows, err := tx.QueryContext(ctx, listTargetStatesQuery, input.TargetID, input.ConfigVersion)
	if err != nil {
		return 0, fmt.Errorf("availability: list states for missing update: %w", err)
	}
	type stateRow struct {
		id  int64
		key stateKey
	}
	var states []stateRow
	for rows.Next() {
		var row stateRow
		if err := rows.Scan(&row.id, &row.key.journeyKey, &row.key.offeringKey); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("availability: scan state for missing update: %w", err)
		}
		states = append(states, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("availability: iterate states for missing update: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("availability: close state rows: %w", err)
	}

	missing := 0
	for _, state := range states {
		if observed[state.key] {
			continue
		}
		if _, err := tx.ExecContext(ctx, markMissingQuery,
			formatTime(input.ObservedAt), formatTime(input.Now), state.id,
		); err != nil {
			return 0, fmt.Errorf("availability: mark state missing: %w", err)
		}
		missing++
	}
	return missing, nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("availability: parse timestamp: %w", err)
	}
	return parsed.UTC(), nil
}

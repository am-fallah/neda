package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/notify"
)

// NTF-006 owns the notification/event/delivery read model. These queries are
// read-only and return committed records. Event creation, delivery claiming,
// retries, and HTTP endpoints belong to NTF-000/NTF-001/API-003.
//
// Ordering is chronological with a stable identity tie-breaker. The stored
// timestamps use RFC3339Nano with variable fractional precision, so the read
// model orders by SQLite's time value rather than by raw text. Cursors are
// opaque and bound to the same ordering tuple; callers must not construct them.

const (
	// DefaultReadLimit is the page size used when a query omits Limit.
	DefaultReadLimit = 50
	// MaxReadLimit is the largest page a read-model query may request.
	MaxReadLimit = 100

	maxReadCursorBytes = 512
)

var (
	// ErrInvalidCursor reports an unreadable or malformed pagination cursor.
	ErrInvalidCursor = errors.New("store: invalid read cursor")
	// ErrInvalidFilter reports an unsupported filter, limit, or identifier.
	ErrInvalidFilter = errors.New("store: invalid read filter")
)

// DeliveryStatus is the persisted notification_deliveries.status value.
type DeliveryStatus string

const (
	DeliveryPending        DeliveryStatus = "pending"
	DeliverySending        DeliveryStatus = "sending"
	DeliverySent           DeliveryStatus = "sent"
	DeliveryRetryScheduled DeliveryStatus = "retry_scheduled"
	DeliveryFailed         DeliveryStatus = "failed"
	DeliverySkipped        DeliveryStatus = "skipped"
)

// Valid reports whether the status is part of the persisted schema.
func (s DeliveryStatus) Valid() bool {
	switch s {
	case DeliveryPending, DeliverySending, DeliverySent,
		DeliveryRetryScheduled, DeliveryFailed, DeliverySkipped:
		return true
	default:
		return false
	}
}

// Terminal reports whether the delivery has reached a final status. Pending,
// sending, and retry_scheduled remain in flight.
func (s DeliveryStatus) Terminal() bool {
	switch s {
	case DeliverySent, DeliveryFailed, DeliverySkipped:
		return true
	default:
		return false
	}
}

// DeliveryState is one event delivery target and its sanitized transport state.
// The target key is intentionally not exposed; the delivery ID is the stable
// public identity.
type DeliveryState struct {
	ID            string
	Channel       notify.Channel
	Status        DeliveryStatus
	AttemptCount  int
	NextAttemptAt *time.Time
	LastAttemptAt *time.Time
	SentAt        *time.Time
	LastErrorKind string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// DeliverySummary counts the persisted delivery rows for one event. Attempted
// counts deliveries with at least one attempt; Terminal counts sent, failed,
// and skipped rows. These are derived views, not new persisted states.
type DeliverySummary struct {
	Total          int
	Pending        int
	Sending        int
	Sent           int
	RetryScheduled int
	Failed         int
	Skipped        int
	Attempted      int
	Terminal       int
}

// EventRecord is one durable notification event and its per-channel delivery
// state. Availability events carry the monitor label when the monitor still
// exists; provider-health events intentionally carry no monitor identity.
type EventRecord struct {
	ID         string
	Kind       notify.EventKind
	ProviderID string

	MonitorID    string
	MonitorLabel string
	EpisodeID    *int64

	OccurredAt time.Time

	TrainNumber     string
	Operator        string
	OriginName      string
	DestinationName string
	DepartureAt     *time.Time
	ArrivalAt       *time.Time
	WagonName       string
	ClassName       string
	PriceAmount     *int64
	PriceCurrency   string
	BookingURL      string

	ErrorKind    string
	FailureCount *int

	CreatedAt time.Time

	Deliveries      []DeliveryState
	DeliverySummary DeliverySummary
}

// DeliveryRecord is one delivery row with the durable event context needed by a
// delivery-history view. Raw error details and target keys are not exposed.
type DeliveryRecord struct {
	ID              string
	EventID         string
	EventKind       notify.EventKind
	EventOccurredAt time.Time
	ProviderID      string
	MonitorID       string

	Channel       notify.Channel
	Status        DeliveryStatus
	AttemptCount  int
	NextAttemptAt *time.Time
	LastAttemptAt *time.Time
	SentAt        *time.Time
	LastErrorKind string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// EventQuery filters a notification-event page. Zero values mean no filter.
type EventQuery struct {
	Limit      int
	Cursor     string
	Kind       notify.EventKind
	MonitorID  string
	ProviderID string
}

// DeliveryQuery filters a delivery-history page. Zero values mean no filter.
type DeliveryQuery struct {
	Limit   int
	Cursor  string
	Channel notify.Channel
	Status  DeliveryStatus
	EventID string
	Kind    notify.EventKind
}

// EventPage is one page of events. Events is never nil. NextCursor is empty on
// the last page.
type EventPage struct {
	Events     []EventRecord
	NextCursor string
}

// DeliveryPage is one page of deliveries. Deliveries is never nil. NextCursor
// is empty on the last page.
type DeliveryPage struct {
	Deliveries []DeliveryRecord
	NextCursor string
}

const eventReadSelect = `SELECT e.id, e.kind, e.provider_id, e.monitor_id, m.label,
	e.episode_id, e.occurred_at, e.train_number, e.operator, e.origin_name,
	e.destination_name, e.departure_at, e.arrival_at, e.wagon_name, e.class_name,
	e.price_amount, e.price_currency, e.booking_url, e.error_kind, e.failure_count,
	e.created_at
FROM events e
LEFT JOIN monitors m ON m.id = e.monitor_id`

const deliveryReadSelect = `SELECT d.id, d.event_id, e.kind, e.occurred_at, e.provider_id,
	e.monitor_id, d.channel, d.status, d.attempt_count, d.next_attempt_at,
	d.last_attempt_at, d.sent_at, d.last_error_kind, d.created_at, d.updated_at
FROM notification_deliveries d
JOIN events e ON e.id = d.event_id`

const deliveryStateSelect = `SELECT event_id, id, channel, status, attempt_count,
	next_attempt_at, last_attempt_at, sent_at, last_error_kind, created_at, updated_at
FROM notification_deliveries
WHERE event_id IN (%s)
ORDER BY event_id, channel, id`

// ListEvents returns one chronologically ordered page of durable events with
// nested delivery state. It never returns rows for a monitor or provider outside
// the supplied filters.
func (s *Store) ListEvents(ctx context.Context, query EventQuery) (EventPage, error) {
	limit, err := normalizeReadLimit(query.Limit)
	if err != nil {
		return EventPage{}, err
	}
	if query.Kind != "" && !validEventKind(query.Kind) {
		return EventPage{}, fmt.Errorf("%w: unsupported event kind %q", ErrInvalidFilter, query.Kind)
	}
	if query.MonitorID != "" && strings.TrimSpace(query.MonitorID) == "" {
		return EventPage{}, fmt.Errorf("%w: monitor id must not be blank", ErrInvalidFilter)
	}
	if query.ProviderID != "" && strings.TrimSpace(query.ProviderID) == "" {
		return EventPage{}, fmt.Errorf("%w: provider id must not be blank", ErrInvalidFilter)
	}

	db, err := s.executor()
	if err != nil {
		return EventPage{}, err
	}

	conditions := make([]string, 0, 5)
	args := make([]any, 0, 8)
	if query.Kind != "" {
		conditions = append(conditions, "e.kind = ?")
		args = append(args, string(query.Kind))
	}
	if query.MonitorID != "" {
		conditions = append(conditions, "e.monitor_id = ?")
		args = append(args, query.MonitorID)
	}
	if query.ProviderID != "" {
		conditions = append(conditions, "e.provider_id = ?")
		args = append(args, query.ProviderID)
	}
	if query.Cursor != "" {
		cursor, err := decodeReadCursor(query.Cursor)
		if err != nil {
			return EventPage{}, err
		}
		conditions = append(conditions, "(julianday(e.occurred_at) < julianday(?) OR (julianday(e.occurred_at) = julianday(?) AND e.id < ?))")
		args = append(args, cursor.Timestamp, cursor.Timestamp, cursor.ID)
	}

	statement := eventReadSelect
	if len(conditions) > 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	statement += " ORDER BY julianday(e.occurred_at) DESC, e.id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		return EventPage{}, wrap("list notification events", err)
	}
	defer rows.Close()

	events := make([]EventRecord, 0, limit)
	for rows.Next() {
		event, err := scanEventRecord(rows)
		if err != nil {
			return EventPage{}, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return EventPage{}, wrap("iterate notification events", err)
	}
	if err := rows.Close(); err != nil {
		return EventPage{}, wrap("close notification events", err)
	}

	page := EventPage{Events: events}
	if len(events) > limit {
		page.Events = events[:limit]
		last := page.Events[len(page.Events)-1]
		page.NextCursor = encodeReadCursor(last.OccurredAt, last.ID)
	}
	if len(page.Events) == 0 {
		page.Events = []EventRecord{}
		return page, nil
	}

	ids := make([]string, 0, len(page.Events))
	for _, event := range page.Events {
		ids = append(ids, event.ID)
	}
	deliveries, err := loadDeliveryStates(ctx, db, ids)
	if err != nil {
		return EventPage{}, err
	}
	for index := range page.Events {
		states := deliveries[page.Events[index].ID]
		if states == nil {
			states = []DeliveryState{}
		}
		page.Events[index].Deliveries = states
		page.Events[index].DeliverySummary = summarizeDeliveries(states)
	}
	return page, nil
}

// GetEvent returns one event with its delivery state, or ErrNotFound.
func (s *Store) GetEvent(ctx context.Context, id string) (EventRecord, error) {
	if strings.TrimSpace(id) == "" {
		return EventRecord{}, fmt.Errorf("%w: event id is required", ErrInvalidFilter)
	}
	db, err := s.executor()
	if err != nil {
		return EventRecord{}, err
	}
	rows, err := db.QueryContext(ctx, eventReadSelect+" WHERE e.id = ?", id)
	if err != nil {
		return EventRecord{}, wrap("get notification event", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return EventRecord{}, wrap("get notification event", err)
		}
		return EventRecord{}, ErrNotFound
	}
	event, err := scanEventRecord(rows)
	if err != nil {
		return EventRecord{}, err
	}
	if err := rows.Close(); err != nil {
		return EventRecord{}, wrap("close notification event", err)
	}

	deliveries, err := loadDeliveryStates(ctx, db, []string{event.ID})
	if err != nil {
		return EventRecord{}, err
	}
	event.Deliveries = deliveries[event.ID]
	if event.Deliveries == nil {
		event.Deliveries = []DeliveryState{}
	}
	event.DeliverySummary = summarizeDeliveries(event.Deliveries)
	return event, nil
}

// ListDeliveries returns one page of delivery history ordered by enqueue time.
func (s *Store) ListDeliveries(ctx context.Context, query DeliveryQuery) (DeliveryPage, error) {
	limit, err := normalizeReadLimit(query.Limit)
	if err != nil {
		return DeliveryPage{}, err
	}
	if query.Channel != "" && !validChannel(query.Channel) {
		return DeliveryPage{}, fmt.Errorf("%w: unsupported channel %q", ErrInvalidFilter, query.Channel)
	}
	if query.Status != "" && !query.Status.Valid() {
		return DeliveryPage{}, fmt.Errorf("%w: unsupported delivery status %q", ErrInvalidFilter, query.Status)
	}
	if query.Kind != "" && !validEventKind(query.Kind) {
		return DeliveryPage{}, fmt.Errorf("%w: unsupported event kind %q", ErrInvalidFilter, query.Kind)
	}
	if query.EventID != "" && strings.TrimSpace(query.EventID) == "" {
		return DeliveryPage{}, fmt.Errorf("%w: event id must not be blank", ErrInvalidFilter)
	}

	db, err := s.executor()
	if err != nil {
		return DeliveryPage{}, err
	}

	conditions := make([]string, 0, 5)
	args := make([]any, 0, 8)
	if query.Channel != "" {
		conditions = append(conditions, "d.channel = ?")
		args = append(args, string(query.Channel))
	}
	if query.Status != "" {
		conditions = append(conditions, "d.status = ?")
		args = append(args, string(query.Status))
	}
	if query.EventID != "" {
		conditions = append(conditions, "d.event_id = ?")
		args = append(args, query.EventID)
	}
	if query.Kind != "" {
		conditions = append(conditions, "e.kind = ?")
		args = append(args, string(query.Kind))
	}
	if query.Cursor != "" {
		cursor, err := decodeReadCursor(query.Cursor)
		if err != nil {
			return DeliveryPage{}, err
		}
		conditions = append(conditions, "(julianday(d.created_at) < julianday(?) OR (julianday(d.created_at) = julianday(?) AND d.id < ?))")
		args = append(args, cursor.Timestamp, cursor.Timestamp, cursor.ID)
	}

	statement := deliveryReadSelect
	if len(conditions) > 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	statement += " ORDER BY julianday(d.created_at) DESC, d.id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		return DeliveryPage{}, wrap("list notification deliveries", err)
	}
	defer rows.Close()

	deliveries := make([]DeliveryRecord, 0, limit)
	for rows.Next() {
		delivery, err := scanDeliveryRecord(rows)
		if err != nil {
			return DeliveryPage{}, err
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return DeliveryPage{}, wrap("iterate notification deliveries", err)
	}
	if err := rows.Close(); err != nil {
		return DeliveryPage{}, wrap("close notification deliveries", err)
	}

	page := DeliveryPage{Deliveries: deliveries}
	if len(deliveries) > limit {
		page.Deliveries = deliveries[:limit]
		last := page.Deliveries[len(page.Deliveries)-1]
		page.NextCursor = encodeReadCursor(last.CreatedAt, last.ID)
	}
	if len(page.Deliveries) == 0 {
		page.Deliveries = []DeliveryRecord{}
	}
	return page, nil
}

func scanEventRecord(rows *sql.Rows) (EventRecord, error) {
	var (
		event                                       EventRecord
		kind, providerID                            string
		monitorID, monitorLabel                     sql.NullString
		episodeID                                   sql.NullInt64
		occurredAt                                  string
		trainNumber, operator, originName, destName sql.NullString
		departureAt, arrivalAt                      sql.NullString
		wagonName, className, priceCurrency         sql.NullString
		priceAmount                                 sql.NullInt64
		bookingURL, errorKind                       sql.NullString
		failureCount                                sql.NullInt64
		createdAt                                   string
	)
	if err := rows.Scan(
		&event.ID, &kind, &providerID, &monitorID, &monitorLabel,
		&episodeID, &occurredAt, &trainNumber, &operator, &originName,
		&destName, &departureAt, &arrivalAt, &wagonName, &className,
		&priceAmount, &priceCurrency, &bookingURL, &errorKind, &failureCount,
		&createdAt,
	); err != nil {
		return EventRecord{}, wrap("scan notification event", err)
	}
	if !validEventKind(notify.EventKind(kind)) {
		return EventRecord{}, fmt.Errorf("store: event %q has invalid kind %q", event.ID, kind)
	}
	event.Kind = notify.EventKind(kind)
	event.ProviderID = providerID
	event.MonitorID = optionalString(monitorID)
	event.MonitorLabel = optionalString(monitorLabel)
	if episodeID.Valid {
		value := episodeID.Int64
		event.EpisodeID = &value
	}
	var err error
	if event.OccurredAt, err = parseTime(occurredAt); err != nil {
		return EventRecord{}, err
	}
	event.TrainNumber = optionalString(trainNumber)
	event.Operator = optionalString(operator)
	event.OriginName = optionalString(originName)
	event.DestinationName = optionalString(destName)
	if event.DepartureAt, err = parseOptionalTime(departureAt); err != nil {
		return EventRecord{}, err
	}
	if event.ArrivalAt, err = parseOptionalTime(arrivalAt); err != nil {
		return EventRecord{}, err
	}
	event.WagonName = optionalString(wagonName)
	event.ClassName = optionalString(className)
	if priceAmount.Valid {
		value := priceAmount.Int64
		event.PriceAmount = &value
	}
	event.PriceCurrency = optionalString(priceCurrency)
	event.BookingURL = optionalString(bookingURL)
	event.ErrorKind = optionalString(errorKind)
	if failureCount.Valid {
		value := int(failureCount.Int64)
		event.FailureCount = &value
	}
	if event.CreatedAt, err = parseTime(createdAt); err != nil {
		return EventRecord{}, err
	}
	return event, nil
}

func scanDeliveryRecord(rows *sql.Rows) (DeliveryRecord, error) {
	var (
		delivery                     DeliveryRecord
		eventKind, providerID        string
		monitorID                    sql.NullString
		eventOccurredAt              string
		channel, status              string
		nextAttemptAt, lastAttemptAt sql.NullString
		sentAt, lastErrorKind        sql.NullString
		createdAt, updatedAt         string
	)
	if err := rows.Scan(
		&delivery.ID, &delivery.EventID, &eventKind, &eventOccurredAt, &providerID,
		&monitorID, &channel, &status, &delivery.AttemptCount, &nextAttemptAt,
		&lastAttemptAt, &sentAt, &lastErrorKind, &createdAt, &updatedAt,
	); err != nil {
		return DeliveryRecord{}, wrap("scan notification delivery", err)
	}
	if !validEventKind(notify.EventKind(eventKind)) {
		return DeliveryRecord{}, fmt.Errorf("store: delivery %q has invalid event kind %q", delivery.ID, eventKind)
	}
	if !validChannel(notify.Channel(channel)) {
		return DeliveryRecord{}, fmt.Errorf("store: delivery %q has invalid channel %q", delivery.ID, channel)
	}
	deliveryStatus := DeliveryStatus(status)
	if !deliveryStatus.Valid() {
		return DeliveryRecord{}, fmt.Errorf("store: delivery %q has invalid status %q", delivery.ID, status)
	}
	delivery.EventKind = notify.EventKind(eventKind)
	delivery.ProviderID = providerID
	delivery.MonitorID = optionalString(monitorID)
	delivery.Channel = notify.Channel(channel)
	delivery.Status = deliveryStatus
	var err error
	if delivery.EventOccurredAt, err = parseTime(eventOccurredAt); err != nil {
		return DeliveryRecord{}, err
	}
	if delivery.NextAttemptAt, err = parseOptionalTime(nextAttemptAt); err != nil {
		return DeliveryRecord{}, err
	}
	if delivery.LastAttemptAt, err = parseOptionalTime(lastAttemptAt); err != nil {
		return DeliveryRecord{}, err
	}
	if delivery.SentAt, err = parseOptionalTime(sentAt); err != nil {
		return DeliveryRecord{}, err
	}
	delivery.LastErrorKind = optionalString(lastErrorKind)
	if delivery.CreatedAt, err = parseTime(createdAt); err != nil {
		return DeliveryRecord{}, err
	}
	if delivery.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return DeliveryRecord{}, err
	}
	return delivery, nil
}

func loadDeliveryStates(ctx context.Context, db dbtx, eventIDs []string) (map[string][]DeliveryState, error) {
	result := make(map[string][]DeliveryState, len(eventIDs))
	if len(eventIDs) == 0 {
		return result, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(eventIDs)), ",")
	args := make([]any, 0, len(eventIDs))
	for _, id := range eventIDs {
		args = append(args, id)
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(deliveryStateSelect, placeholders), args...)
	if err != nil {
		return nil, wrap("list event deliveries", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			state                    DeliveryState
			eventID, channel, status string
			nextAttemptAt            sql.NullString
			lastAttemptAt            sql.NullString
			sentAt, lastErrorKind    sql.NullString
			createdAt, updatedAt     string
		)
		if err := rows.Scan(
			&eventID, &state.ID, &channel, &status, &state.AttemptCount, &nextAttemptAt,
			&lastAttemptAt, &sentAt, &lastErrorKind, &createdAt, &updatedAt,
		); err != nil {
			return nil, wrap("scan event delivery", err)
		}
		if !validChannel(notify.Channel(channel)) {
			return nil, fmt.Errorf("store: event %q has invalid delivery channel %q", eventID, channel)
		}
		deliveryStatus := DeliveryStatus(status)
		if !deliveryStatus.Valid() {
			return nil, fmt.Errorf("store: event %q has invalid delivery status %q", eventID, status)
		}
		state.Channel = notify.Channel(channel)
		state.Status = deliveryStatus
		var err error
		if state.NextAttemptAt, err = parseOptionalTime(nextAttemptAt); err != nil {
			return nil, err
		}
		if state.LastAttemptAt, err = parseOptionalTime(lastAttemptAt); err != nil {
			return nil, err
		}
		if state.SentAt, err = parseOptionalTime(sentAt); err != nil {
			return nil, err
		}
		state.LastErrorKind = optionalString(lastErrorKind)
		if state.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if state.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return nil, err
		}
		result[eventID] = append(result[eventID], state)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate event deliveries", err)
	}
	return result, nil
}

func summarizeDeliveries(deliveries []DeliveryState) DeliverySummary {
	summary := DeliverySummary{Total: len(deliveries)}
	for _, delivery := range deliveries {
		switch delivery.Status {
		case DeliveryPending:
			summary.Pending++
		case DeliverySending:
			summary.Sending++
		case DeliverySent:
			summary.Sent++
		case DeliveryRetryScheduled:
			summary.RetryScheduled++
		case DeliveryFailed:
			summary.Failed++
		case DeliverySkipped:
			summary.Skipped++
		}
		if delivery.AttemptCount > 0 {
			summary.Attempted++
		}
		if delivery.Status.Terminal() {
			summary.Terminal++
		}
	}
	return summary
}

type readCursor struct {
	Timestamp string `json:"ts"`
	ID        string `json:"id"`
}

func encodeReadCursor(timestamp time.Time, id string) string {
	raw, err := json.Marshal(readCursor{Timestamp: formatTime(timestamp), ID: id})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeReadCursor(value string) (readCursor, error) {
	if strings.TrimSpace(value) == "" || len(value) > maxReadCursorBytes {
		return readCursor{}, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return readCursor{}, ErrInvalidCursor
	}
	var cursor readCursor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return readCursor{}, ErrInvalidCursor
	}
	if strings.TrimSpace(cursor.ID) == "" {
		return readCursor{}, ErrInvalidCursor
	}
	parsed, err := parseTime(cursor.Timestamp)
	if err != nil {
		return readCursor{}, ErrInvalidCursor
	}
	cursor.Timestamp = formatTime(parsed)
	return cursor, nil
}

func normalizeReadLimit(limit int) (int, error) {
	if limit == 0 {
		return DefaultReadLimit, nil
	}
	if limit < 1 || limit > MaxReadLimit {
		return 0, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidFilter, MaxReadLimit)
	}
	return limit, nil
}

func validEventKind(kind notify.EventKind) bool {
	switch kind {
	case notify.KindAvailability, notify.KindProviderDegraded, notify.KindProviderRecovered:
		return true
	default:
		return false
	}
}

func validChannel(channel notify.Channel) bool {
	switch channel {
	case notify.ChannelTelegram, notify.ChannelPush:
		return true
	default:
		return false
	}
}

func optionalString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

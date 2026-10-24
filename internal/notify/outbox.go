// Package notify owns the transactional notification outbox boundary.
//
// NTF-000 provides the schema-backed enqueue helper and minimal dedupe
// read-back. It does not claim, lease, retry, format, or deliver notifications;
// those responsibilities belong to later tasks.
package notify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EventKind is a durable event category.
type EventKind string

const (
	KindAvailability      EventKind = "availability"
	KindProviderDegraded  EventKind = "provider_degraded"
	KindProviderRecovered EventKind = "provider_recovered"
)

func (k EventKind) valid() bool {
	switch k {
	case KindAvailability, KindProviderDegraded, KindProviderRecovered:
		return true
	default:
		return false
	}
}

// Channel is a notification delivery channel.
type Channel string

const (
	ChannelTelegram Channel = "telegram"
	ChannelPush     Channel = "push"
)

func (c Channel) valid() bool {
	switch c {
	case ChannelTelegram, ChannelPush:
		return true
	default:
		return false
	}
}

// Event is one durable availability or provider-health event. IDs and dedupe
// keys are supplied by the caller; this package does not own identity policy.
type Event struct {
	ID        string
	Kind      EventKind
	DedupeKey string

	MonitorID  string
	ProviderID string
	EpisodeID  *int64

	OccurredAt      time.Time
	TrainNumber     string
	Operator        string
	OriginName      string
	DestinationName string

	DepartureAt *time.Time
	ArrivalAt   *time.Time

	WagonName     string
	ClassName     string
	PriceAmount   *int64
	PriceCurrency string
	BookingURL    string

	ErrorKind    string
	FailureCount *int

	CreatedAt time.Time
}

// Delivery is one initial outbox target for an event. The outbox owns the
// stored delivery ID: it is derived deterministically from the event ID and
// this destination identity so callers cannot collide the global primary key
// when one transaction produces multiple events.
type Delivery struct {
	Channel   Channel
	TargetKey string
}

// EnqueueResult reports the event identity and whether this call inserted it.
type EnqueueResult struct {
	EventID string
	Created bool
}

// Validate checks the event's required fields and kind-specific shape before
// any insert occurs.
func (e Event) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return errors.New("notify: event id is required")
	}
	if strings.TrimSpace(e.DedupeKey) == "" {
		return errors.New("notify: event dedupe key is required")
	}
	if !e.Kind.valid() {
		return fmt.Errorf("notify: invalid event kind %q", e.Kind)
	}
	if strings.TrimSpace(e.ProviderID) == "" {
		return errors.New("notify: event provider id is required")
	}
	if e.OccurredAt.IsZero() {
		return errors.New("notify: event occurred_at is required")
	}
	if e.CreatedAt.IsZero() {
		return errors.New("notify: event created_at is required")
	}

	switch e.Kind {
	case KindAvailability:
		if strings.TrimSpace(e.MonitorID) == "" {
			return errors.New("notify: availability event monitor id is required")
		}
		if e.EpisodeID == nil || *e.EpisodeID <= 0 {
			return errors.New("notify: availability event episode id is required")
		}
		if strings.TrimSpace(e.ErrorKind) != "" || e.FailureCount != nil {
			return errors.New("notify: availability event must not carry health fields")
		}
	case KindProviderDegraded, KindProviderRecovered:
		if strings.TrimSpace(e.MonitorID) != "" {
			return errors.New("notify: provider-health event must not carry a monitor id")
		}
		if e.EpisodeID != nil {
			return errors.New("notify: provider-health event must not carry an availability episode id")
		}
		if strings.TrimSpace(e.ErrorKind) == "" {
			return errors.New("notify: provider-health event error kind is required")
		}
		if e.FailureCount == nil || *e.FailureCount < 1 {
			return errors.New("notify: provider-health event failure count must be at least 1")
		}
	}
	return nil
}

// Validate checks the delivery channel and target-key shape before any insert.
func (d Delivery) Validate() error {
	switch d.Channel {
	case ChannelTelegram:
		if d.TargetKey != "telegram" {
			return errors.New(`notify: telegram target key must be "telegram"`)
		}
	case ChannelPush:
		if !strings.HasPrefix(d.TargetKey, "push:") || strings.TrimSpace(strings.TrimPrefix(d.TargetKey, "push:")) == "" {
			return errors.New(`notify: push target key must be "push:<subscription_id>"`)
		}
	default:
		return fmt.Errorf("notify: invalid delivery channel %q", d.Channel)
	}
	return nil
}

const insertEventQuery = `INSERT INTO events
	(id, kind, dedupe_key, monitor_id, provider_id, episode_id, occurred_at,
	 train_number, operator, origin_name, destination_name, departure_at, arrival_at,
	 wagon_name, class_name, price_amount, price_currency, booking_url,
	 error_kind, failure_count, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(dedupe_key) DO NOTHING`

const insertDeliveryQuery = `INSERT INTO notification_deliveries
	(id, event_id, channel, target_key, status, attempt_count, next_attempt_at,
	 last_attempt_at, sent_at, last_error_kind, lease_until, created_at, updated_at)
VALUES (?, ?, ?, ?, 'pending', 0, ?, NULL, NULL, NULL, NULL, ?, ?)
ON CONFLICT(event_id, channel, target_key) DO NOTHING`

const findEventByDedupeKeyQuery = `SELECT id FROM events WHERE dedupe_key = ?`

// Enqueue inserts one event and its initial delivery rows inside the caller's
// transaction. It never begins, commits, or rolls back that transaction.
//
// A duplicate dedupe key returns the existing event ID with Created=false and
// inserts no deliveries. Duplicate delivery targets are no-ops. A caller error
// after a partial enqueue must roll back the supplied transaction.
func Enqueue(ctx context.Context, tx *sql.Tx, event Event, deliveries []Delivery) (EnqueueResult, error) {
	if tx == nil {
		return EnqueueResult{}, errors.New("notify: nil transaction")
	}
	if err := event.Validate(); err != nil {
		return EnqueueResult{}, err
	}
	if err := validateDeliveries(deliveries); err != nil {
		return EnqueueResult{}, err
	}

	event = normalizeEvent(event)
	result, err := tx.ExecContext(ctx, insertEventQuery, eventArgs(event)...)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("notify: insert event: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("notify: event rows affected: %w", err)
	}
	if inserted == 0 {
		existingID, found, err := FindEventIDByDedupeKey(ctx, tx, event.DedupeKey)
		if err != nil {
			return EnqueueResult{}, err
		}
		if !found {
			return EnqueueResult{}, errors.New("notify: event dedupe conflict without an existing row")
		}
		return EnqueueResult{EventID: existingID, Created: false}, nil
	}

	seen := make(map[string]struct{}, len(deliveries))
	for _, delivery := range deliveries {
		key := string(delivery.Channel) + "\x00" + delivery.TargetKey
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		deliveryID := deterministicDeliveryID(event.ID, delivery.Channel, delivery.TargetKey)
		if _, err := tx.ExecContext(ctx, insertDeliveryQuery,
			deliveryID, event.ID, string(delivery.Channel), delivery.TargetKey,
			formatTime(event.CreatedAt), formatTime(event.CreatedAt), formatTime(event.CreatedAt),
		); err != nil {
			return EnqueueResult{}, fmt.Errorf("notify: insert delivery: %w", err)
		}
	}
	return EnqueueResult{EventID: event.ID, Created: true}, nil
}

// deterministicDeliveryID derives a stable UUID from the event and destination
// identity. The same event/destination always produces the same row ID, while
// different events with the same destination produce different IDs.
func deterministicDeliveryID(eventID string, channel Channel, targetKey string) string {
	name := eventID + "\x00" + string(channel) + "\x00" + targetKey
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(name)).String()
}

// FindEventIDByDedupeKey returns the existing event ID for a dedupe key. It is
// the minimal read-back needed by event producers; it is not an API read model.
func FindEventIDByDedupeKey(ctx context.Context, tx *sql.Tx, key string) (string, bool, error) {
	if tx == nil {
		return "", false, errors.New("notify: nil transaction")
	}
	if strings.TrimSpace(key) == "" {
		return "", false, errors.New("notify: dedupe key is required")
	}
	var id string
	if err := tx.QueryRowContext(ctx, findEventByDedupeKeyQuery, key).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("notify: find event by dedupe key: %w", err)
	}
	return id, true, nil
}

func validateDeliveries(deliveries []Delivery) error {
	for _, delivery := range deliveries {
		if err := delivery.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func normalizeEvent(event Event) Event {
	event.OccurredAt = event.OccurredAt.UTC()
	event.CreatedAt = event.CreatedAt.UTC()
	return event
}

func eventArgs(event Event) []any {
	return []any{
		event.ID,
		string(event.Kind),
		event.DedupeKey,
		nullableString(event.MonitorID),
		event.ProviderID,
		nullableInt64(event.EpisodeID),
		formatTime(event.OccurredAt),
		nullableString(event.TrainNumber),
		nullableString(event.Operator),
		nullableString(event.OriginName),
		nullableString(event.DestinationName),
		nullableTime(event.DepartureAt),
		nullableTime(event.ArrivalAt),
		nullableString(event.WagonName),
		nullableString(event.ClassName),
		nullableInt64(event.PriceAmount),
		nullableString(event.PriceCurrency),
		nullableString(event.BookingURL),
		nullableString(event.ErrorKind),
		nullableInt(event.FailureCount),
		formatTime(event.CreatedAt),
	}
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

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
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

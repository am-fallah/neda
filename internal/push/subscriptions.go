package push

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/am-fallah/neda/internal/crypto"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/store"
)

var (
	// ErrUnavailable reports that Push cannot operate because VAPID keys or a
	// subject are missing, or the encryption key is absent.
	ErrUnavailable = errors.New("push: unavailable")
	// ErrNotFound reports an unknown subscription.
	ErrNotFound = errors.New("push: subscription not found")
	// ErrDisabled reports a subscription that has been unsubscribed or disabled
	// after a 404/410 response.
	ErrDisabled = errors.New("push: subscription is disabled")
)

// SubscriptionInput is one browser PushSubscription registration.
type SubscriptionInput struct {
	Endpoint string
	P256DH   string
	Auth     string
	Label    string
}

// Subscription is the non-secret subscription view. Endpoint and key material
// are never included.
type Subscription struct {
	ID            string
	Label         string
	CreatedAt     time.Time
	LastSuccessAt *time.Time
	FailureCount  int
}

// ConfigView is the authenticated Push configuration payload.
type ConfigView struct {
	Enabled           bool
	Available         bool
	SubjectConfigured bool
	PublicKey         string
	Subscriptions     []Subscription
}

// LoadedSubscription is decrypted material used only by the sender.
type LoadedSubscription struct {
	ID       string
	Endpoint string
	P256DH   string
	Auth     string
}

// Service owns the Push subscription lifecycle. VAPID keys are held by the
// embedded vapid manager; subscription material is encrypted with the master
// key before it reaches SQLite.
type Service struct {
	store          *store.Store
	db             *sql.DB
	key            *crypto.Key
	vapid          *vapid
	now            func() time.Time
	validateStored func(string) error
}

// New validates dependencies. It does not generate keys; call Ensure.
func New(st *store.Store, db *sql.DB, key *crypto.Key, subject string, now func() time.Time) (*Service, error) {
	if st == nil {
		return nil, errors.New("push: nil store")
	}
	if db == nil {
		return nil, errors.New("push: nil database")
	}
	if key == nil {
		return nil, errors.New("push: nil encryption key")
	}
	if now == nil {
		now = time.Now
	}
	vapid, err := newVAPID(st, key, subject, now)
	if err != nil {
		return nil, err
	}
	return &Service{store: st, db: db, key: key, vapid: vapid, now: now, validateStored: ValidateEndpoint}, nil
}

// Ensure loads or creates the VAPID key pair. Startup calls this so corrupted
// or missing key material fails closed.
func (s *Service) Ensure(ctx context.Context) error {
	if s == nil {
		return ErrUnavailable
	}
	return s.vapid.Ensure(ctx)
}

// Available reports whether a subject and usable VAPID keys exist.
func (s *Service) Available() bool {
	return s != nil && s.vapid.available()
}

// Config returns the authenticated configuration and active device list.
func (s *Service) Config(ctx context.Context) (ConfigView, error) {
	if s == nil || s.store == nil {
		return ConfigView{}, ErrUnavailable
	}
	enabled, err := s.store.PushEnabled(ctx)
	if err != nil {
		return ConfigView{}, err
	}
	publicKey, err := s.vapid.PublicKey(ctx)
	if err != nil {
		return ConfigView{}, err
	}
	subscriptions, err := s.List(ctx)
	if err != nil {
		return ConfigView{}, err
	}
	return ConfigView{
		Enabled:           enabled,
		Available:         s.vapid.available(),
		SubjectConfigured: strings.TrimSpace(s.vapid.subjectValue()) != "",
		PublicKey:         publicKey,
		Subscriptions:     subscriptions,
	}, nil
}

// Register validates and stores a subscription. Re-registering the same
// endpoint updates its keys and re-enables it without creating a duplicate.
func (s *Service) Register(ctx context.Context, input SubscriptionInput) (Subscription, error) {
	if s == nil || s.store == nil {
		return Subscription{}, ErrUnavailable
	}
	if _, err := s.vapid.PublicKey(ctx); err != nil {
		return Subscription{}, err
	}
	if err := ValidateEndpoint(input.Endpoint); err != nil {
		return Subscription{}, err
	}
	if err := ValidateKeys(input.P256DH, input.Auth); err != nil {
		return Subscription{}, err
	}
	label, err := ValidateLabel(input.Label)
	if err != nil {
		return Subscription{}, err
	}
	return s.storeSubscription(ctx, strings.TrimSpace(input.Endpoint), strings.TrimSpace(input.P256DH), strings.TrimSpace(input.Auth), label)
}

// storeSubscription persists already-validated material. It is separated so
// package tests can exercise the sender with a local endpoint without
// weakening the production Register validation path.
func (s *Service) storeSubscription(ctx context.Context, endpoint, p256dh, auth, label string) (Subscription, error) {
	endpointHash := hashEndpoint(endpoint)
	id, err := s.existingID(ctx, endpointHash)
	if err != nil {
		return Subscription{}, err
	}
	if id == "" {
		id = uuid.NewString()
	}

	endpointEnc, err := s.key.Encrypt(subscriptionAAD(id, "endpoint"), []byte(endpoint))
	if err != nil {
		return Subscription{}, fmt.Errorf("push: encrypt endpoint: %w", err)
	}
	p256dhEnc, err := s.key.Encrypt(subscriptionAAD(id, "p256dh"), []byte(p256dh))
	if err != nil {
		return Subscription{}, fmt.Errorf("push: encrypt p256dh: %w", err)
	}
	authEnc, err := s.key.Encrypt(subscriptionAAD(id, "auth"), []byte(auth))
	if err != nil {
		return Subscription{}, fmt.Errorf("push: encrypt auth: %w", err)
	}

	now := s.now().UTC()
	if now.IsZero() {
		return Subscription{}, errors.New("push: registration timestamp is required")
	}
	var nullableLabel any
	if label != "" {
		nullableLabel = label
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Subscription{}, fmt.Errorf("push: begin subscription transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO push_subscriptions
	(id, endpoint_hash, endpoint_enc, p256dh_enc, auth_enc, label, created_at, last_success_at, failure_count, disabled_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NULL, 0, NULL)
ON CONFLICT(endpoint_hash) DO UPDATE SET
	endpoint_enc = excluded.endpoint_enc,
	p256dh_enc = excluded.p256dh_enc,
	auth_enc = excluded.auth_enc,
	label = CASE
		WHEN excluded.label IS NULL THEN push_subscriptions.label
		ELSE excluded.label
	END,
	disabled_at = NULL,
	failure_count = 0`,
		id, endpointHash, []byte(endpointEnc), []byte(p256dhEnc), []byte(authEnc),
		nullableLabel, formatPushTime(now),
	); err != nil {
		return Subscription{}, fmt.Errorf("push: store subscription: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Subscription{}, fmt.Errorf("push: commit subscription: %w", err)
	}
	committed = true
	return s.byEndpointHash(ctx, endpointHash)
}

// List returns active subscriptions ordered by creation time.
func (s *Service) List(ctx context.Context) ([]Subscription, error) {
	if s == nil || s.store == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, label, created_at, last_success_at, failure_count
FROM push_subscriptions
WHERE disabled_at IS NULL
ORDER BY created_at, id
LIMIT 200`)
	if err != nil {
		return nil, fmt.Errorf("push: list subscriptions: %w", err)
	}
	defer rows.Close()
	subscriptions := make([]Subscription, 0)
	for rows.Next() {
		subscription, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("push: list subscriptions: %w", err)
	}
	return subscriptions, nil
}

// Disable unsubscribes one device. Disabling an already-disabled subscription
// is idempotent; an unknown ID returns ErrNotFound.
func (s *Service) Disable(ctx context.Context, id string) error {
	if s == nil || s.store == nil {
		return ErrUnavailable
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return &ValidationError{Field: "id", Message: "is required"}
	}
	now := s.now().UTC()
	if now.IsZero() {
		return errors.New("push: disable timestamp is required")
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE push_subscriptions
SET disabled_at = COALESCE(disabled_at, ?)
WHERE id = ?`, formatPushTime(now), id)
	if err != nil {
		return fmt.Errorf("push: disable subscription: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("push: disable subscription rows: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeliveryDestinations returns one Push delivery target per active device. It
// runs inside the caller's outcome transaction and performs reads only.
func (s *Service) DeliveryDestinations(ctx context.Context, tx *sql.Tx) ([]notify.Delivery, error) {
	if s == nil || s.store == nil {
		return nil, ErrUnavailable
	}
	if tx == nil {
		return nil, errors.New("push: nil transaction")
	}
	if !s.vapid.available() {
		return nil, nil
	}
	var enabled string
	err := tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, store.PushEnabledSettingKey).Scan(&enabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("push: read enabled setting: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(enabled), "true") {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM push_subscriptions
WHERE disabled_at IS NULL
ORDER BY created_at, id
LIMIT 200`)
	if err != nil {
		return nil, fmt.Errorf("push: list delivery destinations: %w", err)
	}
	defer rows.Close()
	deliveries := make([]notify.Delivery, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("push: scan delivery destination: %w", err)
		}
		deliveries = append(deliveries, notify.Delivery{
			Channel:   notify.ChannelPush,
			TargetKey: "push:" + id,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("push: list delivery destinations: %w", err)
	}
	return deliveries, nil
}

// Load decrypts one active subscription for the sender. It rejects disabled
// rows and re-validates stored material before any network access.
func (s *Service) Load(ctx context.Context, id string) (LoadedSubscription, error) {
	if s == nil || s.store == nil {
		return LoadedSubscription{}, ErrUnavailable
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return LoadedSubscription{}, ErrNotFound
	}
	var (
		endpointEnc, p256dhEnc, authEnc []byte
		disabledAt                      sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
SELECT endpoint_enc, p256dh_enc, auth_enc, disabled_at
FROM push_subscriptions
WHERE id = ?`, id).Scan(&endpointEnc, &p256dhEnc, &authEnc, &disabledAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LoadedSubscription{}, ErrNotFound
		}
		return LoadedSubscription{}, fmt.Errorf("push: load subscription: %w", err)
	}
	if disabledAt.Valid {
		return LoadedSubscription{}, ErrDisabled
	}
	endpoint, err := s.decrypt(subscriptionAAD(id, "endpoint"), endpointEnc)
	if err != nil {
		return LoadedSubscription{}, err
	}
	p256dh, err := s.decrypt(subscriptionAAD(id, "p256dh"), p256dhEnc)
	if err != nil {
		return LoadedSubscription{}, err
	}
	auth, err := s.decrypt(subscriptionAAD(id, "auth"), authEnc)
	if err != nil {
		return LoadedSubscription{}, err
	}
	validateEndpoint := s.validateStored
	if validateEndpoint == nil {
		validateEndpoint = ValidateEndpoint
	}
	if err := validateEndpoint(endpoint); err != nil {
		return LoadedSubscription{}, fmt.Errorf("push: stored endpoint is invalid: %w", err)
	}
	if err := ValidateKeys(p256dh, auth); err != nil {
		return LoadedSubscription{}, fmt.Errorf("push: stored keys are invalid: %w", err)
	}
	return LoadedSubscription{ID: id, Endpoint: endpoint, P256DH: p256dh, Auth: auth}, nil
}

// MarkSuccess records a successful send and resets the consecutive failure
// count for an active subscription.
func (s *Service) MarkSuccess(ctx context.Context, id string, at time.Time) error {
	if s == nil || s.store == nil {
		return ErrUnavailable
	}
	if at.IsZero() {
		at = s.now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE push_subscriptions
SET last_success_at = ?, failure_count = 0
WHERE id = ? AND disabled_at IS NULL`, formatPushTime(at), id)
	if err != nil {
		return fmt.Errorf("push: mark success: %w", err)
	}
	return nil
}

// MarkFailure increments the failure counter and optionally disables the
// subscription (for a 404/410 response).
func (s *Service) MarkFailure(ctx context.Context, id string, disable bool, at time.Time) error {
	if s == nil || s.store == nil {
		return ErrUnavailable
	}
	if at.IsZero() {
		at = s.now().UTC()
	}
	if disable {
		_, err := s.db.ExecContext(ctx, `
UPDATE push_subscriptions
SET failure_count = failure_count + 1, disabled_at = COALESCE(disabled_at, ?)
WHERE id = ?`, formatPushTime(at), id)
		if err != nil {
			return fmt.Errorf("push: disable failed subscription: %w", err)
		}
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE push_subscriptions
SET failure_count = failure_count + 1
WHERE id = ? AND disabled_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("push: mark failure: %w", err)
	}
	return nil
}

// existingID returns the stable row ID for an endpoint hash so re-registration
// reuses the same encryption AAD instead of creating a value encrypted for a
// different ID.
func (s *Service) existingID(ctx context.Context, hash string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM push_subscriptions WHERE endpoint_hash = ?`, hash).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("push: read existing subscription: %w", err)
	}
	return id, nil
}

func (s *Service) byEndpointHash(ctx context.Context, hash string) (Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, label, created_at, last_success_at, failure_count
FROM push_subscriptions
WHERE endpoint_hash = ? AND disabled_at IS NULL`, hash)
	if err != nil {
		return Subscription{}, fmt.Errorf("push: read subscription: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Subscription{}, fmt.Errorf("push: read subscription: %w", err)
		}
		return Subscription{}, ErrNotFound
	}
	return scanSubscription(rows)
}

func (s *Service) decrypt(aad string, value []byte) (string, error) {
	if len(value) == 0 {
		return "", errors.New("push: encrypted subscription value is empty")
	}
	plaintext, err := s.key.Decrypt(aad, string(value))
	if err != nil {
		return "", fmt.Errorf("push: decrypt subscription value: %w", err)
	}
	return string(plaintext), nil
}

func scanSubscription(scanner interface{ Scan(...any) error }) (Subscription, error) {
	var (
		subscription  Subscription
		label         sql.NullString
		createdAt     string
		lastSuccessAt sql.NullString
	)
	if err := scanner.Scan(&subscription.ID, &label, &createdAt, &lastSuccessAt, &subscription.FailureCount); err != nil {
		return Subscription{}, fmt.Errorf("push: scan subscription: %w", err)
	}
	subscription.Label = label.String
	parsed, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Subscription{}, fmt.Errorf("push: parse created_at: %w", err)
	}
	subscription.CreatedAt = parsed.UTC()
	if lastSuccessAt.Valid {
		value, err := time.Parse(time.RFC3339Nano, lastSuccessAt.String)
		if err != nil {
			return Subscription{}, fmt.Errorf("push: parse last_success_at: %w", err)
		}
		value = value.UTC()
		subscription.LastSuccessAt = &value
	}
	return subscription, nil
}

func hashEndpoint(endpoint string) string {
	sum := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(sum[:])
}

func subscriptionAAD(id, field string) string {
	return "push.subscription." + id + "." + field
}

func formatPushTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

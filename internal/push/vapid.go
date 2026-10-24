package push

import (
	"context"
	"crypto/elliptic"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/am-fallah/neda/internal/crypto"
	"github.com/am-fallah/neda/internal/store"
)

const (
	// VAPIDPrivateKeySetting is the encrypted settings key for the VAPID
	// private key. It is never returned by an API or written to a log.
	VAPIDPrivateKeySetting = "push.vapid_private_key"
	// VAPIDPublicKeySetting is the plaintext settings key for the VAPID public
	// key. It is safe to return to the authenticated browser.
	VAPIDPublicKeySetting = "push.vapid_public_key"
)

var (
	// ErrVAPIDUnavailable reports that VAPID key material is missing,
	// unreadable, or not configured with a subject.
	ErrVAPIDUnavailable = errors.New("push: VAPID is unavailable")
	// ErrVAPIDKeyMismatch reports inconsistent persisted key material.
	ErrVAPIDKeyMismatch = errors.New("push: VAPID key pair is inconsistent")
)

// vapid owns lazy, process-cached VAPID key material. Keys are generated once
// and persisted in SQLite; the private key is encrypted with the master key.
type vapid struct {
	store    *store.Store
	key      *crypto.Key
	subject  string
	now      func() time.Time
	generate func() (string, string, error)

	mu         sync.Mutex
	publicKey  string
	privateKey string
	loaded     bool
}

func newVAPID(st *store.Store, key *crypto.Key, subject string, now func() time.Time) (*vapid, error) {
	if st == nil {
		return nil, errors.New("push: nil store")
	}
	if key == nil {
		return nil, errors.New("push: nil encryption key")
	}
	if now == nil {
		now = time.Now
	}
	return &vapid{
		store:    st,
		key:      key,
		subject:  strings.TrimSpace(subject),
		now:      now,
		generate: webpush.GenerateVAPIDKeys,
	}, nil
}

// Ensure loads existing keys or generates and persists one pair. It must be
// called during startup so a missing/corrupt pair fails closed before serving.
func (v *vapid) Ensure(ctx context.Context) error {
	if v == nil {
		return ErrVAPIDUnavailable
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.ensureLocked(ctx)
}

func (v *vapid) ensureLocked(ctx context.Context) error {
	if v.loaded {
		return nil
	}
	publicSetting, publicErr := v.store.GetSetting(ctx, VAPIDPublicKeySetting)
	privateSetting, privateErr := v.store.GetSetting(ctx, VAPIDPrivateKeySetting)

	switch {
	case errors.Is(publicErr, store.ErrNotFound) && errors.Is(privateErr, store.ErrNotFound):
		return v.generateLocked(ctx)
	case publicErr != nil:
		return fmt.Errorf("%w: public key: %v", ErrVAPIDUnavailable, publicErr)
	case privateErr != nil:
		return fmt.Errorf("%w: private key: %v", ErrVAPIDUnavailable, privateErr)
	}

	publicKey := strings.TrimSpace(publicSetting.Value)
	privateKey, err := v.key.Decrypt(VAPIDPrivateKeySetting, privateSetting.Value)
	if err != nil {
		return fmt.Errorf("%w: private key cannot be decrypted", ErrVAPIDUnavailable)
	}
	if err := validateVAPIDPair(strings.TrimSpace(string(privateKey)), publicKey); err != nil {
		return err
	}
	v.publicKey = publicKey
	v.privateKey = string(privateKey)
	v.loaded = true
	return nil
}

func (v *vapid) generateLocked(ctx context.Context) error {
	privateKey, publicKey, err := v.generate()
	if err != nil {
		return fmt.Errorf("push: generate VAPID keys: %w", err)
	}
	if err := validateVAPIDPair(privateKey, publicKey); err != nil {
		return err
	}
	encrypted, err := v.key.Encrypt(VAPIDPrivateKeySetting, []byte(privateKey))
	if err != nil {
		return fmt.Errorf("push: encrypt VAPID private key: %w", err)
	}
	now := v.now().UTC()
	if now.IsZero() {
		return errors.New("push: VAPID timestamp is required")
	}
	version := int64(v.key.Version())
	err = v.store.WithTx(ctx, func(tx *store.Store) error {
		if err := tx.SetSetting(ctx, store.Setting{
			Key:       VAPIDPublicKeySetting,
			Value:     publicKey,
			UpdatedAt: now,
		}); err != nil {
			return err
		}
		return tx.SetSetting(ctx, store.Setting{
			Key:        VAPIDPrivateKeySetting,
			Value:      encrypted,
			IsSecret:   true,
			KeyVersion: &version,
			UpdatedAt:  now,
		})
	})
	if err != nil {
		return err
	}
	v.publicKey = publicKey
	v.privateKey = privateKey
	v.loaded = true
	return nil
}

// PublicKey returns the persisted VAPID public key after Ensure.
func (v *vapid) PublicKey(ctx context.Context) (string, error) {
	if v == nil {
		return "", ErrVAPIDUnavailable
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.ensureLocked(ctx); err != nil {
		return "", err
	}
	return v.publicKey, nil
}

// privateKeyForSend returns the decrypted private key. It is only called by the
// sender and is never exposed outside this package.
func (v *vapid) privateKeyForSend(ctx context.Context) (privateKey, publicKey string, err error) {
	if v == nil {
		return "", "", ErrVAPIDUnavailable
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.ensureLocked(ctx); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(v.subject) == "" {
		return "", "", fmt.Errorf("%w: subject is not configured", ErrVAPIDUnavailable)
	}
	return v.privateKey, v.publicKey, nil
}

func (v *vapid) available() bool {
	if v == nil {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.loaded && strings.TrimSpace(v.subject) != ""
}

func (v *vapid) subjectValue() string {
	if v == nil {
		return ""
	}
	return v.subject
}

func validateVAPIDPair(privateKey, publicKey string) error {
	privateBytes, err := decodeVAPIDKey(privateKey)
	if err != nil || len(privateBytes) != 32 {
		return fmt.Errorf("%w: private key is invalid", ErrVAPIDKeyMismatch)
	}
	publicBytes, err := decodeVAPIDKey(publicKey)
	if err != nil || len(publicBytes) != 65 || publicBytes[0] != 0x04 {
		return fmt.Errorf("%w: public key is invalid", ErrVAPIDKeyMismatch)
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), publicBytes)
	if x == nil || y == nil {
		return fmt.Errorf("%w: public key is not on P-256", ErrVAPIDKeyMismatch)
	}
	derivedX, derivedY := elliptic.P256().ScalarBaseMult(privateBytes)
	if derivedX.Cmp(x) != 0 || derivedY.Cmp(y) != 0 {
		return fmt.Errorf("%w: public key does not match the private key", ErrVAPIDKeyMismatch)
	}
	return nil
}

func decodeVAPIDKey(raw string) ([]byte, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "=")
	if raw == "" {
		return nil, errors.New("push: empty VAPID key")
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(raw); err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(raw)
}

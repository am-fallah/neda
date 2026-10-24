// Package crypto owns Neda's authenticated encryption for secret settings.
//
// It provides AES-256-GCM with a fresh random nonce per value. The associated
// data binds every ciphertext to the settings key it belongs to, so moving a
// ciphertext to another setting fails authentication. Values are versioned and
// base64 encoded as "v1:<nonce||ciphertext>".
//
// The package never generates or persists a master key. Production keys are
// supplied by the operator through NEDA_ENCRYPTION_KEY_FILE.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	// KeySize is the required master-key length in bytes (AES-256).
	KeySize = 32
	// CurrentKeyVersion is the only encryption-key version implemented today.
	// The settings table stores this value alongside each ciphertext.
	CurrentKeyVersion = 1

	valuePrefix = "v1:"
	nonceSize   = 12
	// maxEncodedValueBytes bounds a single setting value before base64 decode.
	maxEncodedValueBytes = 64 * 1024
	// maxPlaintextBytes keeps the encoded value below maxEncodedValueBytes.
	maxPlaintextBytes = 32 * 1024
)

var (
	// ErrInvalidKey reports a missing or incorrectly sized key.
	ErrInvalidKey = errors.New("crypto: invalid encryption key")
	// ErrMalformedValue reports a value that is not a supported versioned
	// ciphertext. It is deliberately distinct from authentication failure.
	ErrMalformedValue = errors.New("crypto: malformed encrypted value")
	// ErrUnsupportedVersion reports a value written by a future/incompatible
	// key-version format.
	ErrUnsupportedVersion = errors.New("crypto: unsupported encrypted value version")
	// ErrAuthentication reports a failed GCM authentication check. Wrong keys,
	// tampered ciphertext, and ciphertext moved to another setting all map here.
	ErrAuthentication = errors.New("crypto: encrypted value authentication failed")
)

// Key is an AES-256 master key. Its value is never printed or JSON encoded.
type Key struct {
	material [KeySize]byte
	valid    bool
}

// NewKey copies exactly KeySize bytes into a Key. Callers generally use
// LoadKeyFile; NewKey exists for tests and future rotation code.
func NewKey(material []byte) (*Key, error) {
	if len(material) != KeySize {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidKey, KeySize, len(material))
	}
	key := &Key{valid: true}
	copy(key.material[:], material)
	return key, nil
}

// Version reports the key version stored with encrypted settings.
func (k *Key) Version() int {
	if k == nil || !k.valid {
		return 0
	}
	return CurrentKeyVersion
}

// Zero overwrites the in-memory key material. Callers must not use the Key
// after Zero. Go cannot guarantee that copies made by the runtime are erased.
func (k *Key) Zero() {
	if k == nil {
		return
	}
	for i := range k.material {
		k.material[i] = 0
	}
	k.valid = false
}

// Encrypt encrypts plaintext for one settings key. A fresh nonce is generated
// for every call. settingKey is used as associated data and must be stable.
func (k *Key) Encrypt(settingKey string, plaintext []byte) (string, error) {
	aead, err := k.aead()
	if err != nil {
		return "", err
	}
	if err := validateSettingKey(settingKey); err != nil {
		return "", err
	}
	if len(plaintext) > maxPlaintextBytes {
		return "", fmt.Errorf("crypto: plaintext exceeds %d bytes", maxPlaintextBytes)
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: generate nonce: %w", err)
	}
	sealed := aead.Seal(nil, nonce, plaintext, associatedData(settingKey))
	payload := make([]byte, 0, len(nonce)+len(sealed))
	payload = append(payload, nonce...)
	payload = append(payload, sealed...)
	return valuePrefix + base64.StdEncoding.EncodeToString(payload), nil
}

// Decrypt authenticates and decrypts one versioned setting value. It returns
// no plaintext on authentication failure.
func (k *Key) Decrypt(settingKey, encoded string) ([]byte, error) {
	aead, err := k.aead()
	if err != nil {
		return nil, err
	}
	if err := validateSettingKey(settingKey); err != nil {
		return nil, err
	}
	if len(encoded) > maxEncodedValueBytes {
		return nil, fmt.Errorf("%w: value is too large", ErrMalformedValue)
	}
	if !strings.HasPrefix(encoded, valuePrefix) {
		if strings.HasPrefix(encoded, "v") && strings.Contains(encoded, ":") {
			return nil, ErrUnsupportedVersion
		}
		return nil, ErrMalformedValue
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, valuePrefix))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base64", ErrMalformedValue)
	}
	if len(raw) < nonceSize+aead.Overhead() {
		return nil, fmt.Errorf("%w: value is too short", ErrMalformedValue)
	}
	nonce := raw[:nonceSize]
	ciphertext := raw[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, associatedData(settingKey))
	if err != nil {
		return nil, ErrAuthentication
	}
	return plaintext, nil
}

func (k *Key) aead() (cipher.AEAD, error) {
	if k == nil || !k.valid {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(k.material[:])
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return aead, nil
}

func validateSettingKey(settingKey string) error {
	if strings.TrimSpace(settingKey) == "" {
		return errors.New("crypto: setting key must not be empty")
	}
	if len(settingKey) > 256 {
		return errors.New("crypto: setting key is too long")
	}
	for _, r := range settingKey {
		if r == 0 || r < 0x20 || r == 0x7f {
			return errors.New("crypto: setting key must not contain control characters")
		}
	}
	return nil
}

func associatedData(settingKey string) []byte {
	return []byte(valuePrefix + "setting:" + settingKey)
}

// String implements fmt.Stringer without exposing key material.
func (k Key) String() string { return "crypto.Key(redacted)" }

// GoString implements fmt.GoStringer without exposing key material.
func (k Key) GoString() string { return "crypto.Key(redacted)" }

// Format prevents %v, %+v, %x, and %s from printing key bytes.
func (k Key) Format(state fmt.State, verb rune) {
	_, _ = io.WriteString(state, "crypto.Key(redacted)")
}

// MarshalJSON never emits key material.
func (k Key) MarshalJSON() ([]byte, error) {
	return []byte(`"redacted"`), nil
}

// Package auth provides password hashing, sessions, CSRF, origin checks, and
// login rate limiting. It deliberately does not register HTTP auth endpoints;
// the API layer composes these primitives.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	// MinPasswordRunes is the minimum accepted password length.
	MinPasswordRunes = 12
	// MaxPasswordBytes bounds hashing work and request size.
	MaxPasswordBytes = 1024
)

// Params controls Argon2id hashing. Memory is expressed in KiB.
type Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultParams are the documented provisional Argon2id parameters:
// 64 MiB memory, 3 iterations, 2 lanes, 16-byte salt, 32-byte hash.
var DefaultParams = Params{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

func (p Params) validate() error {
	switch {
	case p.Memory < 8*1024 || p.Memory > 256*1024:
		return errors.New("auth: argon2 memory must be between 8192 and 262144 KiB")
	case p.Iterations < 1 || p.Iterations > 10:
		return errors.New("auth: argon2 iterations must be between 1 and 10")
	case p.Parallelism < 1 || p.Parallelism > 8:
		return errors.New("auth: argon2 parallelism must be between 1 and 8")
	case p.SaltLength < 16 || p.SaltLength > 64:
		return errors.New("auth: argon2 salt length must be between 16 and 64 bytes")
	case p.KeyLength < 16 || p.KeyLength > 64:
		return errors.New("auth: argon2 key length must be between 16 and 64 bytes")
	}
	return nil
}

// ValidatePassword enforces the documented password policy without composition
// rules. Surrounding whitespace is preserved; only whitespace-only passwords
// are rejected.
func ValidatePassword(password string) error {
	if !utf8.ValidString(password) {
		return errors.New("auth: password must be valid UTF-8")
	}
	if len(password) > MaxPasswordBytes {
		return fmt.Errorf("auth: password must be at most %d bytes", MaxPasswordBytes)
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("auth: password must not be whitespace only")
	}
	if utf8.RuneCountInString(password) < MinPasswordRunes {
		return fmt.Errorf("auth: password must be at least %d characters", MinPasswordRunes)
	}
	return nil
}

func passwordWithinLoginBounds(password string) bool {
	return utf8.ValidString(password) && len(password) <= MaxPasswordBytes
}

// HashPassword returns a PHC-formatted Argon2id hash including its random salt
// and parameters.
func HashPassword(password string, params Params) (string, error) {
	if err := params.validate(); err != nil {
		return "", err
	}
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		params.Memory,
		params.Iterations,
		params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	return encoded, nil
}

// VerifyPassword checks password against an encoded Argon2id hash. It uses the
// parameters encoded in the hash so parameter changes do not invalidate old
// passwords. A malformed hash returns an error; a mismatch returns false, nil.
func VerifyPassword(password, encoded string) (bool, error) {
	params, salt, want, err := parsePasswordHash(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func parsePasswordHash(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Params{}, nil, nil, errors.New("auth: malformed argon2id hash")
	}
	if parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return Params{}, nil, nil, errors.New("auth: unsupported argon2 version")
	}

	var params Params
	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 {
		return Params{}, nil, nil, errors.New("auth: malformed argon2 parameters")
	}
	seen := make(map[string]bool, 3)
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok || seen[key] {
			return Params{}, nil, nil, errors.New("auth: malformed argon2 parameters")
		}
		seen[key] = true
		switch key {
		case "m":
			parsed, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return Params{}, nil, nil, errors.New("auth: malformed argon2 parameters")
			}
			params.Memory = uint32(parsed)
		case "t":
			parsed, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return Params{}, nil, nil, errors.New("auth: malformed argon2 parameters")
			}
			params.Iterations = uint32(parsed)
		case "p":
			parsed, err := strconv.ParseUint(value, 10, 8)
			if err != nil {
				return Params{}, nil, nil, errors.New("auth: malformed argon2 parameters")
			}
			params.Parallelism = uint8(parsed)
		default:
			return Params{}, nil, nil, errors.New("auth: unknown argon2 parameter")
		}
	}
	if !seen["m"] || !seen["t"] || !seen["p"] {
		return Params{}, nil, nil, errors.New("auth: malformed argon2 parameters")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, errors.New("auth: malformed argon2 salt")
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, errors.New("auth: malformed argon2 hash")
	}
	params.SaltLength = uint32(len(salt))
	params.KeyLength = uint32(len(hash))
	if err := params.validate(); err != nil {
		return Params{}, nil, nil, err
	}
	return params, salt, hash, nil
}

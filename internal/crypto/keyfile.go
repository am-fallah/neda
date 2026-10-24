package crypto

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const keyFileMaxBytes = 128

// LoadKeyFile reads and validates a master-key file. The file must be a
// regular, non-symlink file owned by the runtime user with mode 0400 or 0600
// and contain base64 text that decodes to exactly 32 bytes.
//
// Errors never include file contents or decoded key material.
func LoadKeyFile(path string) (*Key, error) {
	out, err := loadKeyFileBytes(path)
	if err != nil {
		return nil, err
	}
	defer zero(out)
	return NewKey(out)
}

// ValidateKeyFile performs the same checks as LoadKeyFile and discards the
// decoded key. It exists for startup configuration validation.
func ValidateKeyFile(path string) error {
	key, err := LoadKeyFile(path)
	if err != nil {
		return err
	}
	key.Zero()
	return nil
}

func loadKeyFileBytes(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("key file path is empty")
	}
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("key file unavailable: %w", err)
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("key file must not be a symbolic link")
	}
	if !linkInfo.Mode().IsRegular() {
		return nil, errors.New("key file must be a regular file")
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("key file unavailable: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("key file unavailable: %w", err)
	}
	if !os.SameFile(linkInfo, info) {
		return nil, errors.New("key file changed while it was being opened")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("key file must be a regular file")
	}
	permissions := info.Mode().Perm()
	if permissions != 0o400 && permissions != 0o600 {
		return nil, errors.New("key file permissions must be owner-only (0400 or 0600)")
	}
	if err := checkKeyFileOwnership(info); err != nil {
		return nil, err
	}

	contents, err := io.ReadAll(io.LimitReader(file, keyFileMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("key file unreadable: %w", err)
	}
	if len(contents) > keyFileMaxBytes {
		return nil, fmt.Errorf("key file is too large (maximum %d bytes)", keyFileMaxBytes)
	}

	trimmed := strings.TrimSpace(string(contents))
	if trimmed == "" {
		return nil, errors.New("key file is empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, errors.New("key file contents must be base64")
	}
	if len(decoded) != KeySize {
		return nil, fmt.Errorf("decoded key must be exactly %d bytes, got %d", KeySize, len(decoded))
	}
	return decoded, nil
}

func zero(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

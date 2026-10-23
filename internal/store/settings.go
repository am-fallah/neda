package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Setting is one durable settings row. Secret values are stored as supplied;
// encryption belongs to the component that owns the secret, not this layer.
type Setting struct {
	Key        string
	Value      string
	IsSecret   bool
	KeyVersion *int64
	UpdatedAt  time.Time
}

const getSettingQuery = `SELECT key, value, is_secret, key_version, updated_at FROM settings WHERE key = ?`

const setSettingQuery = `INSERT INTO settings (key, value, is_secret, key_version, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
	value = excluded.value,
	is_secret = excluded.is_secret,
	key_version = excluded.key_version,
	updated_at = excluded.updated_at`

const deleteSettingQuery = `DELETE FROM settings WHERE key = ?`

// GetSetting returns one setting by key.
func (s *Store) GetSetting(ctx context.Context, key string) (Setting, error) {
	db, err := s.executor()
	if err != nil {
		return Setting{}, err
	}
	if strings.TrimSpace(key) == "" {
		return Setting{}, errors.New("store: setting key must not be empty")
	}

	var setting Setting
	var isSecret int
	var keyVersion sql.NullInt64
	var updatedAt string
	err = db.QueryRowContext(ctx, getSettingQuery, key).Scan(
		&setting.Key,
		&setting.Value,
		&isSecret,
		&keyVersion,
		&updatedAt,
	)
	if err != nil {
		return Setting{}, wrap("get setting", err)
	}
	if isSecret != 0 && isSecret != 1 {
		return Setting{}, fmt.Errorf("store: setting %q has invalid is_secret value", key)
	}
	setting.IsSecret = isSecret == 1
	if keyVersion.Valid {
		value := keyVersion.Int64
		setting.KeyVersion = &value
	}
	setting.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Setting{}, err
	}
	return setting, nil
}

// SetSetting inserts or updates a setting.
func (s *Store) SetSetting(ctx context.Context, setting Setting) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	if strings.TrimSpace(setting.Key) == "" {
		return errors.New("store: setting key must not be empty")
	}
	if setting.UpdatedAt.IsZero() {
		return errors.New("store: setting updated_at must be set")
	}
	if setting.KeyVersion != nil && *setting.KeyVersion < 1 {
		return errors.New("store: setting key_version must be positive")
	}

	var keyVersion any
	if setting.KeyVersion != nil {
		keyVersion = *setting.KeyVersion
	}
	if _, err := db.ExecContext(ctx, setSettingQuery,
		setting.Key,
		setting.Value,
		boolToInt(setting.IsSecret),
		keyVersion,
		formatTime(setting.UpdatedAt),
	); err != nil {
		return wrap("set setting", err)
	}
	return nil
}

// DeleteSetting removes a setting. Deleting a missing key returns ErrNotFound.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("store: setting key must not be empty")
	}
	result, err := db.ExecContext(ctx, deleteSettingQuery, key)
	if err != nil {
		return wrap("delete setting", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete setting rows: %w", err)
	}
	if deleted == 0 {
		return ErrNotFound
	}
	return nil
}

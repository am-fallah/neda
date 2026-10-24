package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/domain"
)

// PushEnabledSettingKey is the plain SQLite settings key for the global Push
// channel switch. Missing rows mean disabled.
const PushEnabledSettingKey = "push_enabled"

// EnsurePushEnabled seeds the disabled default without overwriting an
// operator-set value.
func (s *Store) EnsurePushEnabled(ctx context.Context, now time.Time) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	if now.IsZero() {
		return errors.New("store: push enabled timestamp is required")
	}
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO settings (key, value, is_secret, updated_at) VALUES (?, 'false', 0, ?)`,
		PushEnabledSettingKey, formatTime(now),
	); err != nil {
		return wrap("seed push enabled", err)
	}
	return nil
}

// PushEnabled reads the global Push channel switch. A missing row is false.
func (s *Store) PushEnabled(ctx context.Context) (bool, error) {
	setting, err := s.GetSetting(ctx, PushEnabledSettingKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(setting.Value))
	if err != nil {
		return false, fmt.Errorf("store: push enabled value %q is not a boolean", setting.Value)
	}
	return enabled, nil
}

// SetPushEnabled upserts the global Push channel switch.
func (s *Store) SetPushEnabled(ctx context.Context, enabled bool, now time.Time) error {
	if now.IsZero() {
		return errors.New("store: push enabled timestamp is required")
	}
	return s.SetSetting(ctx, Setting{
		Key:       PushEnabledSettingKey,
		Value:     strconv.FormatBool(enabled),
		UpdatedAt: now,
	})
}

// SetPollInterval validates and upserts the global polling interval as whole
// seconds. The scheduler reads the value through PollInterval.
func (s *Store) SetPollInterval(ctx context.Context, interval time.Duration, now time.Time) error {
	if interval%time.Second != 0 {
		return errors.New("store: poll interval must be whole seconds")
	}
	if err := domain.ValidatePollInterval(interval); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if now.IsZero() {
		return errors.New("store: poll interval timestamp is required")
	}
	return s.SetSetting(ctx, Setting{
		Key:       PollIntervalSettingKey,
		Value:     strconv.Itoa(int(interval / time.Second)),
		UpdatedAt: now,
	})
}

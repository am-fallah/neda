package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

const (
	// UnavailableNotifyAfterSettingKey stores whole minutes between a confirmed
	// unavailable streak starting and its single silent notification.
	UnavailableNotifyAfterSettingKey = "unavailable_notify_after_minutes"
	// DefaultUnavailableNotifyAfter is the approved three-hour default.
	DefaultUnavailableNotifyAfter = 3 * time.Hour
	// MinUnavailableNotifyAfter and MaxUnavailableNotifyAfter bound the grace
	// period to 1–24 hours.
	MinUnavailableNotifyAfter = time.Hour
	MaxUnavailableNotifyAfter = 24 * time.Hour
)

// EnsureUnavailableNotifyAfter seeds the default grace period without
// overwriting an operator-set value.
func (s *Store) EnsureUnavailableNotifyAfter(ctx context.Context, now time.Time) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	if now.IsZero() {
		return errors.New("store: unavailable notify timestamp is required")
	}
	minutes := int(DefaultUnavailableNotifyAfter / time.Minute)
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO settings (key, value, is_secret, updated_at) VALUES (?, ?, 0, ?)`,
		UnavailableNotifyAfterSettingKey, strconv.Itoa(minutes), formatTime(now),
	); err != nil {
		return wrap("seed unavailable notify grace", err)
	}
	return nil
}

// UnavailableNotifyAfter reads the configured grace period. A missing row uses
// the documented default.
func (s *Store) UnavailableNotifyAfter(ctx context.Context) (time.Duration, error) {
	setting, err := s.GetSetting(ctx, UnavailableNotifyAfterSettingKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return DefaultUnavailableNotifyAfter, nil
		}
		return 0, err
	}
	minutes, err := strconv.Atoi(setting.Value)
	if err != nil {
		return 0, fmt.Errorf("store: unavailable notify grace %q is not an integer number of minutes", setting.Value)
	}
	grace := time.Duration(minutes) * time.Minute
	if err := ValidateUnavailableNotifyAfter(grace); err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	return grace, nil
}

// SetUnavailableNotifyAfter validates and upserts the grace period.
func (s *Store) SetUnavailableNotifyAfter(ctx context.Context, grace time.Duration, now time.Time) error {
	if err := ValidateUnavailableNotifyAfter(grace); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if grace%time.Minute != 0 {
		return errors.New("store: unavailable notify grace must be whole minutes")
	}
	if now.IsZero() {
		return errors.New("store: unavailable notify timestamp is required")
	}
	return s.SetSetting(ctx, Setting{
		Key:       UnavailableNotifyAfterSettingKey,
		Value:     strconv.Itoa(int(grace / time.Minute)),
		UpdatedAt: now,
	})
}

// ValidateUnavailableNotifyAfter enforces the 1–24 hour range.
func ValidateUnavailableNotifyAfter(grace time.Duration) error {
	if grace < MinUnavailableNotifyAfter || grace > MaxUnavailableNotifyAfter {
		return fmt.Errorf("unavailable notify grace must be between %s and %s",
			MinUnavailableNotifyAfter, MaxUnavailableNotifyAfter)
	}
	return nil
}

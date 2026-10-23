package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/am-fallah/neda/internal/domain"
)

// PollIntervalSettingKey is the plain SQLite settings key for the global
// polling interval. The value is stored as whole seconds.
const PollIntervalSettingKey = "poll_interval_seconds"

// EnsurePollInterval seeds the default interval without overwriting an
// operator-set value.
func (s *Store) EnsurePollInterval(ctx context.Context, now time.Time) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	seconds := int(domain.DefaultPollInterval / time.Second)
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO settings (key, value, is_secret, updated_at) VALUES (?, ?, 0, ?)`,
		PollIntervalSettingKey, strconv.Itoa(seconds), formatTime(now),
	); err != nil {
		return wrap("seed poll interval", err)
	}
	return nil
}

// PollInterval reads and validates the persisted global polling interval.
func (s *Store) PollInterval(ctx context.Context) (time.Duration, error) {
	setting, err := s.GetSetting(ctx, PollIntervalSettingKey)
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.Atoi(setting.Value)
	if err != nil {
		return 0, fmt.Errorf("store: poll interval %q is not an integer number of seconds", setting.Value)
	}
	if seconds < int(domain.MinPollInterval/time.Second) || seconds > int(domain.MaxPollInterval/time.Second) {
		return 0, fmt.Errorf("store: poll interval %d seconds is outside %d–%d",
			seconds, int(domain.MinPollInterval/time.Second), int(domain.MaxPollInterval/time.Second))
	}
	interval := time.Duration(seconds) * time.Second
	if err := domain.ValidatePollInterval(interval); err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	return interval, nil
}

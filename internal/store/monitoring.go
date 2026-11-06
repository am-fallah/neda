package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MonitoringEnabledSettingKey is the plain SQLite settings key for the global
// monitoring switch. A missing row means enabled so an upgraded database keeps
// its previous behavior.
const MonitoringEnabledSettingKey = "monitoring_enabled"

// EnsureMonitoringEnabled seeds the enabled default without overwriting an
// operator-set value.
func (s *Store) EnsureMonitoringEnabled(ctx context.Context, now time.Time) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	if now.IsZero() {
		return errors.New("store: monitoring enabled timestamp is required")
	}
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO settings (key, value, is_secret, updated_at) VALUES (?, 'true', 0, ?)`,
		MonitoringEnabledSettingKey, formatTime(now),
	); err != nil {
		return wrap("seed monitoring enabled", err)
	}
	return nil
}

// MonitoringEnabled reads the global monitoring switch. A missing row is
// enabled for backward compatibility with databases created before the switch.
func (s *Store) MonitoringEnabled(ctx context.Context) (bool, error) {
	setting, err := s.GetSetting(ctx, MonitoringEnabledSettingKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return true, nil
		}
		return false, err
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(setting.Value))
	if err != nil {
		return false, fmt.Errorf("store: monitoring enabled value %q is not a boolean", setting.Value)
	}
	return enabled, nil
}

// SetMonitoringEnabled upserts the global monitoring switch.
func (s *Store) SetMonitoringEnabled(ctx context.Context, enabled bool, now time.Time) error {
	if now.IsZero() {
		return errors.New("store: monitoring enabled timestamp is required")
	}
	return s.SetSetting(ctx, Setting{
		Key:       MonitoringEnabledSettingKey,
		Value:     strconv.FormatBool(enabled),
		UpdatedAt: now,
	})
}

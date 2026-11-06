package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/store"
)

func TestMonitoringEnabledDefaultsToEnabledAndRoundTrips(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	enabled, err := st.MonitoringEnabled(ctx)
	if err != nil {
		t.Fatalf("MonitoringEnabled default: %v", err)
	}
	if !enabled {
		t.Fatal("MonitoringEnabled default = false, want true")
	}
	if err := st.EnsureMonitoringEnabled(ctx, testNow); err != nil {
		t.Fatalf("EnsureMonitoringEnabled: %v", err)
	}
	setting, err := st.GetSetting(ctx, store.MonitoringEnabledSettingKey)
	if err != nil {
		t.Fatalf("GetSetting seeded monitoring: %v", err)
	}
	if setting.IsSecret || setting.Value != "true" {
		t.Fatalf("seeded monitoring setting = %+v", setting)
	}

	if err := st.SetMonitoringEnabled(ctx, false, testNow); err != nil {
		t.Fatalf("SetMonitoringEnabled false: %v", err)
	}
	enabled, err = st.MonitoringEnabled(ctx)
	if err != nil || enabled {
		t.Fatalf("MonitoringEnabled after false = %v, %v", enabled, err)
	}

	// Seeding never overwrites an explicit operator choice.
	if err := st.EnsureMonitoringEnabled(ctx, testNow.Add(time.Minute)); err != nil {
		t.Fatalf("EnsureMonitoringEnabled existing: %v", err)
	}
	enabled, err = st.MonitoringEnabled(ctx)
	if err != nil || enabled {
		t.Fatalf("MonitoringEnabled after reseed = %v, %v", enabled, err)
	}

	if err := st.SetMonitoringEnabled(ctx, true, testNow.Add(time.Minute)); err != nil {
		t.Fatalf("SetMonitoringEnabled true: %v", err)
	}
	enabled, err = st.MonitoringEnabled(ctx)
	if err != nil || !enabled {
		t.Fatalf("MonitoringEnabled after true = %v, %v", enabled, err)
	}
	if err := st.SetMonitoringEnabled(ctx, false, time.Time{}); err == nil {
		t.Fatal("SetMonitoringEnabled accepted a zero timestamp")
	}
	if err := st.SetSetting(ctx, store.Setting{
		Key: store.MonitoringEnabledSettingKey, Value: "not-a-bool", UpdatedAt: testNow,
	}); err != nil {
		t.Fatalf("corrupt monitoring setting: %v", err)
	}
	if _, err := st.MonitoringEnabled(ctx); err == nil {
		t.Fatal("MonitoringEnabled accepted a non-boolean value")
	}
}

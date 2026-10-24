package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/store"
)

func TestPushEnabledDefaultsAndRoundTrip(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	enabled, err := st.PushEnabled(ctx)
	if err != nil {
		t.Fatalf("PushEnabled default: %v", err)
	}
	if enabled {
		t.Fatal("PushEnabled default = true, want false")
	}
	if err := st.EnsurePushEnabled(ctx, testNow); err != nil {
		t.Fatalf("EnsurePushEnabled: %v", err)
	}
	setting, err := st.GetSetting(ctx, store.PushEnabledSettingKey)
	if err != nil {
		t.Fatalf("GetSetting seeded push: %v", err)
	}
	if setting.Value != "false" {
		t.Fatalf("seeded push value = %q, want false", setting.Value)
	}
	if err := st.SetPushEnabled(ctx, true, testNow); err != nil {
		t.Fatalf("SetPushEnabled true: %v", err)
	}
	if err := st.EnsurePushEnabled(ctx, testNow.Add(time.Minute)); err != nil {
		t.Fatalf("EnsurePushEnabled existing: %v", err)
	}
	enabled, err = st.PushEnabled(ctx)
	if err != nil || !enabled {
		t.Fatalf("PushEnabled after true = %v, %v", enabled, err)
	}
	setting, err = st.GetSetting(ctx, store.PushEnabledSettingKey)
	if err != nil {
		t.Fatalf("GetSetting push: %v", err)
	}
	if setting.IsSecret || setting.Value != "true" {
		t.Fatalf("push setting = %+v", setting)
	}
	if err := st.SetPushEnabled(ctx, false, testNow.Add(time.Minute)); err != nil {
		t.Fatalf("SetPushEnabled false: %v", err)
	}
	enabled, err = st.PushEnabled(ctx)
	if err != nil || enabled {
		t.Fatalf("PushEnabled after false = %v, %v", enabled, err)
	}
	if err := st.SetPushEnabled(ctx, true, time.Time{}); err == nil {
		t.Fatal("SetPushEnabled with zero timestamp succeeded")
	}
	if err := st.SetSetting(ctx, store.Setting{
		Key: store.PushEnabledSettingKey, Value: "not-a-bool", UpdatedAt: testNow,
	}); err != nil {
		t.Fatalf("corrupt push setting: %v", err)
	}
	if _, err := st.PushEnabled(ctx); err == nil {
		t.Fatal("PushEnabled accepted a non-boolean value")
	}
}

func TestSetPollIntervalValidationAndRoundTrip(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	if _, err := st.PollInterval(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("PollInterval before seed error = %v, want ErrNotFound", err)
	}
	if err := st.SetPollInterval(ctx, 2*time.Minute, testNow); err != nil {
		t.Fatalf("SetPollInterval: %v", err)
	}
	interval, err := st.PollInterval(ctx)
	if err != nil {
		t.Fatalf("PollInterval: %v", err)
	}
	if interval != 2*time.Minute {
		t.Fatalf("interval = %v, want 2m", interval)
	}
	setting, err := st.GetSetting(ctx, store.PollIntervalSettingKey)
	if err != nil {
		t.Fatalf("GetSetting poll: %v", err)
	}
	if setting.Value != "120" || setting.IsSecret {
		t.Fatalf("poll setting = %+v", setting)
	}
	for _, invalid := range []time.Duration{
		domain.MinPollInterval - time.Second,
		domain.MaxPollInterval + time.Second,
		1500 * time.Millisecond,
	} {
		if err := st.SetPollInterval(ctx, invalid, testNow); err == nil {
			t.Fatalf("SetPollInterval accepted %v", invalid)
		}
	}
	if err := st.SetPollInterval(ctx, time.Minute, time.Time{}); err == nil {
		t.Fatal("SetPollInterval accepted zero timestamp")
	}
}

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/store"
)

func TestUnavailableNotifyAfterDefaultsAndRoundTrip(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	grace, err := st.UnavailableNotifyAfter(ctx)
	if err != nil {
		t.Fatalf("UnavailableNotifyAfter default: %v", err)
	}
	if grace != 3*time.Hour {
		t.Fatalf("default grace = %v, want 3h", grace)
	}
	if err := st.EnsureUnavailableNotifyAfter(ctx, testNow); err != nil {
		t.Fatalf("EnsureUnavailableNotifyAfter: %v", err)
	}
	setting, err := st.GetSetting(ctx, store.UnavailableNotifyAfterSettingKey)
	if err != nil {
		t.Fatalf("GetSetting seeded grace: %v", err)
	}
	if setting.Value != "180" || setting.IsSecret {
		t.Fatalf("seeded grace = %+v", setting)
	}

	if err := st.SetUnavailableNotifyAfter(ctx, 90*time.Minute, testNow); err != nil {
		t.Fatalf("SetUnavailableNotifyAfter: %v", err)
	}
	grace, err = st.UnavailableNotifyAfter(ctx)
	if err != nil || grace != 90*time.Minute {
		t.Fatalf("stored grace = %v, %v; want 90m", grace, err)
	}
	for _, invalid := range []time.Duration{
		0,
		59 * time.Minute,
		25 * time.Hour,
		90 * time.Second,
	} {
		if err := st.SetUnavailableNotifyAfter(ctx, invalid, testNow); err == nil {
			t.Fatalf("SetUnavailableNotifyAfter accepted %v", invalid)
		}
	}
	if err := st.SetUnavailableNotifyAfter(ctx, time.Hour, time.Time{}); err == nil {
		t.Fatal("SetUnavailableNotifyAfter accepted a zero timestamp")
	}
}

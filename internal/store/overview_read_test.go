package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/store"
)

func TestCountMonitorsByStatus(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	future := testNow.Add(24 * time.Hour)
	createScheduleMonitor(t, st, "active", true, future, nil,
		scheduleTargetSpec{providerID: "p-active", active: true})
	createScheduleMonitor(t, st, "paused", false, future, nil,
		scheduleTargetSpec{providerID: "p-paused", active: true})
	createScheduleMonitor(t, st, "expired-at", true, future, timePtr(testNow),
		scheduleTargetSpec{providerID: "p-expired-at", active: true})
	createScheduleMonitor(t, st, "expired-by", true, testNow, nil,
		scheduleTargetSpec{providerID: "p-expired-by", active: true})

	counts, err := st.CountMonitorsByStatus(ctx, testNow)
	if err != nil {
		t.Fatalf("CountMonitorsByStatus: %v", err)
	}
	if counts.Total != 4 || counts.Active != 1 || counts.Paused != 1 || counts.Expired != 2 {
		t.Fatalf("monitor counts = %+v", counts)
	}

	emptyStore, _ := newTestStore(t)
	empty, err := emptyStore.CountMonitorsByStatus(ctx, testNow)
	if err != nil {
		t.Fatalf("CountMonitorsByStatus empty: %v", err)
	}
	if empty != (store.MonitorStatusCounts{}) {
		t.Fatalf("empty counts = %+v", empty)
	}
	if _, err := st.CountMonitorsByStatus(ctx, time.Time{}); err == nil {
		t.Fatal("CountMonitorsByStatus accepted zero time")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := st.CountMonitorsByStatus(canceled, testNow); err == nil {
		t.Fatal("CountMonitorsByStatus with canceled context succeeded")
	}
}

func TestCountDeliveriesByStatus(t *testing.T) {
	st, database := newTestStore(t)
	ctx := context.Background()
	empty, err := st.CountDeliveriesByStatus(ctx)
	if err != nil {
		t.Fatalf("CountDeliveriesByStatus empty: %v", err)
	}
	if empty != (store.DeliveryStatusCounts{}) {
		t.Fatalf("empty delivery counts = %+v", empty)
	}

	failureCount := 3
	insertReadEvent(t, database, readEventInput{
		id: "evt-counts", kind: notify.KindProviderDegraded, providerID: "p-counts",
		occurredAt: testNow, errorKind: "timeout", failureCount: &failureCount,
	})
	next := testNow.Add(time.Minute)
	last := testNow
	sent := testNow
	statuses := []struct {
		id        string
		channel   notify.Channel
		targetKey string
		status    store.DeliveryStatus
		attempts  int
		next      *time.Time
		last      *time.Time
		sent      *time.Time
	}{
		{"d-pending", notify.ChannelTelegram, "telegram", store.DeliveryPending, 0, &next, nil, nil},
		{"d-sending", notify.ChannelPush, "push:sending", store.DeliverySending, 1, nil, &last, nil},
		{"d-sent", notify.ChannelPush, "push:sent", store.DeliverySent, 1, nil, &last, &sent},
		{"d-retry", notify.ChannelPush, "push:retry", store.DeliveryRetryScheduled, 2, &next, &last, nil},
		{"d-failed", notify.ChannelPush, "push:failed", store.DeliveryFailed, 5, nil, &last, nil},
		{"d-skipped", notify.ChannelPush, "push:skipped", store.DeliverySkipped, 0, nil, nil, nil},
	}
	for _, fixture := range statuses {
		insertReadDelivery(t, database, fixture.id, "evt-counts", fixture.channel,
			fixture.targetKey, fixture.status, fixture.attempts, fixture.next,
			fixture.last, fixture.sent, "", testNow, testNow)
	}
	counts, err := st.CountDeliveriesByStatus(ctx)
	if err != nil {
		t.Fatalf("CountDeliveriesByStatus: %v", err)
	}
	want := store.DeliveryStatusCounts{
		Total: 6, Pending: 1, Sending: 1, Sent: 1,
		RetryScheduled: 1, Failed: 1, Skipped: 1,
	}
	if counts != want {
		t.Fatalf("delivery counts = %+v, want %+v", counts, want)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := st.CountDeliveriesByStatus(canceled); err == nil {
		t.Fatal("CountDeliveriesByStatus with canceled context succeeded")
	}
}

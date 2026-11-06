package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/store"
)

func TestDailyReportTimeDefaultsAndValidation(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	value, err := st.DailyReportTime(ctx)
	if err != nil {
		t.Fatalf("DailyReportTime default: %v", err)
	}
	if value != "00:05" {
		t.Fatalf("default report time = %q, want 00:05", value)
	}
	if err := st.EnsureDailyReportTime(ctx, testNow); err != nil {
		t.Fatalf("EnsureDailyReportTime: %v", err)
	}
	setting, err := st.GetSetting(ctx, store.DailyReportTimeSettingKey)
	if err != nil {
		t.Fatalf("GetSetting seeded report time: %v", err)
	}
	if setting.Value != "00:05" || setting.IsSecret {
		t.Fatalf("seeded report time = %+v", setting)
	}
	if err := st.SetDailyReportTime(ctx, "06:30", testNow); err != nil {
		t.Fatalf("SetDailyReportTime: %v", err)
	}
	value, err = st.DailyReportTime(ctx)
	if err != nil || value != "06:30" {
		t.Fatalf("stored report time = %q, %v", value, err)
	}
	for _, invalid := range []string{"", "6:30", "24:00", "23:60", "noon", "00:5"} {
		if err := st.SetDailyReportTime(ctx, invalid, testNow); err == nil {
			t.Fatalf("SetDailyReportTime accepted %q", invalid)
		}
	}
	if err := st.SetDailyReportTime(ctx, "00:05", time.Time{}); err == nil {
		t.Fatal("SetDailyReportTime accepted a zero timestamp")
	}
}

func TestDailyReportClaimAndCompleteLifecycle(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	now := testNow
	reportDate := "2026-10-23"

	record, created, err := st.EnsureDailyReport(ctx, reportDate, now)
	if err != nil {
		t.Fatalf("EnsureDailyReport: %v", err)
	}
	if !created || record.Status != store.DailyReportPending || record.Attempts != 0 {
		t.Fatalf("ensured report = %+v created=%v", record, created)
	}
	again, created, err := st.EnsureDailyReport(ctx, reportDate, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("EnsureDailyReport existing: %v", err)
	}
	if created || again.Status != store.DailyReportPending {
		t.Fatalf("duplicate ensure = %+v created=%v", again, created)
	}

	claimed, ok, err := st.ClaimDailyReport(ctx, reportDate, now, 10*time.Minute)
	if err != nil {
		t.Fatalf("ClaimDailyReport: %v", err)
	}
	if !ok || claimed.Status != store.DailyReportSending || claimed.Attempts != 1 ||
		claimed.LeaseUntil == nil || !claimed.LeaseUntil.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("claimed = %+v ok=%v", claimed, ok)
	}
	if _, ok, err := st.ClaimDailyReport(ctx, reportDate, now.Add(time.Minute), 10*time.Minute); err != nil || ok {
		t.Fatalf("second claim ok=%v err=%v, want lease-held", ok, err)
	}

	messageID := int64(42)
	sentAt := now.Add(2 * time.Minute)
	if err := st.CompleteDailyReport(ctx, reportDate, sentAt, store.DailyReportCompletion{
		Status:    store.DailyReportSent,
		SentAt:    &sentAt,
		MessageID: &messageID,
	}); err != nil {
		t.Fatalf("CompleteDailyReport sent: %v", err)
	}
	sent, err := st.GetDailyReport(ctx, reportDate)
	if err != nil || sent.Status != store.DailyReportSent || sent.SentAt == nil ||
		sent.MessageID == nil || *sent.MessageID != 42 || sent.LeaseUntil != nil {
		t.Fatalf("sent report = %+v, %v", sent, err)
	}
	if _, ok, err := st.ClaimDailyReport(ctx, reportDate, now.Add(time.Hour), 10*time.Minute); err != nil || ok {
		t.Fatalf("claim after sent ok=%v err=%v", ok, err)
	}
}

func TestDailyReportExpiredLeaseBecomesUncertain(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	now := testNow
	reportDate := "2026-10-22"

	if _, _, err := st.EnsureDailyReport(ctx, reportDate, now); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.ClaimDailyReport(ctx, reportDate, now, time.Minute); err != nil || !ok {
		t.Fatalf("initial claim ok=%v err=%v", ok, err)
	}
	record, ok, err := st.ClaimDailyReport(ctx, reportDate, now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("recover claim: %v", err)
	}
	if ok || record.Status != store.DailyReportUncertain || record.LastErrorKind != "lease_expired" {
		t.Fatalf("recovered = %+v ok=%v, want uncertain/lease_expired", record, ok)
	}
	if _, ok, err := st.ClaimDailyReport(ctx, reportDate, now.Add(time.Hour), time.Minute); err != nil || ok {
		t.Fatalf("uncertain report was reclaimed: ok=%v err=%v", ok, err)
	}
}

func TestTerminalDailyReportFailureIsNotRetried(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	now := testNow
	date := "2026-10-20"
	if _, _, err := st.EnsureDailyReport(ctx, date, now); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.ClaimDailyReport(ctx, date, now, time.Minute); err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if err := st.CompleteDailyReport(ctx, date, now, store.DailyReportCompletion{
		Status:        store.DailyReportFailed,
		LastErrorKind: "telegram_permanent",
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.ClaimDailyReport(ctx, date, now.Add(time.Hour), time.Minute); err != nil || ok {
		t.Fatalf("terminal failed report reclaimed: ok=%v err=%v", ok, err)
	}
}

func TestDailyReportRetryAndLatest(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	now := testNow
	first := "2026-10-21"
	second := "2026-10-22"

	if _, _, err := st.EnsureDailyReport(ctx, first, now); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.ClaimDailyReport(ctx, first, now, time.Minute); err != nil || !ok {
		t.Fatalf("claim first ok=%v err=%v", ok, err)
	}
	next := now.Add(5 * time.Minute)
	if err := st.CompleteDailyReport(ctx, first, now, store.DailyReportCompletion{
		Status:        store.DailyReportPending,
		LastErrorKind: "telegram_transient",
		NextAttemptAt: &next,
	}); err != nil {
		t.Fatalf("retry complete: %v", err)
	}
	if _, ok, err := st.ClaimDailyReport(ctx, first, now.Add(time.Minute), time.Minute); err != nil || ok {
		t.Fatalf("early retry ok=%v err=%v, want delayed", ok, err)
	}
	if record, ok, err := st.ClaimDailyReport(ctx, first, next, time.Minute); err != nil || !ok || record.Attempts != 2 {
		t.Fatalf("due retry = %+v ok=%v err=%v", record, ok, err)
	}
	if _, _, err := st.EnsureDailyReport(ctx, second, now); err != nil {
		t.Fatal(err)
	}
	latest, err := st.LatestDailyReport(ctx)
	if err != nil || latest.ReportDate != second {
		t.Fatalf("latest = %+v, %v", latest, err)
	}
	if _, err := st.GetDailyReport(ctx, "2026-10-01"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing report error = %v", err)
	}
	if _, _, err := st.EnsureDailyReport(ctx, "2026-10-01", time.Time{}); err == nil {
		t.Fatal("EnsureDailyReport accepted a zero timestamp")
	}
}

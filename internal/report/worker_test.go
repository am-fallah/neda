package report_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
	"github.com/am-fallah/neda/internal/report"
	"github.com/am-fallah/neda/internal/store"
)

type fakeReportSender struct {
	mu      sync.Mutex
	texts   []string
	results []error
	calls   int
}

func (s *fakeReportSender) Send(_ context.Context, text string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.texts = append(s.texts, text)
	if len(s.results) == 0 {
		return int64(s.calls), nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	if result != nil {
		return 0, result
	}
	return int64(s.calls), nil
}

func (s *fakeReportSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *fakeReportSender) lastText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.texts) == 0 {
		return ""
	}
	return s.texts[len(s.texts)-1]
}

func reportWorker(t *testing.T, st *store.Store, database *sql.DB, sender report.Sender) *report.Worker {
	t.Helper()
	worker, err := report.New(report.Config{
		DB:           database,
		Store:        st,
		Sender:       sender,
		Location:     tehran,
		PollInterval: time.Minute,
		Lease:        time.Minute,
		MaxAttempts:  3,
		RetryDelays:  []time.Duration{5 * time.Minute, 30 * time.Minute},
	})
	if err != nil {
		t.Fatalf("report.New: %v", err)
	}
	return worker
}

func TestWorkerSendsPreviousTehranDaySilently(t *testing.T) {
	st, database := newReportStore(t)
	sender := &fakeReportSender{}
	worker := reportWorker(t, st, database, sender)
	now := time.Date(2026, 11, 6, 0, 5, 0, 0, tehran)

	processed, err := worker.RunOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !processed || sender.count() != 1 {
		t.Fatalf("processed/sends = %v/%d, want one", processed, sender.count())
	}
	text := sender.lastText()
	if !strings.Contains(text, "2026-11-05") || !strings.Contains(text, "1405-08-14") {
		t.Fatalf("report date text:\\n%s", text)
	}
	if !strings.Contains(text, "Quiet day") {
		t.Fatalf("empty day report:\\n%s", text)
	}
	record, err := st.GetDailyReport(context.Background(), "2026-11-05")
	if err != nil {
		t.Fatalf("GetDailyReport: %v", err)
	}
	if record.Status != store.DailyReportSent || record.SentAt == nil || record.MessageID == nil {
		t.Fatalf("report row = %+v", record)
	}
	if record.LastErrorKind != "" || record.LeaseUntil != nil {
		t.Fatalf("sent report diagnostics = %+v", record)
	}

	// A new worker instance reads only persisted state and must not send a
	// duplicate for the same date.
	restarted := reportWorker(t, st, database, sender)
	processed, err = restarted.RunOnce(context.Background(), now.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("restart RunOnce: %v", err)
	}
	if processed || sender.count() != 1 {
		t.Fatalf("duplicate run processed/sends = %v/%d", processed, sender.count())
	}
}

func TestWorkerUsesConfiguredReportTime(t *testing.T) {
	st, database := newReportStore(t)
	sender := &fakeReportSender{}
	worker := reportWorker(t, st, database, sender)
	if err := st.SetDailyReportTime(context.Background(), "06:30", time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("SetDailyReportTime: %v", err)
	}
	if processed, err := worker.RunOnce(context.Background(), time.Date(2026, 11, 6, 6, 29, 0, 0, tehran)); err != nil || processed {
		t.Fatalf("before configured time = %v, %v", processed, err)
	}
	if processed, err := worker.RunOnce(context.Background(), time.Date(2026, 11, 6, 6, 30, 0, 0, tehran)); err != nil || !processed {
		t.Fatalf("at configured time = %v, %v", processed, err)
	}
}

func TestWorkerWaitsForScheduledTime(t *testing.T) {
	st, database := newReportStore(t)
	sender := &fakeReportSender{}
	worker := reportWorker(t, st, database, sender)
	before := time.Date(2026, 11, 6, 0, 4, 0, 0, tehran)

	processed, err := worker.RunOnce(context.Background(), before)
	if err != nil {
		t.Fatalf("RunOnce before schedule: %v", err)
	}
	if processed || sender.count() != 0 {
		t.Fatalf("before-schedule processed/sends = %v/%d", processed, sender.count())
	}
	if _, err := st.GetDailyReport(context.Background(), "2026-11-05"); err != store.ErrNotFound {
		t.Fatalf("before-schedule report row error = %v, want ErrNotFound", err)
	}

	after := time.Date(2026, 11, 6, 23, 0, 0, 0, tehran)
	if processed, err := worker.RunOnce(context.Background(), after); err != nil || !processed {
		t.Fatalf("missed-execution RunOnce = %v, %v; want processed", processed, err)
	}
	if sender.count() != 1 {
		t.Fatalf("sends = %d, want one", sender.count())
	}
}

func TestWorkerSkipsMissingTelegramConfiguration(t *testing.T) {
	st, database := newReportStore(t)
	sender := &fakeReportSender{results: []error{telegramsettings.ErrTelegramNotConfigured}}
	worker := reportWorker(t, st, database, sender)
	now := time.Date(2026, 11, 6, 0, 6, 0, 0, tehran)

	if _, err := worker.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	record, err := st.GetDailyReport(context.Background(), "2026-11-05")
	if err != nil {
		t.Fatalf("GetDailyReport: %v", err)
	}
	if record.Status != store.DailyReportSkipped || record.LastErrorKind != "telegram_not_configured" ||
		record.NextAttemptAt != nil {
		t.Fatalf("skipped report = %+v", record)
	}
	if _, err := worker.RunOnce(context.Background(), now.Add(time.Hour)); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("skipped report was retried; sends = %d", sender.count())
	}
}

func TestWorkerRetriesTransientFailureThenSucceeds(t *testing.T) {
	st, database := newReportStore(t)
	sender := &fakeReportSender{results: []error{&notify.TelegramError{Kind: notify.TelegramErrorTransient}}}
	worker := reportWorker(t, st, database, sender)
	now := time.Date(2026, 11, 6, 0, 6, 0, 0, tehran)

	if _, err := worker.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("RunOnce transient: %v", err)
	}
	record, err := st.GetDailyReport(context.Background(), "2026-11-05")
	if err != nil {
		t.Fatalf("GetDailyReport: %v", err)
	}
	if record.Status != store.DailyReportPending || record.NextAttemptAt == nil ||
		record.LastErrorKind != "telegram_transient" {
		t.Fatalf("transient report = %+v", record)
	}
	if _, err := worker.RunOnce(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatalf("early retry: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("early retry sent again; calls = %d", sender.count())
	}
	if _, err := worker.RunOnce(context.Background(), record.NextAttemptAt.Add(time.Second)); err != nil {
		t.Fatalf("due retry: %v", err)
	}
	if sender.count() != 2 {
		t.Fatalf("due retry calls = %d, want 2", sender.count())
	}
	record, err = st.GetDailyReport(context.Background(), "2026-11-05")
	if err != nil || record.Status != store.DailyReportSent {
		t.Fatalf("final report = %+v, %v", record, err)
	}
}

func TestWorkerLeavesCrashDuringSendUncertain(t *testing.T) {
	st, database := newReportStore(t)
	sender := &fakeReportSender{}
	now := time.Date(2026, 11, 6, 0, 6, 0, 0, tehran)
	date := "2026-11-05"

	if _, _, err := st.EnsureDailyReport(context.Background(), date, now); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := st.ClaimDailyReport(context.Background(), date, now, time.Minute); err != nil || !claimed {
		t.Fatalf("initial claim = %v, %v", claimed, err)
	}
	worker := reportWorker(t, st, database, sender)
	if _, err := worker.RunOnce(context.Background(), now.Add(2*time.Minute)); err != nil {
		t.Fatalf("recovery RunOnce: %v", err)
	}
	record, err := st.GetDailyReport(context.Background(), date)
	if err != nil {
		t.Fatalf("GetDailyReport: %v", err)
	}
	if record.Status != store.DailyReportUncertain || sender.count() != 0 {
		t.Fatalf("crash recovery = %+v sends=%d, want uncertain/no resend", record, sender.count())
	}
}

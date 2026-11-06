// Package report builds and delivers Neda's durable daily Telegram summary.
//
// Reports are scheduled in Asia/Tehran, cover the previous complete local day,
// and are silent. The report row is the idempotency boundary: one row per local
// calendar date, with an at-most-once external send policy for ambiguous
// crashes.
package report

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/jalali"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/store"
)

const (
	// DefaultPollInterval is the worker's schedule/retry polling cadence.
	DefaultPollInterval = time.Minute
	// DefaultLease bounds one report send attempt.
	DefaultLease = 10 * time.Minute
	// DefaultMaxAttempts bounds transient retries for one report date.
	DefaultMaxAttempts = 3
)

var defaultRetryDelays = []time.Duration{
	5 * time.Minute,
	30 * time.Minute,
}

// Summary is the bounded aggregate rendered into one daily report.
type Summary struct {
	ReportDate time.Time
	Gregorian  string
	Jalali     string

	MonitoringEnabled bool

	ChecksTotal     int
	ChecksSuccess   int
	ChecksEmpty     int
	ChecksError     int
	ChecksDiscarded int
	ChecksAbandoned int

	AvailabilityChanges int
	UnavailableNotices  int

	NotificationsSent         int
	NotificationsSentTelegram int
	NotificationsSentPush     int
	NotificationsFailed       int
	NotificationsSkipped      int

	ProviderErrors map[string]int

	MonitorsTotal   int
	MonitorsActive  int
	MonitorsPaused  int
	MonitorsExpired int
	ExpiredToday    int

	AvailabilityLines []string
}

// Quiet reports whether the day had no checks, availability changes, notices,
// or notification outcomes.
func (s Summary) Quiet() bool {
	return s.ChecksTotal == 0 &&
		s.AvailabilityChanges == 0 &&
		s.UnavailableNotices == 0 &&
		s.NotificationsSent == 0 &&
		s.NotificationsFailed == 0 &&
		s.NotificationsSkipped == 0
}

// BuildSummary reads durable check/event/delivery state for the Tehran-local
// day starting at dayStart. It never reads transient HTTP logs.
func BuildSummary(ctx context.Context, db *sql.DB, st *store.Store, dayStart time.Time, now time.Time) (Summary, error) {
	if db == nil {
		return Summary{}, errors.New("report: nil database")
	}
	if st == nil {
		return Summary{}, errors.New("report: nil store")
	}
	dayEnd := dayStart.AddDate(0, 0, 1)
	start := sqlTime(dayStart)
	end := sqlTime(dayEnd)
	summary := Summary{
		ReportDate:     dayStart,
		Gregorian:      dayStart.Format("2006-01-02"),
		Jalali:         jalali.FormatDate(dayStart),
		ProviderErrors: map[string]int{},
	}

	monitoringEnabled, err := st.MonitoringEnabled(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("report: read monitoring switch: %w", err)
	}
	summary.MonitoringEnabled = monitoringEnabled

	rows, err := db.QueryContext(ctx, `SELECT outcome, COUNT(*) FROM check_runs
		WHERE julianday(started_at) >= julianday(?) AND julianday(started_at) < julianday(?)
		GROUP BY outcome`, start, end)
	if err != nil {
		return Summary{}, fmt.Errorf("report: count checks: %w", err)
	}
	for rows.Next() {
		var outcome string
		var count int
		if err := rows.Scan(&outcome, &count); err != nil {
			rows.Close()
			return Summary{}, fmt.Errorf("report: scan checks: %w", err)
		}
		summary.ChecksTotal += count
		switch outcome {
		case "success":
			summary.ChecksSuccess += count
		case "empty":
			summary.ChecksEmpty += count
		case "error":
			summary.ChecksError += count
		case "discarded":
			summary.ChecksDiscarded += count
		case "abandoned":
			summary.ChecksAbandoned += count
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Summary{}, fmt.Errorf("report: iterate checks: %w", err)
	}
	rows.Close()

	rows, err = db.QueryContext(ctx, `SELECT COALESCE(error_kind, 'unknown'), COUNT(*) FROM check_runs
		WHERE outcome = 'error' AND julianday(started_at) >= julianday(?) AND julianday(started_at) < julianday(?)
		GROUP BY COALESCE(error_kind, 'unknown')`, start, end)
	if err != nil {
		return Summary{}, fmt.Errorf("report: count provider errors: %w", err)
	}
	for rows.Next() {
		var kind string
		var count int
		if err := rows.Scan(&kind, &count); err != nil {
			rows.Close()
			return Summary{}, fmt.Errorf("report: scan provider errors: %w", err)
		}
		summary.ProviderErrors[kind] += count
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Summary{}, fmt.Errorf("report: iterate provider errors: %w", err)
	}
	rows.Close()

	var kind string
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events
		WHERE kind = 'availability' AND julianday(occurred_at) >= julianday(?) AND julianday(occurred_at) < julianday(?)`,
		start, end).Scan(&count); err != nil {
		return Summary{}, fmt.Errorf("report: count availability events: %w", err)
	}
	summary.AvailabilityChanges = count
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events
		WHERE kind = 'unavailable' AND julianday(occurred_at) >= julianday(?) AND julianday(occurred_at) < julianday(?)`,
		start, end).Scan(&count); err != nil {
		return Summary{}, fmt.Errorf("report: count unavailable events: %w", err)
	}
	summary.UnavailableNotices = count

	rows, err = db.QueryContext(ctx, `SELECT channel, COUNT(*) FROM notification_deliveries
		WHERE status = 'sent' AND julianday(sent_at) >= julianday(?) AND julianday(sent_at) < julianday(?)
		GROUP BY channel`, start, end)
	if err != nil {
		return Summary{}, fmt.Errorf("report: count sent notifications: %w", err)
	}
	for rows.Next() {
		if err := rows.Scan(&kind, &count); err != nil {
			rows.Close()
			return Summary{}, fmt.Errorf("report: scan sent notifications: %w", err)
		}
		summary.NotificationsSent += count
		switch kind {
		case "telegram":
			summary.NotificationsSentTelegram += count
		case "push":
			summary.NotificationsSentPush += count
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Summary{}, fmt.Errorf("report: iterate sent notifications: %w", err)
	}
	rows.Close()

	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_deliveries
		WHERE status = 'failed' AND julianday(last_attempt_at) >= julianday(?) AND julianday(last_attempt_at) < julianday(?)`,
		start, end).Scan(&summary.NotificationsFailed); err != nil {
		return Summary{}, fmt.Errorf("report: count failed notifications: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_deliveries
		WHERE status = 'skipped' AND julianday(updated_at) >= julianday(?) AND julianday(updated_at) < julianday(?)`,
		start, end).Scan(&summary.NotificationsSkipped); err != nil {
		return Summary{}, fmt.Errorf("report: count skipped notifications: %w", err)
	}

	if err := db.QueryRowContext(ctx, `SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN enabled = 1 AND expired_at IS NULL AND julianday(expires_at) > julianday(?) THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN enabled = 0 AND expired_at IS NULL THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN expired_at IS NOT NULL OR julianday(expires_at) <= julianday(?) THEN 1 ELSE 0 END), 0)
		FROM monitors`, sqlTime(now), sqlTime(now)).Scan(
		&summary.MonitorsTotal, &summary.MonitorsActive, &summary.MonitorsPaused, &summary.MonitorsExpired,
	); err != nil {
		return Summary{}, fmt.Errorf("report: count monitors: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM monitors
		WHERE expired_at IS NOT NULL AND julianday(expired_at) >= julianday(?) AND julianday(expired_at) < julianday(?)`,
		start, end).Scan(&summary.ExpiredToday); err != nil {
		return Summary{}, fmt.Errorf("report: count expired monitors: %w", err)
	}

	// Keep the report useful but bounded: at most three recent transitions.
	rows, err = db.QueryContext(ctx, `SELECT origin_name, destination_name, train_number,
		wagon_name, class_name FROM events
		WHERE kind = 'availability' AND julianday(occurred_at) >= julianday(?) AND julianday(occurred_at) < julianday(?)
		ORDER BY julianday(occurred_at) DESC, id DESC LIMIT 3`, start, end)
	if err != nil {
		return Summary{}, fmt.Errorf("report: list availability lines: %w", err)
	}
	for rows.Next() {
		var origin, destination, train, wagon, class sql.NullString
		if err := rows.Scan(&origin, &destination, &train, &wagon, &class); err != nil {
			rows.Close()
			return Summary{}, fmt.Errorf("report: scan availability line: %w", err)
		}
		summary.AvailabilityLines = append(summary.AvailabilityLines, renderAvailabilityLine(
			optional(origin), optional(destination), optional(train), optional(wagon), optional(class),
		))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Summary{}, fmt.Errorf("report: iterate availability lines: %w", err)
	}
	rows.Close()

	return summary, nil
}

func renderAvailabilityLine(origin, destination, train, wagon, class string) string {
	route := strings.TrimSpace(strings.TrimSpace(origin) + " → " + strings.TrimSpace(destination))
	if route == "→" {
		route = ""
	}
	parts := make([]string, 0, 3)
	if route != "" {
		parts = append(parts, route)
	}
	if train = strings.TrimSpace(train); train != "" {
		parts = append(parts, "Train "+train)
	}
	if wagon = strings.TrimSpace(wagon); wagon != "" {
		if class = strings.TrimSpace(class); class != "" {
			wagon += " / " + class
		}
		parts = append(parts, wagon)
	} else if class = strings.TrimSpace(class); class != "" {
		parts = append(parts, class)
	}
	return strings.Join(parts, " · ")
}

// FormatTelegram renders the report as bounded HTML. Every dynamic value is
// escaped; the fixed warning/count labels are code-owned.
func FormatTelegram(summary Summary) string {
	lines := []string{
		"<b>Neda daily report</b>",
		notify.EscapeTelegramHTML(summary.Gregorian) + " · " + notify.EscapeTelegramHTML(summary.Jalali),
		"Monitoring: " + onOff(summary.MonitoringEnabled),
		fmt.Sprintf("Checks: %d total · %d success · %d empty · %d errors · %d discarded · %d abandoned",
			summary.ChecksTotal, summary.ChecksSuccess, summary.ChecksEmpty,
			summary.ChecksError, summary.ChecksDiscarded, summary.ChecksAbandoned),
		fmt.Sprintf("Availability changes: %d", summary.AvailabilityChanges),
		fmt.Sprintf("Unavailability notices: %d", summary.UnavailableNotices),
		fmt.Sprintf("Notifications: %d sent (Telegram %d · Push %d) · %d failed · %d skipped",
			summary.NotificationsSent, summary.NotificationsSentTelegram,
			summary.NotificationsSentPush, summary.NotificationsFailed, summary.NotificationsSkipped),
		fmt.Sprintf("Monitors: %d total · %d active · %d paused · %d expired",
			summary.MonitorsTotal, summary.MonitorsActive, summary.MonitorsPaused, summary.MonitorsExpired),
		fmt.Sprintf("Expired during the day: %d", summary.ExpiredToday),
	}
	if len(summary.ProviderErrors) > 0 {
		keys := make([]string, 0, len(summary.ProviderErrors))
		for key := range summary.ProviderErrors {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s %d", key, summary.ProviderErrors[key]))
		}
		lines = append(lines, "Provider errors: "+notify.EscapeTelegramHTML(strings.Join(parts, " · ")))
	}
	if len(summary.AvailabilityLines) > 0 {
		lines = append(lines, "", "<b>Recent availability changes</b>")
		for _, line := range summary.AvailabilityLines {
			lines = append(lines, "• "+notify.EscapeTelegramHTML(line))
		}
	}
	if summary.Quiet() {
		lines = append(lines, "", "Quiet day: no checks, availability changes, or notifications.")
	}
	return strings.Join(lines, "\n")
}

func onOff(value bool) string {
	if value {
		return "On"
	}
	return "Off"
}

func optional(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func sqlTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

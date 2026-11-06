package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// DailyReportTimeSettingKey stores the Tehran-local HH:MM report time.
	DailyReportTimeSettingKey = "daily_report_time"
	// DefaultDailyReportTime is the approved 00:05 Asia/Tehran schedule.
	DefaultDailyReportTime = "00:05"
)

// DailyReportStatus is one persisted report lifecycle state.
type DailyReportStatus string

const (
	DailyReportPending   DailyReportStatus = "pending"
	DailyReportSending   DailyReportStatus = "sending"
	DailyReportSent      DailyReportStatus = "sent"
	DailyReportFailed    DailyReportStatus = "failed"
	DailyReportSkipped   DailyReportStatus = "skipped"
	DailyReportUncertain DailyReportStatus = "uncertain"
)

// DailyReportRecord is one durable report row. It contains no message body.
type DailyReportRecord struct {
	ReportDate    string
	Status        DailyReportStatus
	Attempts      int
	NextAttemptAt *time.Time
	LeaseUntil    *time.Time
	StartedAt     *time.Time
	SentAt        *time.Time
	LastErrorKind string
	MessageID     *int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// DailyReportCompletion is the final state of one claimed send attempt.
type DailyReportCompletion struct {
	Status        DailyReportStatus
	SentAt        *time.Time
	MessageID     *int64
	LastErrorKind string
	NextAttemptAt *time.Time
}

const dailyReportColumns = `SELECT report_date, status, attempts, next_attempt_at,
	lease_until, started_at, sent_at, last_error_kind, message_id, created_at, updated_at
FROM daily_reports`

const getDailyReportQuery = dailyReportColumns + ` WHERE report_date = ?`

const latestDailyReportQuery = dailyReportColumns + ` WHERE report_date = (SELECT MAX(report_date) FROM daily_reports)`

// GetDailyReport returns one report row or ErrNotFound.
func (s *Store) GetDailyReport(ctx context.Context, reportDate string) (DailyReportRecord, error) {
	if err := ValidateReportDate(reportDate); err != nil {
		return DailyReportRecord{}, err
	}
	db, err := s.executor()
	if err != nil {
		return DailyReportRecord{}, err
	}
	row := db.QueryRowContext(ctx, getDailyReportQuery, reportDate)
	record, err := scanDailyReport(row)
	if errors.Is(err, sql.ErrNoRows) {
		return DailyReportRecord{}, ErrNotFound
	}
	if err != nil {
		return DailyReportRecord{}, wrap("get daily report", err)
	}
	return record, nil
}

// LatestDailyReport returns the newest report row or ErrNotFound.
func (s *Store) LatestDailyReport(ctx context.Context) (DailyReportRecord, error) {
	db, err := s.executor()
	if err != nil {
		return DailyReportRecord{}, err
	}
	row := db.QueryRowContext(ctx, latestDailyReportQuery)
	record, err := scanDailyReport(row)
	if errors.Is(err, sql.ErrNoRows) {
		return DailyReportRecord{}, ErrNotFound
	}
	if err != nil {
		return DailyReportRecord{}, wrap("get latest daily report", err)
	}
	return record, nil
}

// EnsureDailyReport creates a pending row for reportDate if absent. The unique
// primary key is the duplicate-report guard. Created reports no longer use an
// in-memory schedule.
func (s *Store) EnsureDailyReport(ctx context.Context, reportDate string, now time.Time) (DailyReportRecord, bool, error) {
	if err := ValidateReportDate(reportDate); err != nil {
		return DailyReportRecord{}, false, err
	}
	if now.IsZero() {
		return DailyReportRecord{}, false, errors.New("store: daily report timestamp is required")
	}
	db, err := s.executor()
	if err != nil {
		return DailyReportRecord{}, false, err
	}
	result, err := db.ExecContext(ctx, `INSERT INTO daily_reports
		(report_date, status, attempts, next_attempt_at, lease_until, started_at,
		 sent_at, last_error_kind, message_id, created_at, updated_at)
		VALUES (?, 'pending', 0, ?, NULL, NULL, NULL, NULL, NULL, ?, ?)
		ON CONFLICT(report_date) DO NOTHING`,
		reportDate, formatTime(now), formatTime(now), formatTime(now),
	)
	if err != nil {
		return DailyReportRecord{}, false, wrap("ensure daily report", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return DailyReportRecord{}, false, fmt.Errorf("store: daily report rows affected: %w", err)
	}
	record, err := s.GetDailyReport(ctx, reportDate)
	if err != nil {
		return DailyReportRecord{}, false, err
	}
	return record, affected == 1, nil
}

// ClaimDailyReport atomically claims a due report attempt with a lease. An
// expired sending lease is moved to uncertain instead of being retried: the
// external send may already have succeeded, so Neda prefers a possible missed
// report over a duplicate for the same date.
func (s *Store) ClaimDailyReport(ctx context.Context, reportDate string, now time.Time, lease time.Duration) (DailyReportRecord, bool, error) {
	if err := ValidateReportDate(reportDate); err != nil {
		return DailyReportRecord{}, false, err
	}
	if now.IsZero() {
		return DailyReportRecord{}, false, errors.New("store: daily report claim timestamp is required")
	}
	if lease <= 0 {
		return DailyReportRecord{}, false, errors.New("store: daily report lease must be positive")
	}
	var (
		record  DailyReportRecord
		claimed bool
	)
	err := s.WithSQLTx(ctx, func(_ *sql.Tx, txStore *Store) error {
		current, err := txStore.GetDailyReport(ctx, reportDate)
		if err != nil {
			return err
		}
		switch current.Status {
		case DailyReportSent, DailyReportSkipped, DailyReportUncertain:
			record = current
			return nil
		case DailyReportSending:
			if current.LeaseUntil != nil && current.LeaseUntil.After(now) {
				record = current
				return nil
			}
			if err := txStore.completeDailyReport(ctx, reportDate, now, DailyReportCompletion{
				Status:        DailyReportUncertain,
				LastErrorKind: "lease_expired",
			}); err != nil {
				return err
			}
			record, err = txStore.GetDailyReport(ctx, reportDate)
			return err
		case DailyReportFailed:
			// A terminal failure has no next attempt. Only a retry-scheduled
			// failure may be claimed again.
			if current.NextAttemptAt == nil || current.NextAttemptAt.After(now) {
				record = current
				return nil
			}
		case DailyReportPending:
			if current.NextAttemptAt != nil && current.NextAttemptAt.After(now) {
				record = current
				return nil
			}
		default:
			return fmt.Errorf("store: invalid daily report status %q", current.Status)
		}

		if err := txStore.startDailyReport(ctx, reportDate, now, lease); err != nil {
			return err
		}
		record, err = txStore.GetDailyReport(ctx, reportDate)
		if err != nil {
			return err
		}
		claimed = true
		return nil
	})
	if err != nil {
		return DailyReportRecord{}, false, err
	}
	return record, claimed, nil
}

const startDailyReportQuery = `UPDATE daily_reports SET
	status = 'sending', attempts = attempts + 1, next_attempt_at = NULL,
	lease_until = ?, started_at = ?, last_error_kind = NULL, updated_at = ?
WHERE report_date = ? AND status IN ('pending', 'failed')`

func (s *Store) startDailyReport(ctx context.Context, reportDate string, now time.Time, lease time.Duration) error {
	return execOne(ctx, s, startDailyReportQuery, "start daily report",
		formatTime(now.Add(lease)), formatTime(now), formatTime(now), reportDate,
	)
}

// CompleteDailyReport finalizes one claimed report. It only updates a row that
// is still sending, so a stale worker cannot overwrite a newer state.
func (s *Store) CompleteDailyReport(ctx context.Context, reportDate string, now time.Time, completion DailyReportCompletion) error {
	if err := ValidateReportDate(reportDate); err != nil {
		return err
	}
	if now.IsZero() {
		return errors.New("store: daily report completion timestamp is required")
	}
	db, err := s.executor()
	if err != nil {
		return err
	}
	return (&Store{db: db}).completeDailyReport(ctx, reportDate, now, completion)
}

const completeDailyReportQuery = `UPDATE daily_reports SET
	status = ?, next_attempt_at = ?, lease_until = NULL, sent_at = ?,
	last_error_kind = ?, message_id = ?, updated_at = ?
WHERE report_date = ? AND status = 'sending'`

func (s *Store) completeDailyReport(ctx context.Context, reportDate string, now time.Time, completion DailyReportCompletion) error {
	switch completion.Status {
	case DailyReportSent, DailyReportFailed, DailyReportSkipped, DailyReportUncertain, DailyReportPending:
	default:
		return fmt.Errorf("store: invalid daily report completion status %q", completion.Status)
	}
	if completion.Status == DailyReportSent && (completion.SentAt == nil || completion.SentAt.IsZero()) {
		return errors.New("store: sent daily report requires sent_at")
	}
	if completion.Status == DailyReportPending && completion.NextAttemptAt == nil {
		return errors.New("store: pending daily report requires next_attempt_at")
	}
	var sentAt any
	if completion.SentAt != nil {
		sentAt = formatTime(*completion.SentAt)
	}
	var nextAttempt any
	if completion.NextAttemptAt != nil {
		nextAttempt = formatTime(*completion.NextAttemptAt)
	}
	var messageID any
	if completion.MessageID != nil {
		if *completion.MessageID <= 0 {
			return errors.New("store: daily report message id must be positive")
		}
		messageID = *completion.MessageID
	}
	return execOne(ctx, s, completeDailyReportQuery, "complete daily report",
		string(completion.Status), nextAttempt, sentAt,
		nullableString(completion.LastErrorKind), messageID, formatTime(now), reportDate,
	)
}

// MarkDailyReportUncertain is the explicit recovery path for a crash during a
// send attempt. Callers may use it when they cannot prove whether Telegram
// accepted the message.
func (s *Store) MarkDailyReportUncertain(ctx context.Context, reportDate string, now time.Time, errorKind string) error {
	return s.CompleteDailyReport(ctx, reportDate, now, DailyReportCompletion{
		Status:        DailyReportUncertain,
		LastErrorKind: errorKind,
	})
}

// EnsureDailyReportTime seeds the default time without overwriting an
// operator-set value.
func (s *Store) EnsureDailyReportTime(ctx context.Context, now time.Time) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	if now.IsZero() {
		return errors.New("store: daily report time timestamp is required")
	}
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO settings (key, value, is_secret, updated_at) VALUES (?, ?, 0, ?)`,
		DailyReportTimeSettingKey, DefaultDailyReportTime, formatTime(now),
	); err != nil {
		return wrap("seed daily report time", err)
	}
	return nil
}

// DailyReportTime returns the configured Tehran-local HH:MM time.
func (s *Store) DailyReportTime(ctx context.Context) (string, error) {
	setting, err := s.GetSetting(ctx, DailyReportTimeSettingKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return DefaultDailyReportTime, nil
		}
		return "", err
	}
	value := strings.TrimSpace(setting.Value)
	if err := ValidateDailyReportTime(value); err != nil {
		return "", fmt.Errorf("store: %w", err)
	}
	return value, nil
}

// SetDailyReportTime validates and upserts the report time.
func (s *Store) SetDailyReportTime(ctx context.Context, value string, now time.Time) error {
	value = strings.TrimSpace(value)
	if err := ValidateDailyReportTime(value); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if now.IsZero() {
		return errors.New("store: daily report time timestamp is required")
	}
	return s.SetSetting(ctx, Setting{
		Key:       DailyReportTimeSettingKey,
		Value:     value,
		UpdatedAt: now,
	})
}

// ValidateDailyReportTime accepts only a strict 24-hour HH:MM value.
func ValidateDailyReportTime(value string) error {
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return errors.New("daily report time must be HH:MM in 24-hour Tehran time")
	}
	return nil
}

// ValidateReportDate accepts only a civil YYYY-MM-DD date.
func ValidateReportDate(value string) error {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return errors.New("store: report date must be YYYY-MM-DD")
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDailyReport(row rowScanner) (DailyReportRecord, error) {
	var (
		record                                     DailyReportRecord
		status                                     string
		nextAttempt, lease, started, sent, created sql.NullString
		updatedAt, errorKind                       sql.NullString
		messageID                                  sql.NullInt64
	)
	if err := row.Scan(&record.ReportDate, &status, &record.Attempts, &nextAttempt,
		&lease, &started, &sent, &errorKind, &messageID, &created, &updatedAt); err != nil {
		return DailyReportRecord{}, err
	}
	record.Status = DailyReportStatus(status)
	switch record.Status {
	case DailyReportPending, DailyReportSending, DailyReportSent,
		DailyReportFailed, DailyReportSkipped, DailyReportUncertain:
	default:
		return DailyReportRecord{}, fmt.Errorf("store: invalid daily report status %q", status)
	}
	var err error
	if record.NextAttemptAt, err = parseOptionalTime(nextAttempt); err != nil {
		return DailyReportRecord{}, err
	}
	if record.LeaseUntil, err = parseOptionalTime(lease); err != nil {
		return DailyReportRecord{}, err
	}
	if record.StartedAt, err = parseOptionalTime(started); err != nil {
		return DailyReportRecord{}, err
	}
	if record.SentAt, err = parseOptionalTime(sent); err != nil {
		return DailyReportRecord{}, err
	}
	record.LastErrorKind = optionalString(errorKind)
	if messageID.Valid {
		value := messageID.Int64
		record.MessageID = &value
	}
	if record.CreatedAt, err = parseTime(created.String); err != nil {
		return DailyReportRecord{}, err
	}
	if record.UpdatedAt, err = parseTime(updatedAt.String); err != nil {
		return DailyReportRecord{}, err
	}
	return record, nil
}

func execOne(ctx context.Context, s *Store, query, description string, args ...any) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return wrap(description, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: %s rows affected: %w", description, err)
	}
	if affected != 1 {
		return fmt.Errorf("store: %s found no matching row", description)
	}
	return nil
}

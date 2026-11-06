-- 0007_daily_reports.sql — durable daily Telegram report state.
CREATE TABLE daily_reports (
    report_date     TEXT PRIMARY KEY
                    CHECK (report_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    status          TEXT NOT NULL
                    CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'skipped', 'uncertain')),
    attempts        INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TEXT NULL,
    lease_until     TEXT NULL,
    started_at      TEXT NULL,
    sent_at         TEXT NULL,
    last_error_kind TEXT NULL,
    message_id      INTEGER NULL CHECK (message_id IS NULL OR message_id > 0),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    CHECK ((status = 'sent' AND sent_at IS NOT NULL) OR (status <> 'sent'))
);

CREATE INDEX idx_daily_reports_status_next_attempt
    ON daily_reports(status, next_attempt_at);

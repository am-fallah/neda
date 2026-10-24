-- 0004_notifications.sql — provider health, notification outbox, Push subscriptions.

CREATE TABLE provider_health_episodes (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id           TEXT NOT NULL,
    opened_at             TEXT NOT NULL,
    closed_at             TEXT NULL,
    failure_count_at_open INTEGER NOT NULL CHECK (failure_count_at_open >= 1),
    last_error_kind       TEXT NULL
);

CREATE UNIQUE INDEX ux_one_open_provider_episode
    ON provider_health_episodes(provider_id)
    WHERE closed_at IS NULL;

CREATE TABLE provider_health (
    provider_id          TEXT PRIMARY KEY,
    last_attempt_at      TEXT NULL,
    last_success_at      TEXT NULL,
    last_failure_at      TEXT NULL,
    last_error_kind      TEXT NULL,
    last_duration_ms     INTEGER NULL,
    consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    open_episode_id      INTEGER NULL,
    updated_at           TEXT NOT NULL
);

CREATE TABLE notification_deliveries (
    id              TEXT PRIMARY KEY,
    event_id        TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    channel         TEXT NOT NULL CHECK (channel IN ('telegram', 'push')),
    target_key      TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('pending', 'sending', 'sent', 'retry_scheduled', 'failed', 'skipped')),
    attempt_count   INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TEXT NULL,
    last_attempt_at TEXT NULL,
    sent_at         TEXT NULL,
    last_error_kind TEXT NULL,
    lease_until     TEXT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE (event_id, channel, target_key)
);

CREATE INDEX idx_notification_deliveries_claim
    ON notification_deliveries(status, next_attempt_at);

CREATE INDEX idx_notification_deliveries_event
    ON notification_deliveries(event_id);

CREATE TABLE notification_attempts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    delivery_id TEXT NOT NULL REFERENCES notification_deliveries(id) ON DELETE CASCADE,
    attempt_no  INTEGER NOT NULL CHECK (attempt_no >= 1),
    started_at  TEXT NOT NULL,
    finished_at TEXT NULL,
    status      TEXT NOT NULL CHECK (status IN ('sending', 'sent', 'failed', 'skipped')),
    error_kind  TEXT NULL,
    http_status INTEGER NULL,
    duration_ms INTEGER NULL,
    UNIQUE (delivery_id, attempt_no)
);

CREATE TABLE push_subscriptions (
    id              TEXT PRIMARY KEY,
    endpoint_hash   TEXT NOT NULL UNIQUE,
    endpoint_enc    BLOB NOT NULL,
    p256dh_enc      BLOB NOT NULL,
    auth_enc        BLOB NOT NULL,
    label           TEXT NULL,
    created_at      TEXT NOT NULL,
    last_success_at TEXT NULL,
    failure_count   INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    disabled_at     TEXT NULL
);

CREATE INDEX idx_push_subscriptions_disabled_at
    ON push_subscriptions(disabled_at);

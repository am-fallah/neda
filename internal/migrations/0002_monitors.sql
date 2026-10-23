-- 0002_monitors.sql — monitors and their provider-specific targets.
CREATE TABLE monitors (
    id               TEXT PRIMARY KEY,
    label            TEXT NOT NULL CHECK (length(label) BETWEEN 1 AND 100),
    origin_name      TEXT NOT NULL CHECK (length(origin_name) > 0),
    destination_name TEXT NOT NULL CHECK (length(destination_name) > 0),
    departure_date   TEXT NOT NULL CHECK (departure_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    adults           INTEGER NOT NULL CHECK (adults >= 1),
    children         INTEGER NOT NULL CHECK (children >= 0),
    infants          INTEGER NOT NULL CHECK (infants >= 0),
    enabled          INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    expires_at       TEXT NOT NULL,
    expired_at       TEXT NULL,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE INDEX idx_monitors_enabled_expires_at ON monitors(enabled, expires_at);
CREATE INDEX idx_monitors_departure_date ON monitors(departure_date);

CREATE TABLE monitor_providers (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_id           TEXT NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    provider_id          TEXT NOT NULL,
    active               INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    origin_id            TEXT NOT NULL,
    destination_id       TEXT NOT NULL,
    booking_slug         TEXT NULL,
    gender_code          TEXT NULL,
    config_version       INTEGER NOT NULL DEFAULT 1 CHECK (config_version >= 1),
    next_due_at          TEXT NULL,
    last_attempt_at      TEXT NULL,
    last_success_at      TEXT NULL,
    last_outcome         TEXT NULL CHECK (last_outcome IN ('success', 'empty', 'error') OR last_outcome IS NULL),
    last_error_kind      TEXT NULL,
    last_error_at        TEXT NULL,
    consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    created_at           TEXT NOT NULL,
    updated_at           TEXT NOT NULL,
    UNIQUE (monitor_id, provider_id)
);

CREATE INDEX idx_monitor_providers_next_due_at ON monitor_providers(next_due_at);
CREATE INDEX idx_monitor_providers_provider_id ON monitor_providers(provider_id);

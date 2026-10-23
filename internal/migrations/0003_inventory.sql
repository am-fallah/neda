-- 0003_inventory.sql — check diagnostics, inventory snapshots, episodes, events.
CREATE TABLE check_runs (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_provider_id  INTEGER NOT NULL REFERENCES monitor_providers(id) ON DELETE CASCADE,
    config_version       INTEGER NOT NULL CHECK (config_version >= 1),
    started_at           TEXT NOT NULL,
    finished_at          TEXT NULL,
    outcome              TEXT NOT NULL CHECK (outcome IN ('running', 'success', 'empty', 'error', 'discarded', 'abandoned')),
    error_kind           TEXT NULL,
    http_status          INTEGER NULL,
    duration_ms          INTEGER NULL CHECK (duration_ms IS NULL OR duration_ms >= 0),
    journey_count        INTEGER NOT NULL DEFAULT 0 CHECK (journey_count >= 0),
    offering_count       INTEGER NOT NULL DEFAULT 0 CHECK (offering_count >= 0),
    reservable_count     INTEGER NOT NULL DEFAULT 0 CHECK (reservable_count >= 0),
    not_reservable_count INTEGER NOT NULL DEFAULT 0 CHECK (not_reservable_count >= 0),
    unknown_count        INTEGER NOT NULL DEFAULT 0 CHECK (unknown_count >= 0),
    warning_count        INTEGER NOT NULL DEFAULT 0 CHECK (warning_count >= 0),
    created_at           TEXT NOT NULL,
    CHECK ((outcome = 'running' AND finished_at IS NULL) OR (outcome <> 'running' AND finished_at IS NOT NULL))
);

CREATE INDEX idx_check_runs_target_started ON check_runs(monitor_provider_id, started_at DESC);
CREATE INDEX idx_check_runs_started ON check_runs(started_at);

CREATE TABLE inventory_states (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_provider_id INTEGER NOT NULL REFERENCES monitor_providers(id) ON DELETE CASCADE,
    config_version      INTEGER NOT NULL CHECK (config_version >= 1),
    journey_key         TEXT NOT NULL,
    offering_key        TEXT NOT NULL,
    state               TEXT NOT NULL CHECK (state IN ('reservable', 'not_reservable', 'unknown')),
    state_since         TEXT NOT NULL,
    last_observed_at    TEXT NOT NULL,
    last_decisive_at    TEXT NULL,
    last_missing_at     TEXT NULL,
    last_reason         TEXT NULL,
    last_capacity       INTEGER NULL CHECK (last_capacity IS NULL OR last_capacity >= 0),
    train_number        TEXT NULL,
    operator            TEXT NULL,
    origin_name         TEXT NULL,
    destination_name    TEXT NULL,
    departure_at        TEXT NULL,
    arrival_at          TEXT NULL,
    wagon_name          TEXT NULL,
    class_name          TEXT NULL,
    price_amount        INTEGER NULL,
    price_currency      TEXT NULL,
    last_booking_url    TEXT NULL,
    updated_at          TEXT NOT NULL,
    UNIQUE (monitor_provider_id, config_version, journey_key, offering_key)
);

CREATE INDEX idx_inventory_states_target_state ON inventory_states(monitor_provider_id, config_version, state);

CREATE TABLE availability_episodes (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    state_id     INTEGER NOT NULL REFERENCES inventory_states(id) ON DELETE CASCADE,
    episode_seq  INTEGER NOT NULL CHECK (episode_seq >= 1),
    opened_at    TEXT NOT NULL,
    closed_at    TEXT NULL,
    close_reason TEXT NULL CHECK (close_reason IN ('not_reservable', 'config_changed', 'expired', 'deleted') OR close_reason IS NULL),
    UNIQUE (state_id, episode_seq),
    CHECK ((closed_at IS NULL AND close_reason IS NULL) OR (closed_at IS NOT NULL AND close_reason IS NOT NULL))
);

CREATE UNIQUE INDEX ux_one_open_episode ON availability_episodes(state_id) WHERE closed_at IS NULL;

CREATE TABLE events (
    id             TEXT PRIMARY KEY,
    kind           TEXT NOT NULL CHECK (kind IN ('availability', 'provider_degraded', 'provider_recovered')),
    dedupe_key     TEXT NOT NULL UNIQUE,
    monitor_id     TEXT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    provider_id    TEXT NOT NULL,
    episode_id     INTEGER NULL REFERENCES availability_episodes(id) ON DELETE CASCADE,
    occurred_at    TEXT NOT NULL,
    train_number   TEXT NULL,
    operator       TEXT NULL,
    origin_name    TEXT NULL,
    destination_name TEXT NULL,
    departure_at   TEXT NULL,
    arrival_at     TEXT NULL,
    wagon_name     TEXT NULL,
    class_name     TEXT NULL,
    price_amount   INTEGER NULL,
    price_currency TEXT NULL,
    booking_url    TEXT NULL,
    error_kind     TEXT NULL,
    failure_count  INTEGER NULL CHECK (failure_count IS NULL OR failure_count >= 1),
    created_at     TEXT NOT NULL,
    CHECK (
        (kind = 'availability' AND monitor_id IS NOT NULL AND episode_id IS NOT NULL)
        OR
        (kind IN ('provider_degraded', 'provider_recovered') AND monitor_id IS NULL AND episode_id IS NULL AND error_kind IS NOT NULL AND failure_count IS NOT NULL)
    )
);

CREATE INDEX idx_events_occurred_at ON events(occurred_at DESC);
CREATE INDEX idx_events_kind_occurred_at ON events(kind, occurred_at DESC);
CREATE INDEX idx_events_monitor_occurred_at ON events(monitor_id, occurred_at DESC);

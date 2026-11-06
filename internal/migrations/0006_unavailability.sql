-- 0006_unavailability.sql — confirmed-unavailability streaks and silent notices.
-- neda:foreign-keys-off
--
-- SQLite cannot add a value to an existing CHECK constraint. The events table is
-- rebuilt with the new 'unavailable' kind and the silent flag. The migration
-- runner disables foreign-key enforcement around this marked migration and runs
-- PRAGMA foreign_key_check afterwards; legacy_alter_table keeps the child
-- notification_deliveries reference pointed at the final events table.

ALTER TABLE inventory_states ADD COLUMN unavailable_since TEXT NULL;
ALTER TABLE inventory_states ADD COLUMN last_not_reservable_at TEXT NULL;
ALTER TABLE inventory_states ADD COLUMN unavailable_notified_at TEXT NULL;

CREATE INDEX idx_inventory_states_unavailable_since
    ON inventory_states(unavailable_since)
    WHERE unavailable_since IS NOT NULL;

PRAGMA legacy_alter_table = ON;
ALTER TABLE events RENAME TO events_old;

CREATE TABLE events (
    id             TEXT PRIMARY KEY,
    kind           TEXT NOT NULL CHECK (kind IN ('availability', 'unavailable', 'provider_degraded', 'provider_recovered')),
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
    silent         INTEGER NOT NULL DEFAULT 0 CHECK (silent IN (0, 1)),
    created_at     TEXT NOT NULL,
    CHECK (
        (kind = 'availability' AND monitor_id IS NOT NULL AND episode_id IS NOT NULL)
        OR
        (kind = 'unavailable' AND monitor_id IS NOT NULL AND episode_id IS NULL
            AND error_kind IS NULL AND failure_count IS NULL)
        OR
        (kind IN ('provider_degraded', 'provider_recovered') AND monitor_id IS NULL
            AND episode_id IS NULL AND error_kind IS NOT NULL AND failure_count IS NOT NULL)
    )
);

INSERT INTO events (
    id, kind, dedupe_key, monitor_id, provider_id, episode_id, occurred_at,
    train_number, operator, origin_name, destination_name, departure_at, arrival_at,
    wagon_name, class_name, price_amount, price_currency, booking_url,
    error_kind, failure_count, silent, created_at
)
SELECT
    id, kind, dedupe_key, monitor_id, provider_id, episode_id, occurred_at,
    train_number, operator, origin_name, destination_name, departure_at, arrival_at,
    wagon_name, class_name, price_amount, price_currency, booking_url,
    error_kind, failure_count, 0, created_at
FROM events_old;

DROP TABLE events_old;
PRAGMA legacy_alter_table = OFF;

CREATE INDEX idx_events_occurred_at ON events(occurred_at DESC);
CREATE INDEX idx_events_kind_occurred_at ON events(kind, occurred_at DESC);
CREATE INDEX idx_events_monitor_occurred_at ON events(monitor_id, occurred_at DESC);

-- 0005_monitor_selections.sql — selected train/class options per provider target.
CREATE TABLE monitor_provider_selections (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_provider_id  INTEGER NOT NULL REFERENCES monitor_providers(id) ON DELETE CASCADE,
    config_version       INTEGER NOT NULL CHECK (config_version >= 1),
    journey_key          TEXT NOT NULL,
    offering_key         TEXT NOT NULL,
    journey_native_id    TEXT NULL,
    offering_native_id   TEXT NULL,
    train_number         TEXT NULL,
    operator             TEXT NULL,
    origin_name          TEXT NULL,
    destination_name     TEXT NULL,
    departure_at         TEXT NULL,
    arrival_at           TEXT NULL,
    wagon_id             TEXT NULL,
    wagon_name           TEXT NULL,
    class_name           TEXT NULL,
    compartment_type     TEXT NULL,
    created_at           TEXT NOT NULL,
    UNIQUE (monitor_provider_id, config_version, journey_key, offering_key)
);

CREATE INDEX idx_monitor_provider_selections_target
    ON monitor_provider_selections(monitor_provider_id, config_version);

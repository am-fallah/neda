-- 0009_check_request_metrics.sql — per-check logical provider metrics.
--
-- These fields describe one logical adapter.Check invocation. They are written
-- in the existing check_runs finalization update, so no extra commit is added
-- to the check path. Rows created before this migration remain
-- provider_request=0 and are excluded from provider traffic metrics.
ALTER TABLE check_runs
    ADD COLUMN provider_request INTEGER NOT NULL DEFAULT 0
    CHECK (provider_request IN (0, 1));

ALTER TABLE check_runs
    ADD COLUMN provider_started_at TEXT NULL;

ALTER TABLE check_runs
    ADD COLUMN provider_duration_ms INTEGER NULL
    CHECK (provider_duration_ms IS NULL OR provider_duration_ms >= 0);

ALTER TABLE check_runs
    ADD COLUMN provider_error_kind TEXT NULL;

ALTER TABLE check_runs
    ADD COLUMN queue_wait_ms INTEGER NULL
    CHECK (queue_wait_ms IS NULL OR queue_wait_ms >= 0);

ALTER TABLE check_runs
    ADD COLUMN expected_spacing_ms INTEGER NULL
    CHECK (expected_spacing_ms IS NULL OR expected_spacing_ms >= 0);

ALTER TABLE check_runs
    ADD COLUMN configured_interval_ms INTEGER NULL
    CHECK (configured_interval_ms IS NULL OR configured_interval_ms >= 0);

ALTER TABLE check_runs
    ADD COLUMN adaptive_interval_ms INTEGER NULL
    CHECK (adaptive_interval_ms IS NULL OR adaptive_interval_ms >= 0);

CREATE INDEX idx_check_runs_provider_started
    ON check_runs(provider_started_at)
    WHERE provider_request = 1;

CREATE INDEX idx_check_runs_provider_target_started
    ON check_runs(monitor_provider_id, provider_started_at)
    WHERE provider_request = 1;

CREATE INDEX idx_check_runs_provider_errors
    ON check_runs(provider_started_at)
    WHERE provider_error_kind IS NOT NULL;

-- Crash-abandoned attempts are recovered to outcome='abandoned' without a
-- provider_request marker. This partial index keeps incomplete-check reads
-- bounded without scanning the full check history.
CREATE INDEX idx_check_runs_incomplete
    ON check_runs(started_at)
    WHERE outcome = 'abandoned' AND provider_request = 0;

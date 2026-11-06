-- 0010_provider_minute_metrics.sql — bounded provider-level rollup and its
-- durable source rows.
--
-- Traffic columns are deterministically recomputed from check_runs. Queue
-- columns are deterministically recomputed from provider_queue_samples, and
-- deferral-episode counts from provider_deferral_episodes. The source tables
-- make replay idempotent and allow late samples to correct an already-written
-- minute. provider_minute_metrics has no monitor/target foreign key because
-- provider traffic history must survive monitor or target deletion.
CREATE TABLE provider_minute_metrics (
    minute_start                  TEXT NOT NULL,
    provider_id                   TEXT NOT NULL,

    provider_checks               INTEGER NOT NULL DEFAULT 0
                                  CHECK (provider_checks >= 0),
    successes                     INTEGER NOT NULL DEFAULT 0
                                  CHECK (successes >= 0),
    failures                      INTEGER NOT NULL DEFAULT 0
                                  CHECK (failures >= 0),
    rate_limited                  INTEGER NOT NULL DEFAULT 0
                                  CHECK (rate_limited >= 0),
    interrupted                   INTEGER NOT NULL DEFAULT 0
                                  CHECK (interrupted >= 0),
    discarded                     INTEGER NOT NULL DEFAULT 0
                                  CHECK (discarded >= 0),
    incomplete                    INTEGER NOT NULL DEFAULT 0
                                  CHECK (incomplete >= 0),

    duration_sum_ms               INTEGER NOT NULL DEFAULT 0
                                  CHECK (duration_sum_ms >= 0),
    duration_count                INTEGER NOT NULL DEFAULT 0
                                  CHECK (duration_count >= 0),
    -- Disjoint duration buckets. Intervals are (previous bound, bound]:
    -- 0..50, 51..100, 101..250, 251..500, 501..1000, 1001..2000,
    -- 2001..5000, 5001..10000, 10001..20000, and >20000 (overflow).
    dur_0_50                      INTEGER NOT NULL DEFAULT 0 CHECK (dur_0_50 >= 0),
    dur_51_100                    INTEGER NOT NULL DEFAULT 0 CHECK (dur_51_100 >= 0),
    dur_101_250                   INTEGER NOT NULL DEFAULT 0 CHECK (dur_101_250 >= 0),
    dur_251_500                   INTEGER NOT NULL DEFAULT 0 CHECK (dur_251_500 >= 0),
    dur_501_1000                  INTEGER NOT NULL DEFAULT 0 CHECK (dur_501_1000 >= 0),
    dur_1001_2000                 INTEGER NOT NULL DEFAULT 0 CHECK (dur_1001_2000 >= 0),
    dur_2001_5000                 INTEGER NOT NULL DEFAULT 0 CHECK (dur_2001_5000 >= 0),
    dur_5001_10000                INTEGER NOT NULL DEFAULT 0 CHECK (dur_5001_10000 >= 0),
    dur_10001_20000               INTEGER NOT NULL DEFAULT 0 CHECK (dur_10001_20000 >= 0),
    dur_gt_20000                  INTEGER NOT NULL DEFAULT 0 CHECK (dur_gt_20000 >= 0),

    dispatch_attempts             INTEGER NOT NULL DEFAULT 0
                                  CHECK (dispatch_attempts >= 0),
    scheduler_samples             INTEGER NOT NULL DEFAULT 0
                                  CHECK (scheduler_samples >= 0),
    due_avg                       INTEGER NOT NULL DEFAULT 0 CHECK (due_avg >= 0),
    due_max                       INTEGER NOT NULL DEFAULT 0 CHECK (due_max >= 0),
    overdue_max                   INTEGER NOT NULL DEFAULT 0 CHECK (overdue_max >= 0),
    deferred_avg                  INTEGER NOT NULL DEFAULT 0 CHECK (deferred_avg >= 0),
    deferred_max                  INTEGER NOT NULL DEFAULT 0 CHECK (deferred_max >= 0),
    in_flight_max                 INTEGER NOT NULL DEFAULT 0 CHECK (in_flight_max >= 0),
    oldest_due_age_max_ms         INTEGER NOT NULL DEFAULT 0
                                  CHECK (oldest_due_age_max_ms >= 0),
    deferral_episodes_cooldown    INTEGER NOT NULL DEFAULT 0
                                  CHECK (deferral_episodes_cooldown >= 0),
    deferral_episodes_spacing     INTEGER NOT NULL DEFAULT 0
                                  CHECK (deferral_episodes_spacing >= 0),
    deferral_episodes_concurrency INTEGER NOT NULL DEFAULT 0
                                  CHECK (deferral_episodes_concurrency >= 0),
    queue_wait_sum_ms             INTEGER NOT NULL DEFAULT 0
                                  CHECK (queue_wait_sum_ms >= 0),
    queue_wait_count              INTEGER NOT NULL DEFAULT 0
                                  CHECK (queue_wait_count >= 0),

    traffic_derived_at            TEXT NULL,
    queue_sampled_at              TEXT NULL,
    last_execution_at             TEXT NULL,

    PRIMARY KEY (minute_start, provider_id)
);

CREATE INDEX idx_provider_minute_metrics_provider
    ON provider_minute_metrics(provider_id, minute_start DESC);

CREATE INDEX idx_provider_minute_metrics_time
    ON provider_minute_metrics(minute_start);

-- One row per bounded scheduler sample. The random per-process boot prefix plus
-- monotonic sequence makes replay safe across restarts. These rows are the
-- durable source for queue rollups; they are retained with the rollup.
CREATE TABLE provider_queue_samples (
    id                     TEXT PRIMARY KEY,
    provider_id            TEXT NOT NULL,
    sampled_at             TEXT NOT NULL,
    minute_start           TEXT NOT NULL,
    due                    INTEGER NOT NULL DEFAULT 0 CHECK (due >= 0),
    in_flight              INTEGER NOT NULL DEFAULT 0 CHECK (in_flight >= 0),
    queued                 INTEGER NOT NULL DEFAULT 0 CHECK (queued >= 0),
    overdue                INTEGER NOT NULL DEFAULT 0 CHECK (overdue >= 0),
    deferred               INTEGER NOT NULL DEFAULT 0 CHECK (deferred >= 0),
    deferred_by_cooldown   INTEGER NOT NULL DEFAULT 0
                            CHECK (deferred_by_cooldown >= 0),
    deferred_by_spacing    INTEGER NOT NULL DEFAULT 0
                            CHECK (deferred_by_spacing >= 0),
    deferred_by_concurrency INTEGER NOT NULL DEFAULT 0
                            CHECK (deferred_by_concurrency >= 0),
    oldest_pending_age_ms  INTEGER NOT NULL DEFAULT 0
                            CHECK (oldest_pending_age_ms >= 0),
    next_eligible_at       TEXT NULL,
    blocked_by_concurrency INTEGER NOT NULL DEFAULT 0
                            CHECK (blocked_by_concurrency IN (0, 1)),
    dispatched             INTEGER NOT NULL DEFAULT 0 CHECK (dispatched >= 0),
    created_at             TEXT NOT NULL
);

CREATE INDEX idx_provider_queue_samples_minute
    ON provider_queue_samples(minute_start, provider_id);

CREATE INDEX idx_provider_queue_samples_provider
    ON provider_queue_samples(provider_id, minute_start);

-- One durable logical deferral episode. Identity is target + reason, not
-- next_due_at. A reason transition closes one episode and opens another; a
-- deadline change does not. target_id is nullable so provider history survives
-- target deletion.
CREATE TABLE provider_deferral_episodes (
    id            TEXT PRIMARY KEY,
    provider_id   TEXT NOT NULL,
    target_id     INTEGER NULL
                  REFERENCES monitor_providers(id) ON DELETE SET NULL,
    reason        TEXT NOT NULL
                  CHECK (reason IN ('cooldown', 'spacing', 'concurrency')),
    started_at    TEXT NOT NULL,
    last_seen_at  TEXT NOT NULL,
    resolved_at   TEXT NULL,
    created_at    TEXT NOT NULL,
    CHECK (resolved_at IS NULL OR resolved_at >= started_at)
);

CREATE INDEX idx_provider_deferral_episodes_provider_time
    ON provider_deferral_episodes(provider_id, started_at);

CREATE INDEX idx_provider_deferral_episodes_started
    ON provider_deferral_episodes(started_at);

CREATE INDEX idx_provider_deferral_episodes_active
    ON provider_deferral_episodes(target_id)
    WHERE resolved_at IS NULL;

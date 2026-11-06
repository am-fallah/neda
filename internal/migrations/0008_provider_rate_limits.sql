-- 0008_provider_rate_limits.sql — durable adaptive provider throttling state.
--
-- The configured polling interval remains in settings and is never overwritten
-- by rate limiting. This table stores only temporary penalties, the hard
-- Retry-After cooldown, and recovery progress. Scope is explicit so a future
-- adapter can narrow the penalty without another schema change; the current
-- provider boundary reports only provider-level scope.
CREATE TABLE provider_rate_limits (
    provider_id           TEXT NOT NULL,
    scope                 TEXT NOT NULL DEFAULT 'provider'
                          CHECK (length(scope) BETWEEN 1 AND 64),
    consecutive_429s      INTEGER NOT NULL DEFAULT 0
                          CHECK (consecutive_429s >= 0),
    adaptive_interval_ms  INTEGER NOT NULL DEFAULT 0
                          CHECK (adaptive_interval_ms >= 0),
    cooldown_until        TEXT NULL,
    consecutive_successes INTEGER NOT NULL DEFAULT 0
                          CHECK (consecutive_successes >= 0),
    last_429_at           TEXT NULL,
    last_success_at       TEXT NULL,
    last_retry_after_ms   INTEGER NOT NULL DEFAULT 0
                          CHECK (last_retry_after_ms >= 0),
    updated_at            TEXT NOT NULL,
    PRIMARY KEY (provider_id, scope)
);

CREATE INDEX idx_provider_rate_limits_cooldown
    ON provider_rate_limits(cooldown_until)
    WHERE cooldown_until IS NOT NULL;

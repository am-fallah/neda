-- 0011_provider_rate_limit_events.sql — best-effort adaptive/cooldown history.
--
-- Current provider state remains authoritative in provider_rate_limits. These
-- rows are persisted after the outcome transaction commits, so a history write
-- failure can never roll back a check outcome or target schedule. Event kinds
-- distinguish every observed 429 from an escalation, cooldown extension,
-- partial recovery, and full recovery.
CREATE TABLE provider_rate_limit_events (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id             TEXT NOT NULL,
    scope                   TEXT NOT NULL DEFAULT 'provider'
                            CHECK (length(scope) BETWEEN 1 AND 64),
    occurred_at             TEXT NOT NULL,
    kind                    TEXT NOT NULL
                            CHECK (kind IN (
                                'rate_limited',
                                'cooldown_extended',
                                'recovery_progress',
                                'recovery_step',
                                'recovered'
                            )),
    previous_level          INTEGER NOT NULL DEFAULT 0
                            CHECK (previous_level >= 0),
    escalation_level        INTEGER NOT NULL CHECK (escalation_level >= 0),
    previous_adaptive_ms    INTEGER NOT NULL CHECK (previous_adaptive_ms >= 0),
    adaptive_ms             INTEGER NOT NULL CHECK (adaptive_ms >= 0),
    previous_cooldown_until TEXT NULL,
    cooldown_until          TEXT NULL,
    retry_after_ms          INTEGER NOT NULL DEFAULT 0
                            CHECK (retry_after_ms >= 0),
    check_run_id            INTEGER NULL
                            REFERENCES check_runs(id) ON DELETE SET NULL,
    created_at              TEXT NOT NULL
);

CREATE INDEX idx_provider_rate_limit_events_provider
    ON provider_rate_limit_events(provider_id, occurred_at DESC);

CREATE INDEX idx_provider_rate_limit_events_time
    ON provider_rate_limit_events(occurred_at DESC);

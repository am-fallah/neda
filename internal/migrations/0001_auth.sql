-- 0001_auth.sql — settings, single owner user, and sessions.
CREATE TABLE settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    is_secret   INTEGER NOT NULL CHECK (is_secret IN (0, 1)),
    key_version INTEGER NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE users (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash       TEXT NOT NULL,
    password_changed_at TEXT NOT NULL,
    created_at          TEXT NOT NULL
);

CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    token_hash   BLOB NOT NULL UNIQUE,
    csrf_hash    BLOB NOT NULL,
    created_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    revoked_at   TEXT NULL,
    user_agent   TEXT NULL
);

CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Session is one server-managed login session. Only token and CSRF hashes are
// persisted; raw values belong to the HTTP client.
type Session struct {
	ID         string
	TokenHash  []byte
	CSRFHash   []byte
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	UserAgent  string
}

const createSessionQuery = `INSERT INTO sessions
	(id, token_hash, csrf_hash, created_at, last_seen_at, expires_at, revoked_at, user_agent)
VALUES (?, ?, ?, ?, ?, ?, NULL, ?)`

const getSessionByTokenHashQuery = `SELECT id, token_hash, csrf_hash, created_at, last_seen_at, expires_at, revoked_at, user_agent
FROM sessions WHERE token_hash = ?`

const touchSessionQuery = `UPDATE sessions SET last_seen_at = ? WHERE id = ? AND revoked_at IS NULL`

const revokeSessionQuery = `UPDATE sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`

const revokeAllSessionsQuery = `UPDATE sessions SET revoked_at = ? WHERE revoked_at IS NULL`

const revokeOtherSessionsQuery = `UPDATE sessions SET revoked_at = ? WHERE revoked_at IS NULL AND id <> ?`

// CreateSession persists a new session.
func (s *Store) CreateSession(ctx context.Context, session Session) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	if strings.TrimSpace(session.ID) == "" {
		return errors.New("store: session id must not be empty")
	}
	if len(session.TokenHash) != sha256.Size {
		return fmt.Errorf("store: session token hash must be %d bytes", sha256.Size)
	}
	if len(session.CSRFHash) != sha256.Size {
		return fmt.Errorf("store: session CSRF hash must be %d bytes", sha256.Size)
	}
	if session.CreatedAt.IsZero() || session.LastSeenAt.IsZero() || session.ExpiresAt.IsZero() {
		return errors.New("store: session timestamps must be set")
	}
	if !session.ExpiresAt.After(session.CreatedAt) {
		return errors.New("store: session expiry must be after creation")
	}

	var userAgent any
	if session.UserAgent != "" {
		userAgent = session.UserAgent
	}
	if _, err := db.ExecContext(ctx, createSessionQuery,
		session.ID,
		session.TokenHash,
		session.CSRFHash,
		formatTime(session.CreatedAt),
		formatTime(session.LastSeenAt),
		formatTime(session.ExpiresAt),
		userAgent,
	); err != nil {
		return wrap("create session", err)
	}
	return nil
}

// GetSessionByTokenHash finds a session by the SHA-256 of its raw token. A
// revoked session is still returned so the caller can distinguish revocation
// from absence.
func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, error) {
	db, err := s.executor()
	if err != nil {
		return Session{}, err
	}
	if len(tokenHash) != sha256.Size {
		return Session{}, fmt.Errorf("store: session token hash must be %d bytes", sha256.Size)
	}

	var session Session
	var createdAt, lastSeenAt, expiresAt string
	var revokedAt sql.NullString
	var userAgent sql.NullString
	if err := db.QueryRowContext(ctx, getSessionByTokenHashQuery, tokenHash).Scan(
		&session.ID,
		&session.TokenHash,
		&session.CSRFHash,
		&createdAt,
		&lastSeenAt,
		&expiresAt,
		&revokedAt,
		&userAgent,
	); err != nil {
		return Session{}, wrap("get session", err)
	}

	session.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Session{}, err
	}
	session.LastSeenAt, err = parseTime(lastSeenAt)
	if err != nil {
		return Session{}, err
	}
	session.ExpiresAt, err = parseTime(expiresAt)
	if err != nil {
		return Session{}, err
	}
	if revokedAt.Valid {
		parsed, err := parseTime(revokedAt.String)
		if err != nil {
			return Session{}, err
		}
		session.RevokedAt = &parsed
	}
	if userAgent.Valid {
		session.UserAgent = userAgent.String
	}
	return session, nil
}

// TouchSession updates last_seen_at for an active session.
func (s *Store) TouchSession(ctx context.Context, id string, lastSeen time.Time) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("store: session id must not be empty")
	}
	if lastSeen.IsZero() {
		return errors.New("store: session last_seen_at must be set")
	}
	result, err := db.ExecContext(ctx, touchSessionQuery, formatTime(lastSeen), id)
	if err != nil {
		return wrap("touch session", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: touch session rows: %w", err)
	}
	if updated == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeSession revokes one session. It is idempotent: revoking a missing or
// already-revoked session succeeds.
func (s *Store) RevokeSession(ctx context.Context, id string, revokedAt time.Time) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("store: session id must not be empty")
	}
	if revokedAt.IsZero() {
		return errors.New("store: session revocation time must be set")
	}
	if _, err := db.ExecContext(ctx, revokeSessionQuery, formatTime(revokedAt), id); err != nil {
		return wrap("revoke session", err)
	}
	return nil
}

// RevokeAllSessions revokes every active session.
func (s *Store) RevokeAllSessions(ctx context.Context, revokedAt time.Time) (int64, error) {
	db, err := s.executor()
	if err != nil {
		return 0, err
	}
	if revokedAt.IsZero() {
		return 0, errors.New("store: session revocation time must be set")
	}
	result, err := db.ExecContext(ctx, revokeAllSessionsQuery, formatTime(revokedAt))
	if err != nil {
		return 0, wrap("revoke all sessions", err)
	}
	revoked, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: revoke all sessions rows: %w", err)
	}
	return revoked, nil
}

// RevokeOtherSessions revokes every active session except currentID.
func (s *Store) RevokeOtherSessions(ctx context.Context, currentID string, revokedAt time.Time) (int64, error) {
	db, err := s.executor()
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(currentID) == "" {
		return 0, errors.New("store: current session id must not be empty")
	}
	if revokedAt.IsZero() {
		return 0, errors.New("store: session revocation time must be set")
	}
	result, err := db.ExecContext(ctx, revokeOtherSessionsQuery, formatTime(revokedAt), currentID)
	if err != nil {
		return 0, wrap("revoke other sessions", err)
	}
	revoked, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: revoke other sessions rows: %w", err)
	}
	return revoked, nil
}

package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// User is the single owner account. The schema enforces id = 1.
type User struct {
	ID                int64
	PasswordHash      string
	PasswordChangedAt time.Time
	CreatedAt         time.Time
}

const getUserQuery = `SELECT id, password_hash, password_changed_at, created_at FROM users WHERE id = 1`

const upsertUserQuery = `INSERT INTO users (id, password_hash, password_changed_at, created_at)
VALUES (1, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	password_hash = excluded.password_hash,
	password_changed_at = excluded.password_changed_at`

const updateUserPasswordQuery = `UPDATE users
SET password_hash = ?, password_changed_at = ?
WHERE id = 1 AND password_changed_at = ?`

// GetUser returns the owner account, or ErrNotFound when no password is set.
func (s *Store) GetUser(ctx context.Context) (User, error) {
	db, err := s.executor()
	if err != nil {
		return User{}, err
	}

	var user User
	var changedAt, createdAt string
	if err := db.QueryRowContext(ctx, getUserQuery).Scan(
		&user.ID,
		&user.PasswordHash,
		&changedAt,
		&createdAt,
	); err != nil {
		return User{}, wrap("get user", err)
	}
	user.PasswordChangedAt, err = parseTime(changedAt)
	if err != nil {
		return User{}, err
	}
	user.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return User{}, err
	}
	return user, nil
}

// UpsertUser creates or replaces the owner password hash while preserving the
// original created_at value.
func (s *Store) UpsertUser(ctx context.Context, passwordHash string, now time.Time) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	if strings.TrimSpace(passwordHash) == "" {
		return errors.New("store: password hash must not be empty")
	}
	if now.IsZero() {
		return errors.New("store: user timestamp must be set")
	}
	if _, err := db.ExecContext(ctx, upsertUserQuery,
		passwordHash,
		formatTime(now),
		formatTime(now),
	); err != nil {
		return wrap("upsert user", err)
	}
	return nil
}

// UpdateUserPassword updates the hash only when password_changed_at still
// matches expectedChangedAt. It returns ErrConflict when another password
// change won the race.
func (s *Store) UpdateUserPassword(ctx context.Context, passwordHash string, changedAt, expectedChangedAt time.Time) error {
	db, err := s.executor()
	if err != nil {
		return err
	}
	if strings.TrimSpace(passwordHash) == "" {
		return errors.New("store: password hash must not be empty")
	}
	if changedAt.IsZero() || expectedChangedAt.IsZero() {
		return errors.New("store: password timestamps must be set")
	}

	result, err := db.ExecContext(ctx, updateUserPasswordQuery,
		passwordHash,
		formatTime(changedAt),
		formatTime(expectedChangedAt),
	)
	if err != nil {
		return wrap("update user password", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update user rows: %w", err)
	}
	if updated == 0 {
		return ErrConflict
	}
	return nil
}

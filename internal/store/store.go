// Package store provides the repository layer for Neda's SQLite database.
//
// Repositories accept a small database interface so the same methods work on a
// *sql.DB or inside a transaction. Callers that need atomic multi-repository
// work use WithTx; the callback must not perform network or long CPU work
// because SQLite is configured with a single writer.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound reports that a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

// ErrConflict reports that a conditional update did not match the expected row.
var ErrConflict = errors.New("store: conflict")

// ErrNestedTx reports an attempt to begin a transaction inside another one.
var ErrNestedTx = errors.New("store: nested transactions are not supported")

// ErrNilDB reports that a Store was constructed without a database.
var ErrNilDB = errors.New("store: nil database")

type dbtx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Store is a repository handle. New creates a root handle backed by a
// *sql.DB; WithTx creates a transaction-scoped handle.
type Store struct {
	db dbtx
}

// New returns a Store backed by db.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// WithTx runs fn in a transaction and commits only when fn returns nil. A
// returned error or panic rolls the transaction back. Nested calls are
// rejected because SQLite does not support nested transactions.
func (s *Store) WithTx(ctx context.Context, fn func(*Store) error) error {
	if s == nil || s.db == nil {
		return ErrNilDB
	}
	db, ok := s.db.(*sql.DB)
	if !ok {
		return ErrNestedTx
	}
	if fn == nil {
		return errors.New("store: nil transaction callback")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin transaction: %w", err)
	}

	txStore := &Store{db: tx}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := fn(txStore); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit transaction: %w", err)
	}
	committed = true
	return nil
}

func (s *Store) executor() (dbtx, error) {
	if s == nil || s.db == nil {
		return nil, ErrNilDB
	}
	return s.db, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(raw string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: parse timestamp: %w", err)
	}
	return parsed.UTC(), nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("store: %s: %w", operation, err)
}

// Package db opens the authoritative SQLite database and applies migrations.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// DB wraps the single-writer SQLite connection.
type DB struct {
	*sql.DB
	path string
}

// Open opens path with Neda's required PRAGMAs applied to every connection.
func Open(ctx context.Context, path string) (*DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("db: database path must not be empty")
	}
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if dir := filepath.Dir(path); dir != "." && dir != string(filepath.Separator) {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, fmt.Errorf("db: create database directory: %w", err)
			}
		}
	}

	sqlDB, err := sql.Open("sqlite", dataSourceName(path))
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return &DB{DB: sqlDB, path: path}, nil
}

// Path returns the configured database path.
func (d *DB) Path() string {
	return d.path
}

// Ready verifies that the database is reachable and the expected number of
// migrations is applied. It intentionally returns no internal detail.
func (d *DB) Ready(ctx context.Context, expectedMigrations int) error {
	if err := d.PingContext(ctx); err != nil {
		return fmt.Errorf("db: not ready: %w", err)
	}
	var count int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		return fmt.Errorf("db: migrations not ready: %w", err)
	}
	if count != expectedMigrations {
		return fmt.Errorf("db: migration count %d, expected %d", count, expectedMigrations)
	}
	return nil
}

func dataSourceName(path string) string {
	values := url.Values{}
	for _, pragma := range []string{
		"foreign_keys(1)",
		"journal_mode(WAL)",
		"synchronous(FULL)",
		"busy_timeout(5000)",
		"wal_autocheckpoint(1000)",
		"journal_size_limit(67108864)",
	} {
		values.Add("_pragma", pragma)
	}
	parsed := &url.URL{Scheme: "file", Path: path}
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

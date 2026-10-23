package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"time"
)

var migrationName = regexp.MustCompile(`^(\d{4})_([A-Za-z0-9_-]+)\.sql$`)

type migration struct {
	version  int
	name     string
	sql      string
	checksum string
}

// Migrate applies all pending migrations in version order. It is safe to call
// repeatedly; applied files are verified by checksum and never edited.
func Migrate(ctx context.Context, database *sql.DB, fsys fs.FS) (int, error) {
	migrations, err := loadMigrations(fsys)
	if err != nil {
		return 0, err
	}
	if err := ensureSchemaMigrations(ctx, database); err != nil {
		return 0, err
	}
	applied, err := loadApplied(ctx, database)
	if err != nil {
		return 0, err
	}

	for _, m := range migrations {
		existing, ok := applied[m.version]
		if ok {
			if existing.name != m.name || existing.checksum != m.checksum {
				return 0, fmt.Errorf("db: migration %04d %s does not match applied migration %s", m.version, m.name, existing.name)
			}
			delete(applied, m.version)
			continue
		}
		if err := applyMigration(ctx, database, m); err != nil {
			return 0, err
		}
	}

	if len(applied) > 0 {
		versions := make([]int, 0, len(applied))
		for version := range applied {
			versions = append(versions, version)
		}
		sort.Ints(versions)
		return 0, fmt.Errorf("db: database has migrations not present in this binary: %v", versions)
	}

	if err := foreignKeyCheck(ctx, database); err != nil {
		return 0, err
	}
	return len(migrations), nil
}

func ensureSchemaMigrations(ctx context.Context, database *sql.DB) error {
	const query = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		checksum   TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`
	if _, err := database.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("db: create schema_migrations: %w", err)
	}
	return nil
}

func loadMigrations(fsys fs.FS) ([]migration, error) {
	if fsys == nil {
		return nil, errors.New("db: nil migration filesystem")
	}
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("db: list migrations: %w", err)
	}
	sort.Strings(names)
	migrations := make([]migration, 0, len(names))
	seen := make(map[int]string, len(names))
	for _, name := range names {
		match := migrationName.FindStringSubmatch(name)
		if match == nil {
			return nil, fmt.Errorf("db: invalid migration filename %q", name)
		}
		version, err := strconv.Atoi(match[1])
		if err != nil || version < 1 {
			return nil, fmt.Errorf("db: invalid migration version in %q", name)
		}
		if previous, exists := seen[version]; exists {
			return nil, fmt.Errorf("db: duplicate migration version %d (%s and %s)", version, previous, name)
		}
		seen[version] = name
		contents, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("db: read migration %s: %w", name, err)
		}
		sum := sha256.Sum256(contents)
		migrations = append(migrations, migration{
			version:  version,
			name:     name,
			sql:      string(contents),
			checksum: hex.EncodeToString(sum[:]),
		})
	}
	if len(migrations) == 0 {
		return nil, errors.New("db: no migrations found")
	}
	return migrations, nil
}

type appliedMigration struct {
	name     string
	checksum string
}

func loadApplied(ctx context.Context, database *sql.DB) (map[int]appliedMigration, error) {
	rows, err := database.QueryContext(ctx, `SELECT version, name, checksum FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("db: read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]appliedMigration)
	for rows.Next() {
		var version int
		var record appliedMigration
		if err := rows.Scan(&version, &record.name, &record.checksum); err != nil {
			return nil, fmt.Errorf("db: scan schema_migrations: %w", err)
		}
		applied[version] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: iterate schema_migrations: %w", err)
	}
	return applied, nil
}

func applyMigration(ctx context.Context, database *sql.DB, m migration) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin migration %s: %w", m.name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("db: apply migration %s: %w", m.name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES (?, ?, ?, ?)`,
		m.version, m.name, m.checksum, time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("db: record migration %s: %w", m.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit migration %s: %w", m.name, err)
	}
	return nil
}

func foreignKeyCheck(ctx context.Context, database *sql.DB) error {
	rows, err := database.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("db: foreign_key_check: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var table string
		var rowID sql.NullInt64
		var parent string
		var fkID sql.NullInt64
		if err := rows.Scan(&table, &rowID, &parent, &fkID); err != nil {
			return fmt.Errorf("db: scan foreign_key_check: %w", err)
		}
		return fmt.Errorf("db: foreign key violation in table %s referencing %s", table, parent)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("db: iterate foreign_key_check: %w", err)
	}
	return nil
}

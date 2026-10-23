// Package migrations contains the embedded forward-only SQL migrations.
package migrations

import "embed"

// FS contains the sorted .sql migration files.
//
//go:embed *.sql
var FS embed.FS

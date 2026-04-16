// Package store is the SQLite-backed source of truth for the daemon. It
// wraps modernc.org/sqlite (pure-Go driver; no cgo) and uses goose with
// embedded migrations.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Repo is the daemon's persistence layer. Construct with Open, close via
// Close.
type Repo struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and returns a *Repo.
// It does NOT run migrations; call Migrate for that.
func Open(path string) (*Repo, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("db.Ping: %w", err)
	}
	// SQLite is single-writer; a small pool is fine and avoids lock churn.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return &Repo{db: db}, nil
}

// Close shuts down the underlying *sql.DB.
func (r *Repo) Close() error { return r.db.Close() }

// Migrate runs the embedded migrations. Safe to call on every boot.
func (r *Repo) Migrate(ctx context.Context) error {
	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("sqlite"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, r.db, "migrations"); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

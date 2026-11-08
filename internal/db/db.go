// Package db opens the PostgreSQL pool and applies schema migrations.
//
// All v2 tables live in their own schema (Schema), so they can sit next to the
// old Fever tables in the same database. Connections use it as search_path, so
// queries never need to qualify table names.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Schema is the PostgreSQL schema holding every v2 table.
const Schema = "reader"

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Open connects to PostgreSQL with a small pool: on a low-end server every
// idle backend costs several MB of RAM on the database side.
func Open(ctx context.Context, databaseURL string, maxConns int32) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, errors.New("DATABASE_URL is not set")
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.RuntimeParams["search_path"] = Schema
	cfg.ConnConfig.RuntimeParams["application_name"] = "fever-v2"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	return pool, nil
}

// Migrate creates the schema if needed and applies pending migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	if err := createSchema(ctx, pool); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	files, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	// An advisory lock makes concurrent starts (the server restarting while
	// an import runs, say) apply migrations one at a time.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, files, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	for _, r := range results {
		log.Info("applied migration", "version", r.Source.Version, "file", r.Source.Path, "took", r.Duration.Round(time.Millisecond))
	}
	return nil
}

// createSchema creates the v2 schema. CREATE SCHEMA IF NOT EXISTS can still
// fail when two processes run it at the same moment, so it runs under a
// transaction-level advisory lock.
func createSchema(ctx context.Context, pool *pgxpool.Pool) error {
	const lockID = 0x66657665 // "feve"
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{Schema}.Sanitize()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

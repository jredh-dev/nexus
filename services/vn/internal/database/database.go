// Package database provides PostgreSQL access for the vn visual novel engine.
//
// It manages connection pooling via pgxpool, schema migrations, and CRUD
// operations for videos (stored as large objects), significant events,
// subtitles (with toggle-point visibility), readers, and votes.
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgxpool.Pool and provides domain-specific operations.
type DB struct {
	Pool *pgxpool.Pool
}

// New connects to PostgreSQL and runs migrations.
// connStr is a libpq-style connection string or DSN, e.g.:
//
//	"host=/tmp/ctl-pg dbname=vn user=jredh"
func New(ctx context.Context, connStr string) (*DB, error) {
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	db := &DB{Pool: pool}
	if err := db.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	return db, nil
}

// Close shuts down the connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}

// ResetAll truncates all data tables (not schema_version).
// This is a destructive operation intended for integration testing.
// It uses TRUNCATE ... CASCADE to handle foreign key dependencies,
// then cleans up any orphaned large objects left behind by deleted videos.
func (db *DB) ResetAll(ctx context.Context) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin reset tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Truncate all data tables. CASCADE handles FK dependencies.
	_, err = tx.Exec(ctx, `TRUNCATE votes, readers, subtitles, significant_events, videos CASCADE`)
	if err != nil {
		return fmt.Errorf("truncate tables: %w", err)
	}

	// Clean up orphaned large objects. Since videos is now empty, every
	// large object in pg_largeobject_metadata is orphaned.
	_, err = tx.Exec(ctx, `SELECT lo_unlink(oid) FROM pg_largeobject_metadata`)
	if err != nil {
		return fmt.Errorf("unlink large objects: %w", err)
	}

	return tx.Commit(ctx)
}

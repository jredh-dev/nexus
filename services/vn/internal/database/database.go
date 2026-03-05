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

// Package database provides PostgreSQL access for the discord-monitor service.
//
// It manages connection pooling via pgxpool, schema migrations, and CRUD
// operations for guilds, channels, messages, read cursors, activity
// aggregations, keywords, and digests.
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgxpool.Pool and provides domain-specific operations
// for the discord-monitor service.
type DB struct {
	Pool *pgxpool.Pool
}

// New connects to PostgreSQL, verifies the connection, and runs migrations.
// connStr is a libpq-style connection string or DSN, e.g.:
//
//	"host=localhost port=5432 dbname=discord_monitor user=jredh"
func New(ctx context.Context, connStr string) (*DB, error) {
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	// Verify the connection is actually usable before proceeding.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	db := &DB{Pool: pool}

	// Run all schema migrations in a single transaction.
	if err := db.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return db, nil
}

// Close shuts down the connection pool. Safe to call multiple times.
func (db *DB) Close() {
	db.Pool.Close()
}

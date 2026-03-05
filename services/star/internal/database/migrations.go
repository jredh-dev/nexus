package database

import (
	"context"
	"fmt"
)

// Schema version. Bump when adding new migrations.
const schemaVersion = 1

// migrations are applied in order. Each is idempotent (IF NOT EXISTS).
var migrations = []string{
	// --- v1: core tables ---

	// Videos: raw binary stored as PostgreSQL large objects.
	// data_oid references an entry in pg_largeobject via lo_create/lo_open.
	`CREATE TABLE IF NOT EXISTS videos (
		video_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name        TEXT NOT NULL,
		codec       TEXT NOT NULL,
		mime_type   TEXT NOT NULL,
		duration_ms INTEGER NOT NULL,
		width       INTEGER,
		height      INTEGER,
		loop_type   TEXT NOT NULL DEFAULT 'none'
			CHECK (loop_type IN ('none', 'forward', 'palindrome')),
		has_subtitles_already BOOLEAN NOT NULL DEFAULT FALSE,
		data_oid    OID NOT NULL,
		size_bytes  BIGINT NOT NULL,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,

	// Significant events within a video timeline.
	// timestamps[] holds seconds into the video when sub-events occur
	// (e.g. "Harry found the stone" at [12.5], "used it" at [45.0]).
	`CREATE TABLE IF NOT EXISTS significant_events (
		event_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		video_id    UUID NOT NULL REFERENCES videos(video_id) ON DELETE CASCADE,
		description TEXT NOT NULL,
		timestamps  DOUBLE PRECISION[] NOT NULL,
		is_visible  BOOLEAN NOT NULL DEFAULT TRUE,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,

	// Subtitles with toggle-point visibility model.
	//
	// timestamps_visible[] contains "toggle points" in seconds.
	// The subtitle alternates between visible/invisible at each point.
	//
	//   initialize_visible = true  → visible from t=0, first timestamp = disappear
	//   initialize_visible = false → invisible from t=0, first timestamp = appear
	//   end_visible = true         → must end in visible state
	//   end_visible = false        → must end in invisible state
	//
	// Constraint on timestamp count:
	//   same init/end   → even count (0 valid: always-on or always-off)
	//   different       → odd count  (at least 1 toggle required)
	`CREATE TABLE IF NOT EXISTS subtitles (
		subtitle_id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		video_id            UUID NOT NULL REFERENCES videos(video_id) ON DELETE CASCADE,
		event_id            UUID REFERENCES significant_events(event_id) ON DELETE SET NULL,
		text                TEXT NOT NULL,
		timestamps_visible  DOUBLE PRECISION[] NOT NULL DEFAULT '{}',
		initialize_visible  BOOLEAN NOT NULL DEFAULT FALSE,
		end_visible         BOOLEAN NOT NULL DEFAULT FALSE,
		sort_order          INTEGER NOT NULL DEFAULT 0,
		created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
		CONSTRAINT valid_visibility_timestamps CHECK (
			CASE
				WHEN initialize_visible = end_visible
					THEN coalesce(array_length(timestamps_visible, 1), 0) % 2 = 0
				ELSE coalesce(array_length(timestamps_visible, 1), 0) % 2 = 1
			END
		)
	)`,

	// Readers: anonymous viewers identified by device fingerprint.
	// tokens accumulate as chapters are read (1 token per chapter).
	`CREATE TABLE IF NOT EXISTS readers (
		reader_id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		device_hash TEXT UNIQUE NOT NULL,
		tokens      INTEGER NOT NULL DEFAULT 0,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,

	// Votes: readers spend tokens to vote on story choices.
	`CREATE TABLE IF NOT EXISTS votes (
		vote_id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		reader_id    UUID NOT NULL REFERENCES readers(reader_id) ON DELETE CASCADE,
		chapter_id   TEXT NOT NULL,
		choice       TEXT NOT NULL,
		tokens_spent INTEGER NOT NULL DEFAULT 1 CHECK (tokens_spent > 0),
		created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,

	// Indexes for common queries.
	`CREATE INDEX IF NOT EXISTS idx_significant_events_video
		ON significant_events(video_id)`,
	`CREATE INDEX IF NOT EXISTS idx_subtitles_video
		ON subtitles(video_id)`,
	`CREATE INDEX IF NOT EXISTS idx_subtitles_event
		ON subtitles(event_id)`,
	`CREATE INDEX IF NOT EXISTS idx_votes_chapter
		ON votes(chapter_id)`,
	`CREATE INDEX IF NOT EXISTS idx_votes_reader
		ON votes(reader_id)`,

	// Schema version tracking.
	`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
}

// migrate runs all migrations inside a transaction.
func (db *DB) migrate(ctx context.Context) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for i, m := range migrations {
		if _, err := tx.Exec(ctx, m); err != nil {
			return fmt.Errorf("migration %d: %w", i, err)
		}
	}

	// Record schema version (upsert). pgx does not allow multiple
	// statements in a single Exec, so we split into two calls.
	if _, err = tx.Exec(ctx, `DELETE FROM schema_version`); err != nil {
		return fmt.Errorf("clear schema version: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO schema_version (version) VALUES ($1)`, schemaVersion); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}

	return tx.Commit(ctx)
}

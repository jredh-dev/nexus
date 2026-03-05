// Package database provides PostgreSQL access for the discord-monitor service.
//
// It manages connection pooling via pgxpool, schema migrations, and CRUD
// operations for guilds, channels, messages, read cursors, activity
// aggregations, keywords, and digests.
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

	// Guilds: Discord servers being tracked.
	// mode indicates how we connect — 'bot' uses the official Bot API with
	// gateway intents, 'selfbot' uses browser-mimicking HTTP requests.
	// owner_id and member_count are cached from the Discord API for display.
	`CREATE TABLE IF NOT EXISTS guilds (
		guild_id       TEXT PRIMARY KEY,
		name           TEXT NOT NULL,
		icon_hash      TEXT NOT NULL DEFAULT '',
		mode           TEXT NOT NULL CHECK (mode IN ('bot', 'selfbot')),
		owner_id       TEXT NOT NULL DEFAULT '',
		member_count   INTEGER NOT NULL DEFAULT 0,
		is_active      BOOLEAN NOT NULL DEFAULT TRUE,
		last_synced_at TIMESTAMPTZ,
		created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,

	// Channels: individual text channels within a guild.
	// type mirrors Discord's channel type enum (0 = GUILD_TEXT, 2 = GUILD_VOICE, etc.).
	// parent_id is the category channel ID (null for top-level channels).
	// is_monitored controls whether we poll this channel for new messages.
	`CREATE TABLE IF NOT EXISTS channels (
		channel_id     TEXT PRIMARY KEY,
		guild_id       TEXT NOT NULL REFERENCES guilds(guild_id) ON DELETE CASCADE,
		name           TEXT NOT NULL,
		type           INTEGER NOT NULL DEFAULT 0,
		parent_id      TEXT,
		position       INTEGER NOT NULL DEFAULT 0,
		is_monitored   BOOLEAN NOT NULL DEFAULT TRUE,
		last_synced_at TIMESTAMPTZ,
		created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,

	// Read cursors: per-channel bookmark tracking the last-read message.
	// Used to resume polling from where we left off across restarts.
	// Only one cursor per channel — we always read forward.
	`CREATE TABLE IF NOT EXISTS read_cursors (
		channel_id       TEXT PRIMARY KEY REFERENCES channels(channel_id) ON DELETE CASCADE,
		last_read_msg_id TEXT NOT NULL,
		updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,

	// Messages: sampled message snapshots (not a full archive).
	// We store enough to display recent activity, run keyword matching,
	// and generate digests. Old messages are pruned periodically.
	// mentions_roles stores role IDs as a text array for keyword/role matching.
	`CREATE TABLE IF NOT EXISTS messages (
		message_id      TEXT PRIMARY KEY,
		channel_id      TEXT NOT NULL REFERENCES channels(channel_id) ON DELETE CASCADE,
		author_id       TEXT NOT NULL,
		author_name     TEXT NOT NULL DEFAULT '',
		content         TEXT NOT NULL DEFAULT '',
		has_embeds      BOOLEAN NOT NULL DEFAULT FALSE,
		has_attachments BOOLEAN NOT NULL DEFAULT FALSE,
		mentions_me     BOOLEAN NOT NULL DEFAULT FALSE,
		mentions_roles  TEXT[] NOT NULL DEFAULT '{}',
		created_at      TIMESTAMPTZ NOT NULL,
		stored_at       TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,

	// Activity hourly: aggregated message counts per channel per hour.
	// Used for heatmap visualization — "when is this channel most active?"
	// Composite PK ensures one row per channel per hour bucket.
	`CREATE TABLE IF NOT EXISTS activity_hourly (
		channel_id     TEXT NOT NULL REFERENCES channels(channel_id) ON DELETE CASCADE,
		hour_bucket    TIMESTAMPTZ NOT NULL,
		message_count  INTEGER NOT NULL DEFAULT 0,
		unique_authors INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (channel_id, hour_bucket)
	)`,

	// Keywords: watchlist patterns for priority scoring.
	// Patterns can be plain text (substring match) or regex (is_regex = true).
	// guild_id is optional — NULL means the keyword applies to all guilds.
	// priority 0-100, higher = more important for digest ranking.
	`CREATE TABLE IF NOT EXISTS keywords (
		keyword_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		pattern    TEXT NOT NULL,
		is_regex   BOOLEAN NOT NULL DEFAULT FALSE,
		guild_id   TEXT REFERENCES guilds(guild_id) ON DELETE CASCADE,
		priority   INTEGER NOT NULL DEFAULT 50,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,

	// Digests: generated summaries of activity over a time period.
	// content is JSONB containing structured summary data (top channels,
	// keyword hits, activity stats, notable messages).
	`CREATE TABLE IF NOT EXISTS digests (
		digest_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		guild_id     TEXT REFERENCES guilds(guild_id) ON DELETE CASCADE,
		period_start TIMESTAMPTZ NOT NULL,
		period_end   TIMESTAMPTZ NOT NULL,
		content      JSONB NOT NULL,
		created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,

	// Schema version tracking — same pattern as vn service.
	`CREATE TABLE IF NOT EXISTS schema_version (
		version    INTEGER NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,

	// --- Indexes for common query patterns ---

	// Channels by guild: listing all channels in a server.
	`CREATE INDEX IF NOT EXISTS idx_channels_guild
		ON channels(guild_id)`,

	// Messages by channel + time: reading messages in order for a channel.
	`CREATE INDEX IF NOT EXISTS idx_messages_channel_created
		ON messages(channel_id, created_at DESC)`,

	// Messages by author: finding all messages from a specific user.
	`CREATE INDEX IF NOT EXISTS idx_messages_author
		ON messages(author_id)`,

	// Activity by channel: time-series queries for heatmaps.
	`CREATE INDEX IF NOT EXISTS idx_activity_hourly_channel
		ON activity_hourly(channel_id, hour_bucket DESC)`,

	// Keywords by guild: looking up watchlist for a specific server.
	`CREATE INDEX IF NOT EXISTS idx_keywords_guild
		ON keywords(guild_id)`,

	// Digests by guild + time: finding summaries for a server.
	`CREATE INDEX IF NOT EXISTS idx_digests_guild_period
		ON digests(guild_id, period_start DESC)`,
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

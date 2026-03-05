package database

import (
	"context"
	"fmt"
	"time"
)

// Channel represents a Discord channel within a tracked guild.
type Channel struct {
	ChannelID    string     `json:"channel_id"`
	GuildID      string     `json:"guild_id"`
	Name         string     `json:"name"`
	Type         int        `json:"type"` // Discord channel type: 0=text, 2=voice, etc.
	ParentID     *string    `json:"parent_id,omitempty"` // category channel ID
	Position     int        `json:"position"`
	IsMonitored  bool       `json:"is_monitored"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// UpsertChannel inserts or updates a channel from Discord API data.
// Called during guild sync to refresh the channel list. The is_monitored
// flag is preserved on conflict — it's controlled separately via
// SetChannelMonitored.
func (db *DB) UpsertChannel(ctx context.Context, ch Channel) (*Channel, error) {
	var result Channel
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO channels (channel_id, guild_id, name, type, parent_id, position, last_synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (channel_id) DO UPDATE SET
			name           = EXCLUDED.name,
			type           = EXCLUDED.type,
			parent_id      = EXCLUDED.parent_id,
			position       = EXCLUDED.position,
			last_synced_at = now()
		RETURNING channel_id, guild_id, name, type, parent_id, position,
		          is_monitored, last_synced_at, created_at`,
		ch.ChannelID, ch.GuildID, ch.Name, ch.Type, ch.ParentID, ch.Position,
	).Scan(
		&result.ChannelID, &result.GuildID, &result.Name, &result.Type,
		&result.ParentID, &result.Position, &result.IsMonitored,
		&result.LastSyncedAt, &result.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert channel %s: %w", ch.ChannelID, err)
	}
	return &result, nil
}

// ListChannels returns all channels for a guild, ordered by position.
// If monitoredOnly is true, only channels with is_monitored=true are returned.
func (db *DB) ListChannels(ctx context.Context, guildID string, monitoredOnly bool) ([]Channel, error) {
	query := `
		SELECT channel_id, guild_id, name, type, parent_id, position,
		       is_monitored, last_synced_at, created_at
		FROM channels
		WHERE guild_id = $1`
	if monitoredOnly {
		query += ` AND is_monitored = TRUE`
	}
	query += ` ORDER BY position ASC`

	rows, err := db.Pool.Query(ctx, query, guildID)
	if err != nil {
		return nil, fmt.Errorf("list channels for guild %s: %w", guildID, err)
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		if err := rows.Scan(
			&ch.ChannelID, &ch.GuildID, &ch.Name, &ch.Type,
			&ch.ParentID, &ch.Position, &ch.IsMonitored,
			&ch.LastSyncedAt, &ch.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan channel row: %w", err)
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// SetChannelMonitored enables or disables message polling for a channel.
// Unmonitored channels are skipped during scan loops but their existing
// data is preserved.
func (db *DB) SetChannelMonitored(ctx context.Context, channelID string, monitored bool) error {
	tag, err := db.Pool.Exec(ctx, `
		UPDATE channels SET is_monitored = $2
		WHERE channel_id = $1`, channelID, monitored)
	if err != nil {
		return fmt.Errorf("set channel %s monitored=%v: %w", channelID, monitored, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("channel %s not found", channelID)
	}
	return nil
}

package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReadCursor tracks the last-read message position for a channel.
// Used to resume polling from where we left off across restarts.
type ReadCursor struct {
	ChannelID     string    `json:"channel_id"`
	LastReadMsgID string    `json:"last_read_msg_id"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// GetCursor returns the read cursor for a channel.
// Returns nil (not an error) if no cursor exists yet — the caller should
// treat this as "start from the beginning" or "start from now" depending
// on the scan mode.
func (db *DB) GetCursor(ctx context.Context, channelID string) (*ReadCursor, error) {
	var c ReadCursor
	err := db.Pool.QueryRow(ctx, `
		SELECT channel_id, last_read_msg_id, updated_at
		FROM read_cursors WHERE channel_id = $1`, channelID,
	).Scan(&c.ChannelID, &c.LastReadMsgID, &c.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cursor for channel %s: %w", channelID, err)
	}
	return &c, nil
}

// SetCursor upserts the read cursor for a channel to a specific message ID.
// This is the general-purpose cursor update — use AdvanceCursor when you
// want to automatically set it to the latest stored message.
func (db *DB) SetCursor(ctx context.Context, channelID, messageID string) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO read_cursors (channel_id, last_read_msg_id, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (channel_id) DO UPDATE SET
			last_read_msg_id = EXCLUDED.last_read_msg_id,
			updated_at       = now()`,
		channelID, messageID,
	)
	if err != nil {
		return fmt.Errorf("set cursor for channel %s to %s: %w", channelID, messageID, err)
	}
	return nil
}

// AdvanceCursor sets the cursor to the latest (max) message_id stored for
// the given channel. This is called after a successful poll cycle to mark
// all fetched messages as "read".
// If no messages exist for the channel, the cursor is not modified.
func (db *DB) AdvanceCursor(ctx context.Context, channelID string) error {
	// Find the newest message_id in the channel. Discord snowflake IDs are
	// monotonically increasing, so MAX gives us the latest message.
	var maxID *string
	err := db.Pool.QueryRow(ctx, `
		SELECT MAX(message_id) FROM messages WHERE channel_id = $1`,
		channelID,
	).Scan(&maxID)
	if err != nil {
		return fmt.Errorf("find max message_id for channel %s: %w", channelID, err)
	}

	// No messages stored yet — nothing to advance to.
	if maxID == nil {
		return nil
	}

	return db.SetCursor(ctx, channelID, *maxID)
}

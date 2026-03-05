package database

import (
	"context"
	"fmt"
	"time"
)

// Message represents a sampled Discord message snapshot.
type Message struct {
	MessageID      string    `json:"message_id"`
	ChannelID      string    `json:"channel_id"`
	AuthorID       string    `json:"author_id"`
	AuthorName     string    `json:"author_name"`
	Content        string    `json:"content"`
	HasEmbeds      bool      `json:"has_embeds"`
	HasAttachments bool      `json:"has_attachments"`
	MentionsMe     bool      `json:"mentions_me"`
	MentionsRoles  []string  `json:"mentions_roles"`
	CreatedAt      time.Time `json:"created_at"`
	StoredAt       time.Time `json:"stored_at"`
}

// StoreMessages batch-inserts messages using a single transaction.
// Duplicate message_ids are skipped (ON CONFLICT DO NOTHING) so this
// is safe to call with overlapping ranges during catch-up polling.
func (db *DB) StoreMessages(ctx context.Context, msgs []Message) (int64, error) {
	if len(msgs) == 0 {
		return 0, nil
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin store messages tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var stored int64
	for _, m := range msgs {
		tag, err := tx.Exec(ctx, `
			INSERT INTO messages (message_id, channel_id, author_id, author_name,
			                      content, has_embeds, has_attachments,
			                      mentions_me, mentions_roles, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (message_id) DO NOTHING`,
			m.MessageID, m.ChannelID, m.AuthorID, m.AuthorName,
			m.Content, m.HasEmbeds, m.HasAttachments,
			m.MentionsMe, m.MentionsRoles, m.CreatedAt,
		)
		if err != nil {
			return 0, fmt.Errorf("insert message %s: %w", m.MessageID, err)
		}
		stored += tag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit store messages tx: %w", err)
	}
	return stored, nil
}

// GetUnreadMessages returns messages in a channel created after the given
// message ID, ordered by creation time ascending (oldest first).
// Discord message IDs are snowflakes — lexicographic comparison gives
// chronological order for IDs from the same epoch.
// limit caps the result set; 0 means no limit.
func (db *DB) GetUnreadMessages(ctx context.Context, channelID, afterMsgID string, limit int) ([]Message, error) {
	query := `
		SELECT message_id, channel_id, author_id, author_name,
		       content, has_embeds, has_attachments,
		       mentions_me, mentions_roles, created_at, stored_at
		FROM messages
		WHERE channel_id = $1 AND message_id > $2
		ORDER BY created_at ASC`
	if limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, limit)
	}

	rows, err := db.Pool.Query(ctx, query, channelID, afterMsgID)
	if err != nil {
		return nil, fmt.Errorf("get unread messages for channel %s: %w", channelID, err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(
			&m.MessageID, &m.ChannelID, &m.AuthorID, &m.AuthorName,
			&m.Content, &m.HasEmbeds, &m.HasAttachments,
			&m.MentionsMe, &m.MentionsRoles, &m.CreatedAt, &m.StoredAt,
		); err != nil {
			return nil, fmt.Errorf("scan message row: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// GetMessageCount returns the number of messages in a channel since the
// given time. Useful for activity stats and digest generation.
func (db *DB) GetMessageCount(ctx context.Context, channelID string, since time.Time) (int, error) {
	var count int
	err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM messages
		WHERE channel_id = $1 AND created_at >= $2`,
		channelID, since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count messages for channel %s since %v: %w", channelID, since, err)
	}
	return count, nil
}

// PruneOldMessages deletes messages older than the given time.
// Returns the number of rows removed. Called periodically to keep the
// database from growing unbounded — we only need recent messages for
// display and digest generation.
func (db *DB) PruneOldMessages(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := db.Pool.Exec(ctx, `
		DELETE FROM messages WHERE created_at < $1`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("prune messages older than %v: %w", olderThan, err)
	}
	return tag.RowsAffected(), nil
}

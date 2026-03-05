package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// DigestRecord represents a stored digest summary in the database.
// The Content field is a freeform JSON object containing the structured
// summary data (top channels, keyword hits, activity stats, etc.).
type DigestRecord struct {
	ID          string                 `json:"id"`
	GuildID     string                 `json:"guild_id"`
	PeriodStart time.Time              `json:"period_start"`
	PeriodEnd   time.Time              `json:"period_end"`
	Content     map[string]interface{} `json:"content"`
	CreatedAt   time.Time              `json:"created_at"`
}

// StoreDigest saves a generated digest as a JSONB document.
// The digest_id is auto-generated as a UUID. The content map is
// serialized to JSONB for flexible querying.
func (db *DB) StoreDigest(ctx context.Context, guildID string, periodStart, periodEnd time.Time, content map[string]interface{}) error {
	// Serialize content to JSON bytes for the JSONB column.
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("marshal digest content: %w", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO digests (guild_id, period_start, period_end, content)
		VALUES ($1, $2, $3, $4)`,
		guildID, periodStart, periodEnd, contentJSON,
	)
	if err != nil {
		return fmt.Errorf("store digest for guild %s: %w", guildID, err)
	}
	return nil
}

// ListDigests returns recent digests for a guild, ordered by period_start
// descending (newest first). Limit caps the result set; 0 means no limit.
func (db *DB) ListDigests(ctx context.Context, guildID string, limit int) ([]DigestRecord, error) {
	query := `
		SELECT digest_id, guild_id, period_start, period_end, content, created_at
		FROM digests
		WHERE guild_id = $1
		ORDER BY period_start DESC`
	if limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, limit)
	}

	rows, err := db.Pool.Query(ctx, query, guildID)
	if err != nil {
		return nil, fmt.Errorf("list digests for guild %s: %w", guildID, err)
	}
	defer rows.Close()

	var digests []DigestRecord
	for rows.Next() {
		var d DigestRecord
		var contentJSON []byte
		if err := rows.Scan(
			&d.ID, &d.GuildID, &d.PeriodStart, &d.PeriodEnd,
			&contentJSON, &d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan digest row: %w", err)
		}
		// Deserialize JSONB content back into the map.
		if err := json.Unmarshal(contentJSON, &d.Content); err != nil {
			return nil, fmt.Errorf("unmarshal digest content: %w", err)
		}
		digests = append(digests, d)
	}
	return digests, rows.Err()
}

// GetLatestDigest returns the most recent digest for a guild.
// Returns nil (not an error) if no digests exist yet — the caller should
// treat this as "no prior digest" and generate from a default time.
func (db *DB) GetLatestDigest(ctx context.Context, guildID string) (*DigestRecord, error) {
	var d DigestRecord
	var contentJSON []byte
	err := db.Pool.QueryRow(ctx, `
		SELECT digest_id, guild_id, period_start, period_end, content, created_at
		FROM digests
		WHERE guild_id = $1
		ORDER BY period_start DESC
		LIMIT 1`, guildID,
	).Scan(
		&d.ID, &d.GuildID, &d.PeriodStart, &d.PeriodEnd,
		&contentJSON, &d.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get latest digest for guild %s: %w", guildID, err)
	}

	if err := json.Unmarshal(contentJSON, &d.Content); err != nil {
		return nil, fmt.Errorf("unmarshal digest content: %w", err)
	}
	return &d, nil
}

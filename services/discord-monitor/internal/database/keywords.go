package database

import (
	"context"
	"fmt"
	"time"
)

// Keyword represents a watchlist pattern for priority scoring.
// Keywords can be plain text (case-insensitive substring match) or regex.
// An empty GuildID means the keyword is global (applies to all guilds).
type Keyword struct {
	ID        string    `json:"id"`
	Pattern   string    `json:"pattern"`
	IsRegex   bool      `json:"is_regex"`
	GuildID   string    `json:"guild_id"` // empty = global
	Priority  int       `json:"priority"` // 0-100, higher = more important
	CreatedAt time.Time `json:"created_at"`
}

// AddKeyword inserts a new keyword pattern into the database.
// The keyword_id is auto-generated as a UUID. guildID may be empty for
// global keywords. priority should be 0-100.
func (db *DB) AddKeyword(ctx context.Context, pattern string, isRegex bool, guildID string, priority int) error {
	// Use NULL for empty guild_id to match the foreign key constraint.
	var guildPtr *string
	if guildID != "" {
		guildPtr = &guildID
	}

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO keywords (pattern, is_regex, guild_id, priority)
		VALUES ($1, $2, $3, $4)`,
		pattern, isRegex, guildPtr, priority,
	)
	if err != nil {
		return fmt.Errorf("add keyword %q: %w", pattern, err)
	}
	return nil
}

// ListKeywords returns all keywords, optionally filtered by guild.
// If guildID is empty, returns all keywords (both global and guild-specific).
// If guildID is non-empty, returns global keywords plus keywords for that guild.
func (db *DB) ListKeywords(ctx context.Context, guildID string) ([]Keyword, error) {
	var query string
	var args []any

	if guildID == "" {
		// Return all keywords.
		query = `
			SELECT keyword_id, pattern, is_regex, COALESCE(guild_id, ''),
			       priority, created_at
			FROM keywords
			ORDER BY priority DESC, created_at ASC`
	} else {
		// Return global keywords + keywords for the specified guild.
		query = `
			SELECT keyword_id, pattern, is_regex, COALESCE(guild_id, ''),
			       priority, created_at
			FROM keywords
			WHERE guild_id IS NULL OR guild_id = $1
			ORDER BY priority DESC, created_at ASC`
		args = append(args, guildID)
	}

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list keywords: %w", err)
	}
	defer rows.Close()

	var keywords []Keyword
	for rows.Next() {
		var kw Keyword
		if err := rows.Scan(
			&kw.ID, &kw.Pattern, &kw.IsRegex, &kw.GuildID,
			&kw.Priority, &kw.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan keyword row: %w", err)
		}
		keywords = append(keywords, kw)
	}
	return keywords, rows.Err()
}

// DeleteKeyword removes a keyword by its UUID.
// Returns an error if the keyword does not exist.
func (db *DB) DeleteKeyword(ctx context.Context, keywordID string) error {
	tag, err := db.Pool.Exec(ctx, `
		DELETE FROM keywords WHERE keyword_id = $1`, keywordID)
	if err != nil {
		return fmt.Errorf("delete keyword %s: %w", keywordID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("keyword %s not found", keywordID)
	}
	return nil
}

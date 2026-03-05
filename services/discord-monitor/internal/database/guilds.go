package database

import (
	"context"
	"fmt"
	"time"
)

// Guild represents a tracked Discord server.
type Guild struct {
	GuildID      string     `json:"guild_id"`
	Name         string     `json:"name"`
	IconHash     string     `json:"icon_hash"`
	Mode         string     `json:"mode"` // "bot" or "selfbot"
	OwnerID      string     `json:"owner_id"`
	MemberCount  int        `json:"member_count"`
	IsActive     bool       `json:"is_active"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// UpsertGuild inserts a guild or updates it if the guild_id already exists.
// This is the primary sync operation — called after fetching guild data from
// Discord. Fields like name, icon, owner, and member count are refreshed,
// but is_active is preserved (controlled separately via SetGuildActive).
func (db *DB) UpsertGuild(ctx context.Context, g Guild) (*Guild, error) {
	var result Guild
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO guilds (guild_id, name, icon_hash, mode, owner_id, member_count, last_synced_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now(), now())
		ON CONFLICT (guild_id) DO UPDATE SET
			name           = EXCLUDED.name,
			icon_hash      = EXCLUDED.icon_hash,
			mode           = EXCLUDED.mode,
			owner_id       = EXCLUDED.owner_id,
			member_count   = EXCLUDED.member_count,
			last_synced_at = now(),
			updated_at     = now()
		RETURNING guild_id, name, icon_hash, mode, owner_id, member_count,
		          is_active, last_synced_at, created_at, updated_at`,
		g.GuildID, g.Name, g.IconHash, g.Mode, g.OwnerID, g.MemberCount,
	).Scan(
		&result.GuildID, &result.Name, &result.IconHash, &result.Mode,
		&result.OwnerID, &result.MemberCount, &result.IsActive,
		&result.LastSyncedAt, &result.CreatedAt, &result.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert guild %s: %w", g.GuildID, err)
	}
	return &result, nil
}

// GetGuild retrieves a single guild by its Discord ID.
func (db *DB) GetGuild(ctx context.Context, guildID string) (*Guild, error) {
	var g Guild
	err := db.Pool.QueryRow(ctx, `
		SELECT guild_id, name, icon_hash, mode, owner_id, member_count,
		       is_active, last_synced_at, created_at, updated_at
		FROM guilds WHERE guild_id = $1`, guildID,
	).Scan(
		&g.GuildID, &g.Name, &g.IconHash, &g.Mode,
		&g.OwnerID, &g.MemberCount, &g.IsActive,
		&g.LastSyncedAt, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get guild %s: %w", guildID, err)
	}
	return &g, nil
}

// ListGuilds returns all tracked guilds, ordered by name.
// If activeOnly is true, only guilds with is_active=true are returned.
func (db *DB) ListGuilds(ctx context.Context, activeOnly bool) ([]Guild, error) {
	query := `
		SELECT guild_id, name, icon_hash, mode, owner_id, member_count,
		       is_active, last_synced_at, created_at, updated_at
		FROM guilds`
	if activeOnly {
		query += ` WHERE is_active = TRUE`
	}
	query += ` ORDER BY name ASC`

	rows, err := db.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list guilds: %w", err)
	}
	defer rows.Close()

	var guilds []Guild
	for rows.Next() {
		var g Guild
		if err := rows.Scan(
			&g.GuildID, &g.Name, &g.IconHash, &g.Mode,
			&g.OwnerID, &g.MemberCount, &g.IsActive,
			&g.LastSyncedAt, &g.CreatedAt, &g.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan guild row: %w", err)
		}
		guilds = append(guilds, g)
	}
	return guilds, rows.Err()
}

// SetGuildActive enables or disables monitoring for a guild.
// Deactivated guilds are skipped during polling but their data is preserved.
func (db *DB) SetGuildActive(ctx context.Context, guildID string, active bool) error {
	tag, err := db.Pool.Exec(ctx, `
		UPDATE guilds SET is_active = $2, updated_at = now()
		WHERE guild_id = $1`, guildID, active)
	if err != nil {
		return fmt.Errorf("set guild %s active=%v: %w", guildID, active, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("guild %s not found", guildID)
	}
	return nil
}

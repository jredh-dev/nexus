package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/jredh-dev/nexus/services/discord-monitor/internal/database"
)

// Digest summarizes activity in a guild over a time period.
// Used for periodic summaries ("what happened in this server today?")
// and on-demand reports.
type Digest struct {
	GuildID       string          `json:"guild_id"`
	GuildName     string          `json:"guild_name"`
	PeriodStart   time.Time       `json:"period_start"`
	PeriodEnd     time.Time       `json:"period_end"`
	TotalMessages int             `json:"total_messages"`
	Channels      []ChannelDigest `json:"channels"`
}

// ChannelDigest summarizes activity in a single channel within a digest period.
type ChannelDigest struct {
	ChannelID     string `json:"channel_id"`
	ChannelName   string `json:"channel_name"`
	MessageCount  int    `json:"message_count"`
	UniqueAuthors int    `json:"unique_authors"`
	HasMentions   bool   `json:"has_mentions"`
	HasKeywords   bool   `json:"has_keywords"`
}

// GenerateDigest creates a digest for a guild covering messages since the
// given time. It queries all monitored channels in the guild, counts messages,
// identifies unique authors, and checks for mentions and keyword matches.
//
// The returned digest is a snapshot — it does not modify any database state.
// Call db.StoreDigest() separately to persist it.
func GenerateDigest(ctx context.Context, db *database.DB, guildID string, since time.Time) (*Digest, error) {
	now := time.Now()

	// Fetch the guild metadata for the digest header.
	guild, err := db.GetGuild(ctx, guildID)
	if err != nil {
		return nil, fmt.Errorf("get guild %s: %w", guildID, err)
	}

	// Fetch all keywords for keyword-match detection.
	keywords, err := db.ListKeywords(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list keywords: %w", err)
	}

	// Filter to keywords that apply to this guild (global + guild-specific).
	guildKeywords := filterKeywords(keywords, guildID)

	// Get all channels in the guild (not just monitored — we want a complete
	// digest even for channels that were recently toggled off).
	channels, err := db.ListChannels(ctx, guildID, false)
	if err != nil {
		return nil, fmt.Errorf("list channels for guild %s: %w", guildID, err)
	}

	digest := &Digest{
		GuildID:     guildID,
		GuildName:   guild.Name,
		PeriodStart: since,
		PeriodEnd:   now,
	}

	for _, ch := range channels {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Only include text channels (type 0) in the digest.
		if ch.Type != 0 {
			continue
		}

		// Count messages in this channel during the digest period.
		msgCount, err := db.GetMessageCount(ctx, ch.ChannelID, since)
		if err != nil {
			return nil, fmt.Errorf("count messages for channel %s: %w", ch.ChannelID, err)
		}

		// Skip channels with no activity during the period.
		if msgCount == 0 {
			continue
		}

		// Fetch the actual messages for deeper analysis (unique authors,
		// mentions, keywords). We use "0" as afterID to get all messages
		// since our time window — GetUnreadMessages filters by message_id
		// ordering, so we rely on the stored_at/created_at for time filtering.
		msgs, err := db.GetUnreadMessages(ctx, ch.ChannelID, "0", 0)
		if err != nil {
			return nil, fmt.Errorf("get messages for channel %s: %w", ch.ChannelID, err)
		}

		// Filter messages to the digest period.
		var periodMsgs []database.Message
		for _, m := range msgs {
			if m.CreatedAt.After(since) || m.CreatedAt.Equal(since) {
				periodMsgs = append(periodMsgs, m)
			}
		}

		// Count unique authors.
		authorSet := make(map[string]bool)
		hasMentions := false
		for _, m := range periodMsgs {
			authorSet[m.AuthorID] = true
			if m.MentionsMe {
				hasMentions = true
			}
		}

		// Check for keyword matches.
		hasKeywords := len(matchKeywords(periodMsgs, guildKeywords)) > 0

		digest.TotalMessages += len(periodMsgs)
		digest.Channels = append(digest.Channels, ChannelDigest{
			ChannelID:     ch.ChannelID,
			ChannelName:   ch.Name,
			MessageCount:  len(periodMsgs),
			UniqueAuthors: len(authorSet),
			HasMentions:   hasMentions,
			HasKeywords:   hasKeywords,
		})
	}

	return digest, nil
}

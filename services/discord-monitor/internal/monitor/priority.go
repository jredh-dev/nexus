// Package monitor provides priority scoring, digest generation, and
// activity analysis for the discord-monitor service.
//
// The priority scoring engine computes a 0-100 score for each channel
// based on unread message characteristics (mentions, keywords, volume,
// recency). Scores are used to rank channels in the API and determine
// notification urgency.
package monitor

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jredh-dev/nexus/services/discord-monitor/internal/database"
)

// Scoring weight constants. These control how much each signal contributes
// to the final priority score. The sum of all weights exceeds 100, but the
// final score is clamped to [0, 100].
const (
	// weightDirectMention: the user was @mentioned by name. This is the
	// strongest signal — someone is directly addressing the user.
	weightDirectMention = 40

	// weightRoleMention: a role the user holds was @mentioned. Important
	// but less urgent than a direct mention.
	weightRoleMention = 30

	// weightKeywordMatch: a watched keyword appeared in the messages.
	// Normalized — multiple keyword hits don't stack beyond this cap.
	weightKeywordMatch = 20

	// weightHighVolume: the channel has a high volume of unread messages
	// (>50). Scales linearly from 0-10 as volume goes from 50-200.
	weightHighVolume = 10

	// weightRecentActivity: messages were posted in the last hour.
	// A recency bonus indicating the conversation is happening right now.
	weightRecentActivity = 5

	// highVolumeThreshold: minimum unread messages to start earning
	// the high-volume score component.
	highVolumeThreshold = 50

	// highVolumeCap: the message count at which high-volume score is maxed.
	highVolumeCap = 200
)

// ChannelPriority represents the computed priority for a single channel.
// Used to rank channels in the unread API response.
type ChannelPriority struct {
	ChannelID   string   `json:"channel_id"`
	GuildID     string   `json:"guild_id"`
	GuildName   string   `json:"guild_name"`
	ChannelName string   `json:"channel_name"`
	UnreadCount int      `json:"unread_count"`
	Score       int      `json:"score"`
	Reasons     []string `json:"reasons"` // e.g. ["mention", "keyword: deploy", "high volume"]
}

// ScoreChannel computes the priority score for a single channel's unread
// messages. Returns the score (0-100) and a list of reasons explaining
// why the score was assigned.
//
// Scoring weights:
//
//	+40  direct @mention of the monitored user
//	+30  @role mention matching user's roles
//	+20  keyword match (any configured keyword pattern)
//	+10  high volume (>50 messages, scales linearly to cap at 200)
//	+5   recent activity (any message in the last hour)
func ScoreChannel(msgs []database.Message, keywords []database.Keyword, userID string) (int, []string) {
	if len(msgs) == 0 {
		return 0, nil
	}

	score := 0
	var reasons []string

	// Check for direct @mentions of the monitored user.
	hasMention := false
	for _, m := range msgs {
		if m.MentionsMe {
			hasMention = true
			break
		}
	}
	if hasMention {
		score += weightDirectMention
		reasons = append(reasons, "mention")
	}

	// Check for @role mentions. We look for any non-empty role mention
	// across all messages. In a full implementation, we'd check if the
	// user actually holds the mentioned role — for now, any role mention
	// in a monitored channel is considered relevant.
	hasRoleMention := false
	for _, m := range msgs {
		if len(m.MentionsRoles) > 0 {
			hasRoleMention = true
			break
		}
	}
	if hasRoleMention {
		score += weightRoleMention
		reasons = append(reasons, "role mention")
	}

	// Check for keyword matches across all messages.
	matchedKeywords := matchKeywords(msgs, keywords)
	if len(matchedKeywords) > 0 {
		score += weightKeywordMatch
		for _, kw := range matchedKeywords {
			reasons = append(reasons, "keyword: "+kw)
		}
	}

	// High volume bonus: scales linearly from 0 to weightHighVolume
	// as message count goes from highVolumeThreshold to highVolumeCap.
	count := len(msgs)
	if count > highVolumeThreshold {
		// Calculate linear scaling factor.
		excess := count - highVolumeThreshold
		range_ := highVolumeCap - highVolumeThreshold
		volumeScore := (excess * weightHighVolume) / range_
		if volumeScore > weightHighVolume {
			volumeScore = weightHighVolume
		}
		score += volumeScore
		reasons = append(reasons, fmt.Sprintf("high volume (%d msgs)", count))
	}

	// Recent activity bonus: any message in the last hour.
	oneHourAgo := time.Now().Add(-1 * time.Hour)
	for _, m := range msgs {
		if m.CreatedAt.After(oneHourAgo) {
			score += weightRecentActivity
			reasons = append(reasons, "recent activity")
			break
		}
	}

	// Clamp to [0, 100].
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	return score, reasons
}

// matchKeywords checks all messages against all keyword patterns.
// Returns a deduplicated list of matched keyword patterns.
func matchKeywords(msgs []database.Message, keywords []database.Keyword) []string {
	if len(keywords) == 0 {
		return nil
	}

	// Build a set of matched keywords to deduplicate.
	matched := make(map[string]bool)

	for _, kw := range keywords {
		for _, m := range msgs {
			if matchKeyword(m.Content, kw) {
				matched[kw.Pattern] = true
				break // One match per keyword is enough.
			}
		}
	}

	// Convert map to slice.
	result := make([]string, 0, len(matched))
	for pattern := range matched {
		result = append(result, pattern)
	}
	return result
}

// matchKeyword checks if a single message matches a keyword pattern.
// Supports both plain text (case-insensitive substring) and regex patterns.
func matchKeyword(content string, kw database.Keyword) bool {
	if kw.IsRegex {
		re, err := regexp.Compile(kw.Pattern)
		if err != nil {
			// Invalid regex — skip silently. The pattern was validated on
			// insertion, but we handle this defensively.
			return false
		}
		return re.MatchString(content)
	}
	// Plain text: case-insensitive substring match.
	return strings.Contains(strings.ToLower(content), strings.ToLower(kw.Pattern))
}

// ScoreAll computes priorities for all channels with unread messages.
// Returns a sorted slice (highest priority first) of channels that have
// at least one unread message.
func ScoreAll(ctx context.Context, db *database.DB, userID string) ([]ChannelPriority, error) {
	// Fetch all keywords for scoring — both global and guild-specific.
	allKeywords, err := db.ListKeywords(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list keywords: %w", err)
	}

	// Fetch all active guilds.
	guilds, err := db.ListGuilds(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("list guilds: %w", err)
	}

	var priorities []ChannelPriority

	for _, g := range guilds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Get monitored channels for this guild.
		channels, err := db.ListChannels(ctx, g.GuildID, true)
		if err != nil {
			return nil, fmt.Errorf("list channels for guild %s: %w", g.GuildID, err)
		}

		// Filter keywords relevant to this guild (global + guild-specific).
		guildKeywords := filterKeywords(allKeywords, g.GuildID)

		for _, ch := range channels {
			// Get unread messages: everything after the read cursor.
			cursor, err := db.GetCursor(ctx, ch.ChannelID)
			if err != nil {
				return nil, fmt.Errorf("get cursor for channel %s: %w", ch.ChannelID, err)
			}

			afterID := "0"
			if cursor != nil {
				afterID = cursor.LastReadMsgID
			}

			msgs, err := db.GetUnreadMessages(ctx, ch.ChannelID, afterID, 0)
			if err != nil {
				return nil, fmt.Errorf("get unread messages for channel %s: %w", ch.ChannelID, err)
			}

			if len(msgs) == 0 {
				continue
			}

			score, reasons := ScoreChannel(msgs, guildKeywords, userID)

			priorities = append(priorities, ChannelPriority{
				ChannelID:   ch.ChannelID,
				GuildID:     g.GuildID,
				GuildName:   g.Name,
				ChannelName: ch.Name,
				UnreadCount: len(msgs),
				Score:       score,
				Reasons:     reasons,
			})
		}
	}

	// Sort by score descending. Using insertion sort since the list is
	// typically small (tens of channels at most).
	sortByScore(priorities)

	return priorities, nil
}

// filterKeywords returns keywords that apply to the given guild.
// A keyword applies if it's global (empty GuildID) or matches the guild.
func filterKeywords(keywords []database.Keyword, guildID string) []database.Keyword {
	var filtered []database.Keyword
	for _, kw := range keywords {
		if kw.GuildID == "" || kw.GuildID == guildID {
			filtered = append(filtered, kw)
		}
	}
	return filtered
}

// sortByScore sorts channel priorities by score descending (highest first).
// Stable sort preserves original order for equal scores.
func sortByScore(priorities []ChannelPriority) {
	// Simple insertion sort — channel count is always small.
	for i := 1; i < len(priorities); i++ {
		key := priorities[i]
		j := i - 1
		for j >= 0 && priorities[j].Score < key.Score {
			priorities[j+1] = priorities[j]
			j--
		}
		priorities[j+1] = key
	}
}

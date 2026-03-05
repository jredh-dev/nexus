package selfbot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Discord API response types. These structs contain only the fields we
// actually need for monitoring — Discord's API returns much more, but
// json.Decoder silently ignores unknown fields.

// User represents the authenticated Discord user (from GET /users/@me).
type User struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	Avatar        string `json:"avatar"`
	Email         string `json:"email"`
}

// Guild represents a Discord server the user is a member of.
type Guild struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Icon                   string `json:"icon"`
	Owner                  bool   `json:"owner"`
	OwnerID                string `json:"owner_id"`
	ApproximateMemberCount int    `json:"approximate_member_count"`
}

// Channel represents a text or voice channel within a guild.
type Channel struct {
	ID       string  `json:"id"`
	GuildID  string  `json:"guild_id"`
	Name     string  `json:"name"`
	Type     int     `json:"type"`      // 0=text, 2=voice, 4=category, etc.
	ParentID *string `json:"parent_id"` // category ID, null for top-level
	Position int     `json:"position"`
}

// Message represents a Discord message.
type Message struct {
	ID           string            `json:"id"`
	ChannelID    string            `json:"channel_id"`
	Author       Author            `json:"author"`
	Content      string            `json:"content"`
	Timestamp    time.Time         `json:"timestamp"`
	Embeds       []json.RawMessage `json:"embeds"`        // we only check length
	Attachments  []json.RawMessage `json:"attachments"`   // we only check length
	Mentions     []Author          `json:"mentions"`      // users mentioned
	MentionRoles []string          `json:"mention_roles"` // role IDs mentioned
}

// Author is the user who sent a message.
type Author struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// GetMe returns the authenticated user's profile.
// This is the first call to make after constructing the client — it
// verifies the token is valid.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	resp, err := c.do(ctx, http.MethodGet, "/users/@me", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkStatus(resp); err != nil {
		return nil, fmt.Errorf("get me: %w", err)
	}

	var u User
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("decode user: %w", err)
	}
	return &u, nil
}

// GetGuilds returns all guilds the user is a member of.
// Discord limits this to 200 guilds per page; for monitoring purposes
// this is more than enough. Add pagination if needed in the future.
func (c *Client) GetGuilds(ctx context.Context) ([]Guild, error) {
	// with_counts=true includes approximate_member_count in the response.
	resp, err := c.do(ctx, http.MethodGet, "/users/@me/guilds?with_counts=true", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkStatus(resp); err != nil {
		return nil, fmt.Errorf("get guilds: %w", err)
	}

	var guilds []Guild
	if err := json.NewDecoder(resp.Body).Decode(&guilds); err != nil {
		return nil, fmt.Errorf("decode guilds: %w", err)
	}
	return guilds, nil
}

// GetChannels returns all channels in a guild.
// Includes text, voice, category, and other channel types. The caller
// should filter by type if only text channels are needed (type=0).
func (c *Client) GetChannels(ctx context.Context, guildID string) ([]Channel, error) {
	path := "/guilds/" + guildID + "/channels"
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkStatus(resp); err != nil {
		return nil, fmt.Errorf("get channels for guild %s: %w", guildID, err)
	}

	var channels []Channel
	if err := json.NewDecoder(resp.Body).Decode(&channels); err != nil {
		return nil, fmt.Errorf("decode channels: %w", err)
	}
	return channels, nil
}

// GetMessages returns messages from a channel, optionally after a given
// message ID (for pagination / catch-up polling).
//
// afterID: if non-empty, only messages after this ID are returned (exclusive).
// limit: max messages to return (1-100, Discord enforces this).
//
// Messages are returned newest-first (Discord's default). The caller
// should reverse the slice if chronological order is needed.
func (c *Client) GetMessages(ctx context.Context, channelID, afterID string, limit int) ([]Message, error) {
	// Clamp limit to Discord's allowed range.
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	path := "/channels/" + channelID + "/messages?limit=" + strconv.Itoa(limit)
	if afterID != "" {
		path += "&after=" + afterID
	}

	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkStatus(resp); err != nil {
		return nil, fmt.Errorf("get messages for channel %s: %w", channelID, err)
	}

	var msgs []Message
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		return nil, fmt.Errorf("decode messages: %w", err)
	}
	return msgs, nil
}

// checkStatus returns an error if the HTTP response indicates failure.
// Includes the response body in the error message for debugging.
func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
}

package selfbot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jredh-dev/nexus/services/discord-monitor/internal/database"
)

// Scanner periodically scans all guilds and channels via the selfbot client
// and stores results in the database. It runs as a long-lived goroutine,
// performing a full scan cycle on each tick of the configured interval.
//
// Scan cycle:
//  1. GetMe() to resolve userID (first run only, cached after)
//  2. GetGuilds() → upsert each to DB
//  3. For each guild: GetChannels() → upsert text channels to DB
//  4. For each monitored channel: GetMessages(afterID=cursor, limit=100)
//     → StoreMessages, flag mentions_me if author mentions userID
//  5. Advance read cursor to latest stored message per channel
//  6. Log scan results (guilds synced, messages found, errors)
type Scanner struct {
	client   *Client
	db       *database.DB
	interval time.Duration
	userID   string // resolved from GetMe() on first scan
}

// NewScanner creates a Scanner that polls Discord at the given interval.
// The client should already be authenticated (token set). The userID is
// resolved lazily on the first scan via GetMe().
func NewScanner(client *Client, db *database.DB, interval time.Duration) *Scanner {
	return &Scanner{
		client:   client,
		db:       db,
		interval: interval,
	}
}

// Start begins the scan loop. Blocks until ctx is cancelled.
// Runs an immediate scan on start, then ticks at the configured interval.
// Errors from individual scan cycles are logged but do not stop the loop.
func (s *Scanner) Start(ctx context.Context) {
	log.Printf("[scanner] starting scan loop, interval=%s", s.interval)

	// Run an immediate scan before entering the ticker loop.
	if err := s.ScanOnce(ctx); err != nil {
		log.Printf("[scanner] initial scan error: %v", err)
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[scanner] stopping: context cancelled")
			return
		case <-ticker.C:
			if err := s.ScanOnce(ctx); err != nil {
				log.Printf("[scanner] scan cycle error: %v", err)
			}
		}
	}
}

// ScanOnce performs a single scan cycle. Exported for testing.
// Steps: resolve user → sync guilds → sync channels → fetch messages.
func (s *Scanner) ScanOnce(ctx context.Context) error {
	start := time.Now()

	// Resolve our user ID on the first scan. We need this to detect
	// @mentions directed at us in message content.
	if s.userID == "" {
		user, err := s.client.GetMe(ctx)
		if err != nil {
			return fmt.Errorf("resolve user identity: %w", err)
		}
		s.userID = user.ID
		log.Printf("[scanner] resolved user: %s#%s (%s)",
			user.Username, user.Discriminator, user.ID)
	}

	// Step 1: Fetch and upsert all guilds the user belongs to.
	guilds, err := s.client.GetGuilds(ctx)
	if err != nil {
		return fmt.Errorf("fetch guilds: %w", err)
	}

	var (
		guildsSynced   int
		channelsSynced int
		messagesStored int64
		scanErrors     int
	)

	for _, g := range guilds {
		// Check for context cancellation between guilds to allow
		// graceful shutdown mid-scan.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Upsert the guild into our database.
		_, err := s.db.UpsertGuild(ctx, database.Guild{
			GuildID:     g.ID,
			Name:        g.Name,
			IconHash:    g.Icon,
			Mode:        "selfbot",
			OwnerID:     g.OwnerID,
			MemberCount: g.ApproximateMemberCount,
		})
		if err != nil {
			log.Printf("[scanner] upsert guild %s (%s): %v", g.ID, g.Name, err)
			scanErrors++
			continue
		}
		guildsSynced++

		// Step 2: Fetch and upsert channels for this guild.
		channels, err := s.client.GetChannels(ctx, g.ID)
		if err != nil {
			log.Printf("[scanner] fetch channels for guild %s: %v", g.ID, err)
			scanErrors++
			continue
		}

		for _, ch := range channels {
			// Only sync text channels (type 0). Voice, category, etc.
			// are not relevant for message monitoring.
			if ch.Type != 0 {
				continue
			}

			_, err := s.db.UpsertChannel(ctx, database.Channel{
				ChannelID: ch.ID,
				GuildID:   g.ID,
				Name:      ch.Name,
				Type:      ch.Type,
				ParentID:  ch.ParentID,
				Position:  ch.Position,
			})
			if err != nil {
				log.Printf("[scanner] upsert channel %s (%s): %v", ch.ID, ch.Name, err)
				scanErrors++
				continue
			}
			channelsSynced++
		}

		// Step 3: Fetch new messages for monitored channels in this guild.
		stored, errs := s.scanGuildMessages(ctx, g.ID)
		messagesStored += stored
		scanErrors += errs
	}

	elapsed := time.Since(start)
	log.Printf("[scanner] scan complete: guilds=%d channels=%d messages=%d errors=%d elapsed=%s",
		guildsSynced, channelsSynced, messagesStored, scanErrors, elapsed.Round(time.Millisecond))

	return nil
}

// scanGuildMessages fetches new messages for all monitored channels in a guild.
// Returns the total number of messages stored and the count of errors.
func (s *Scanner) scanGuildMessages(ctx context.Context, guildID string) (int64, int) {
	// Only scan channels that are marked as monitored.
	channels, err := s.db.ListChannels(ctx, guildID, true)
	if err != nil {
		log.Printf("[scanner] list monitored channels for guild %s: %v", guildID, err)
		return 0, 1
	}

	var (
		totalStored int64
		totalErrors int
	)

	for _, ch := range channels {
		if ctx.Err() != nil {
			return totalStored, totalErrors
		}

		stored, err := s.scanChannel(ctx, ch)
		if err != nil {
			log.Printf("[scanner] scan channel %s (%s): %v", ch.ChannelID, ch.Name, err)
			totalErrors++
			continue
		}
		totalStored += stored
	}

	return totalStored, totalErrors
}

// scanChannel fetches messages from a single channel starting after the
// read cursor, converts them to database messages, and stores them.
func (s *Scanner) scanChannel(ctx context.Context, ch database.Channel) (int64, error) {
	// Get the read cursor — this tells us where we left off last time.
	cursor, err := s.db.GetCursor(ctx, ch.ChannelID)
	if err != nil {
		return 0, fmt.Errorf("get cursor: %w", err)
	}

	// If no cursor exists, we start from "0" which means "get latest messages".
	// We don't try to backfill the entire channel history — that would be
	// extremely expensive and hit rate limits.
	afterID := ""
	if cursor != nil {
		afterID = cursor.LastReadMsgID
	}

	// Fetch up to 100 messages from Discord.
	apiMsgs, err := s.client.GetMessages(ctx, ch.ChannelID, afterID, 100)
	if err != nil {
		return 0, fmt.Errorf("fetch messages: %w", err)
	}

	if len(apiMsgs) == 0 {
		return 0, nil
	}

	// Convert Discord API messages to our database format.
	dbMsgs := make([]database.Message, 0, len(apiMsgs))
	for _, m := range apiMsgs {
		dbMsgs = append(dbMsgs, s.convertMessage(m))
	}

	// Store all messages in a single transaction.
	stored, err := s.db.StoreMessages(ctx, dbMsgs)
	if err != nil {
		return 0, fmt.Errorf("store messages: %w", err)
	}

	// Advance the cursor to the latest stored message so the next scan
	// picks up from where we left off.
	if err := s.db.AdvanceCursor(ctx, ch.ChannelID); err != nil {
		return stored, fmt.Errorf("advance cursor: %w", err)
	}

	return stored, nil
}

// convertMessage transforms a Discord API message into a database message.
// It checks the mentions list to see if the selfbot user was @mentioned.
func (s *Scanner) convertMessage(m Message) database.Message {
	// Check if the selfbot user was directly @mentioned.
	mentionsMe := false
	for _, mention := range m.Mentions {
		if mention.ID == s.userID {
			mentionsMe = true
			break
		}
	}

	// Also check if the user ID appears in the raw content as <@userID>
	// which catches inline mentions that might not be in the mentions array.
	if !mentionsMe && strings.Contains(m.Content, "<@"+s.userID+">") {
		mentionsMe = true
	}

	return database.Message{
		MessageID:      m.ID,
		ChannelID:      m.ChannelID,
		AuthorID:       m.Author.ID,
		AuthorName:     m.Author.Username,
		Content:        m.Content,
		HasEmbeds:      len(m.Embeds) > 0,
		HasAttachments: len(m.Attachments) > 0,
		MentionsMe:     mentionsMe,
		MentionsRoles:  m.MentionRoles,
		CreatedAt:      m.Timestamp,
	}
}

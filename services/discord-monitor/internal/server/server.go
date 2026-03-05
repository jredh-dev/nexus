// Package server provides the HTTP API for the discord-monitor service.
//
// Routes:
//
//	GET /health       — health check (provided by go-http scaffold)
//	GET /api/guilds   — list tracked guilds
//	GET /api/unread   — unread messages across all monitored channels
//	GET /api/status   — service status (uptime, selfbot connected, etc.)
package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	gohttp "github.com/jredh-dev/nexus/services/go-http"

	"github.com/jredh-dev/nexus/services/discord-monitor/internal/database"
)

// startTime is set at server creation for uptime calculation.
var startTime time.Time

// Server is the discord-monitor HTTP API server.
type Server struct {
	db     *database.DB
	router chi.Router
	gohttp *gohttp.Server
}

// Config holds the dependencies for creating a Server.
type Config struct {
	DB *database.DB

	// SelfbotConnected indicates whether the selfbot client was
	// successfully initialized. Used for the /api/status response.
	SelfbotConnected bool
}

// New creates a discord-monitor HTTP server with all routes registered.
// Uses the go-http scaffold for standard middleware (logging, recovery,
// CORS, graceful shutdown) and the /health endpoint.
func New(cfg Config) *gohttp.Server {
	startTime = time.Now()

	srv := gohttp.New()

	h := &handlers{
		db:               cfg.DB,
		selfbotConnected: cfg.SelfbotConnected,
	}

	srv.Router.Route("/api", func(r chi.Router) {
		r.Get("/guilds", h.listGuilds)
		r.Get("/unread", h.getUnread)
		r.Get("/status", h.getStatus)
	})

	return srv
}

// handlers holds request handler dependencies.
type handlers struct {
	db               *database.DB
	selfbotConnected bool
}

// listGuilds returns all tracked guilds as JSON.
// Query param: ?active=true to filter to active guilds only.
func (h *handlers) listGuilds(w http.ResponseWriter, r *http.Request) {
	activeOnly := r.URL.Query().Get("active") == "true"

	guilds, err := h.db.ListGuilds(r.Context(), activeOnly)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Return empty array instead of null when there are no guilds.
	if guilds == nil {
		guilds = []database.Guild{}
	}

	gohttp.WriteJSON(w, http.StatusOK, guilds)
}

// getUnread returns unread messages across all monitored channels.
// This aggregates messages newer than each channel's read cursor.
// Query param: ?guild_id=X to filter to a specific guild.
func (h *handlers) getUnread(w http.ResponseWriter, r *http.Request) {
	guildFilter := r.URL.Query().Get("guild_id")

	// Determine which guilds to scan.
	guilds, err := h.db.ListGuilds(r.Context(), true)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type channelUnread struct {
		ChannelID   string             `json:"channel_id"`
		ChannelName string             `json:"channel_name"`
		GuildID     string             `json:"guild_id"`
		GuildName   string             `json:"guild_name"`
		Messages    []database.Message `json:"messages"`
	}

	var results []channelUnread

	for _, g := range guilds {
		// Skip guilds that don't match the filter (if set).
		if guildFilter != "" && g.GuildID != guildFilter {
			continue
		}

		channels, err := h.db.ListChannels(r.Context(), g.GuildID, true)
		if err != nil {
			gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}

		for _, ch := range channels {
			cursor, err := h.db.GetCursor(r.Context(), ch.ChannelID)
			if err != nil {
				gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}

			// No cursor means we haven't read anything yet — use "0"
			// to get all stored messages.
			afterID := "0"
			if cursor != nil {
				afterID = cursor.LastReadMsgID
			}

			msgs, err := h.db.GetUnreadMessages(r.Context(), ch.ChannelID, afterID, 50)
			if err != nil {
				gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}

			if len(msgs) > 0 {
				results = append(results, channelUnread{
					ChannelID:   ch.ChannelID,
					ChannelName: ch.Name,
					GuildID:     g.GuildID,
					GuildName:   g.Name,
					Messages:    msgs,
				})
			}
		}
	}

	if results == nil {
		results = []channelUnread{}
	}

	gohttp.WriteJSON(w, http.StatusOK, results)
}

// getStatus returns service health and operational info.
func (h *handlers) getStatus(w http.ResponseWriter, r *http.Request) {
	guilds, err := h.db.ListGuilds(r.Context(), false)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	activeCount := 0
	for _, g := range guilds {
		if g.IsActive {
			activeCount++
		}
	}

	gohttp.WriteJSON(w, http.StatusOK, map[string]any{
		"status":             "ok",
		"uptime_seconds":     int(time.Since(startTime).Seconds()),
		"selfbot_connected":  h.selfbotConnected,
		"total_guilds":       len(guilds),
		"active_guilds":      activeCount,
	})
}

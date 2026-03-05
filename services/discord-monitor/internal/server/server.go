// Package server provides the HTTP API for the discord-monitor service.
//
// Routes:
//
//	GET    /health              — health check (provided by go-http scaffold)
//	GET    /api/guilds          — list tracked guilds
//	GET    /api/unread          — unread messages with priority scoring
//	GET    /api/status          — service status (uptime, selfbot connected, etc.)
//	GET    /api/keywords        — list keyword watchlist
//	POST   /api/keywords        — add a keyword pattern
//	DELETE /api/keywords/{id}   — delete a keyword by ID
//	GET    /api/digests         — list digests for a guild
//	POST   /api/digests/generate — trigger digest generation
//	GET    /api/heatmap         — activity heatmap for a channel or guild
package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	gohttp "github.com/jredh-dev/nexus/services/go-http"

	"github.com/jredh-dev/nexus/services/discord-monitor/internal/database"
	"github.com/jredh-dev/nexus/services/discord-monitor/internal/monitor"
)

// startTime is set at server creation for uptime calculation.
var startTime time.Time

// Config holds the dependencies for creating a Server.
type Config struct {
	DB *database.DB

	// SelfbotConnected indicates whether the selfbot client was
	// successfully initialized. Used for the /api/status response.
	SelfbotConnected bool

	// UserID is the authenticated Discord user's ID. Used for priority
	// scoring (detecting @mentions directed at us).
	UserID string
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
		userID:           cfg.UserID,
	}

	srv.Router.Route("/api", func(r chi.Router) {
		r.Get("/guilds", h.listGuilds)
		r.Get("/unread", h.getUnread)
		r.Get("/status", h.getStatus)

		// Keyword watchlist endpoints.
		r.Get("/keywords", h.listKeywords)
		r.Post("/keywords", h.addKeyword)
		r.Delete("/keywords/{id}", h.deleteKeyword)

		// Digest endpoints.
		r.Get("/digests", h.listDigests)
		r.Post("/digests/generate", h.generateDigest)

		// Activity heatmap endpoint.
		r.Get("/heatmap", h.getHeatmap)
	})

	return srv
}

// handlers holds request handler dependencies.
type handlers struct {
	db               *database.DB
	selfbotConnected bool
	userID           string
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

// getUnread returns unread messages across all monitored channels,
// scored and sorted by priority. Uses the monitor.ScoreAll engine
// to compute priority scores based on mentions, keywords, volume, etc.
// Query param: ?guild_id=X to filter to a specific guild.
func (h *handlers) getUnread(w http.ResponseWriter, r *http.Request) {
	priorities, err := monitor.ScoreAll(r.Context(), h.db, h.userID)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Filter by guild if requested.
	guildFilter := r.URL.Query().Get("guild_id")
	if guildFilter != "" {
		var filtered []monitor.ChannelPriority
		for _, p := range priorities {
			if p.GuildID == guildFilter {
				filtered = append(filtered, p)
			}
		}
		priorities = filtered
	}

	if priorities == nil {
		priorities = []monitor.ChannelPriority{}
	}

	gohttp.WriteJSON(w, http.StatusOK, priorities)
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
		"status":            "ok",
		"uptime_seconds":    int(time.Since(startTime).Seconds()),
		"selfbot_connected": h.selfbotConnected,
		"total_guilds":      len(guilds),
		"active_guilds":     activeCount,
	})
}

// listKeywords returns all keyword patterns.
// Query param: ?guild_id=X to filter to keywords for a specific guild
// (includes global keywords).
func (h *handlers) listKeywords(w http.ResponseWriter, r *http.Request) {
	guildID := r.URL.Query().Get("guild_id")

	keywords, err := h.db.ListKeywords(r.Context(), guildID)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if keywords == nil {
		keywords = []database.Keyword{}
	}

	gohttp.WriteJSON(w, http.StatusOK, keywords)
}

// addKeywordRequest is the JSON body for POST /api/keywords.
type addKeywordRequest struct {
	Pattern  string `json:"pattern"`
	IsRegex  bool   `json:"is_regex"`
	GuildID  string `json:"guild_id"`
	Priority int    `json:"priority"`
}

// addKeyword creates a new keyword pattern.
// Request body (JSON): {"pattern": "deploy", "is_regex": false, "guild_id": "", "priority": 50}
func (h *handlers) addKeyword(w http.ResponseWriter, r *http.Request) {
	var req addKeywordRequest
	if err := gohttp.DecodeJSON(r, &req); err != nil {
		gohttp.WriteError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Validate required fields.
	if req.Pattern == "" {
		gohttp.WriteError(w, http.StatusBadRequest, "pattern is required")
		return
	}

	// Clamp priority to valid range.
	if req.Priority < 0 {
		req.Priority = 0
	}
	if req.Priority > 100 {
		req.Priority = 100
	}

	if err := h.db.AddKeyword(r.Context(), req.Pattern, req.IsRegex, req.GuildID, req.Priority); err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	gohttp.WriteJSON(w, http.StatusCreated, map[string]string{
		"status": "created",
	})
}

// deleteKeyword removes a keyword by its UUID.
func (h *handlers) deleteKeyword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		gohttp.WriteError(w, http.StatusBadRequest, "keyword id is required")
		return
	}

	if err := h.db.DeleteKeyword(r.Context(), id); err != nil {
		gohttp.WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	gohttp.WriteJSON(w, http.StatusOK, map[string]string{
		"status": "deleted",
	})
}

// listDigests returns recent digests for a guild.
// Query params: ?guild_id=X (required), ?limit=N (default 10).
func (h *handlers) listDigests(w http.ResponseWriter, r *http.Request) {
	guildID := r.URL.Query().Get("guild_id")
	if guildID == "" {
		gohttp.WriteError(w, http.StatusBadRequest, "guild_id query parameter is required")
		return
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	digests, err := h.db.ListDigests(r.Context(), guildID, limit)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if digests == nil {
		digests = []database.DigestRecord{}
	}

	gohttp.WriteJSON(w, http.StatusOK, digests)
}

// generateDigest triggers digest generation for a guild.
// Query param: ?guild_id=X (required).
// The digest covers the period since the last digest (or last 24 hours
// if no prior digest exists).
func (h *handlers) generateDigest(w http.ResponseWriter, r *http.Request) {
	guildID := r.URL.Query().Get("guild_id")
	if guildID == "" {
		gohttp.WriteError(w, http.StatusBadRequest, "guild_id query parameter is required")
		return
	}

	// Determine the start of the digest period: either the end of the
	// last digest or 24 hours ago.
	since := time.Now().Add(-24 * time.Hour)
	latest, err := h.db.GetLatestDigest(r.Context(), guildID)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if latest != nil {
		since = latest.PeriodEnd
	}

	// Generate the digest.
	digest, err := monitor.GenerateDigest(r.Context(), h.db, guildID, since)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Store the digest in the database. Convert the digest struct to a
	// generic map for JSONB storage.
	content := map[string]interface{}{
		"total_messages": digest.TotalMessages,
		"channels":       digest.Channels,
		"guild_name":     digest.GuildName,
	}
	if err := h.db.StoreDigest(r.Context(), guildID, digest.PeriodStart, digest.PeriodEnd, content); err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	gohttp.WriteJSON(w, http.StatusCreated, digest)
}

// getHeatmap returns the 7x24 activity heatmap for a channel or guild.
// Query params: ?channel_id=X or ?guild_id=X (one required), ?days=N (default 7).
func (h *handlers) getHeatmap(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")
	guildID := r.URL.Query().Get("guild_id")

	if channelID == "" && guildID == "" {
		gohttp.WriteError(w, http.StatusBadRequest, "channel_id or guild_id query parameter is required")
		return
	}

	days := 7
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 {
			days = n
		}
	}

	var buckets []database.HeatmapBucket
	var err error

	if channelID != "" {
		// Channel-specific heatmap takes precedence if both are provided.
		buckets, err = h.db.GetHeatmap(r.Context(), channelID, days)
	} else {
		buckets, err = h.db.GetGuildHeatmap(r.Context(), guildID, days)
	}

	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if buckets == nil {
		buckets = []database.HeatmapBucket{}
	}

	gohttp.WriteJSON(w, http.StatusOK, buckets)
}

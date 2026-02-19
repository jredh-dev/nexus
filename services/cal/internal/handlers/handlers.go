package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jredh-dev/nexus/services/cal/internal/database"
	"github.com/jredh-dev/nexus/services/cal/internal/ical"
)

// slugPattern matches valid slugs: lowercase letters, digits, and hyphens,
// 2-64 characters, must start and end with alphanumeric.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	db *database.DB
}

// New creates a new Handler.
func New(db *database.DB) *Handler {
	return &Handler{db: db}
}

// --- Subscription endpoint (served to calendar clients) ---

// Subscribe serves the iCal feed for a given token.
// GET /{token}.ics
func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		http.NotFound(w, r)
		return
	}

	feed, err := h.db.FeedByToken(token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	events, err := h.db.EventsByFeed(feed.ID)
	if err != nil {
		log.Printf("error fetching events for feed %s: %v", feed.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	icalFeed := ical.Feed{
		Name: feed.Name,
		TTL:  1 * time.Hour,
	}

	icalEvents := make([]ical.Event, len(events))
	for i, e := range events {
		icalEvents[i] = ical.Event{
			UID:         e.ID + "@nexus-cal",
			Summary:     e.Summary,
			Description: e.Description,
			Location:    e.Location,
			URL:         e.URL,
			Start:       e.Start,
			End:         e.End,
			AllDay:      e.AllDay,
			Deadline:    e.Deadline,
			Status:      e.Status,
			Categories:  e.Categories,
			Created:     e.CreatedAt,
			Updated:     e.UpdatedAt,
		}
	}

	body := ical.Generate(icalFeed, icalEvents)

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"calendar.ics\"")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(body))
}

// --- Management API (JSON) ---

type createFeedReq struct {
	Name string `json:"name"`
	Slug string `json:"slug"` // optional: readable URL slug (e.g. "my-calendar")
}

type createFeedResp struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
	URL   string `json:"url"`
}

// CreateFeed creates a new calendar feed.
// POST /api/feeds
func (h *Handler) CreateFeed(w http.ResponseWriter, r *http.Request) {
	var req createFeedReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}

	token := uuid.New().String()
	if req.Slug != "" {
		if !slugPattern.MatchString(req.Slug) {
			jsonError(w, "slug must be 2-64 characters, lowercase alphanumeric and hyphens, must start and end with alphanumeric", http.StatusBadRequest)
			return
		}
		token = req.Slug
	}

	now := time.Now().UTC()
	feed := &database.Feed{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Token:     token,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := h.db.CreateFeed(feed); err != nil {
		log.Printf("error creating feed: %v", err)
		// Check for slug collision (UNIQUE constraint on token)
		if req.Slug != "" {
			jsonError(w, "slug already in use", http.StatusConflict)
			return
		}
		jsonError(w, "failed to create feed", http.StatusInternalServerError)
		return
	}

	resp := createFeedResp{
		ID:    feed.ID,
		Name:  feed.Name,
		Token: feed.Token,
		URL:   "/" + feed.Token + ".ics",
	}
	jsonOK(w, http.StatusCreated, resp)
}

// ListFeeds returns all feeds.
// GET /api/feeds
func (h *Handler) ListFeeds(w http.ResponseWriter, r *http.Request) {
	feeds, err := h.db.ListFeeds()
	if err != nil {
		log.Printf("error listing feeds: %v", err)
		jsonError(w, "failed to list feeds", http.StatusInternalServerError)
		return
	}
	if feeds == nil {
		feeds = []*database.Feed{}
	}
	jsonOK(w, http.StatusOK, feeds)
}

// DeleteFeed removes a feed and all its events.
// DELETE /api/feeds/{id}
func (h *Handler) DeleteFeed(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.db.DeleteFeed(id); err != nil {
		log.Printf("error deleting feed %s: %v", id, err)
		jsonError(w, "failed to delete feed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type createEventReq struct {
	FeedID      string  `json:"feed_id"`
	Summary     string  `json:"summary"`
	Description string  `json:"description"`
	Location    string  `json:"location"`
	URL         string  `json:"url"`
	Start       string  `json:"start"` // RFC 3339
	End         *string `json:"end"`   // RFC 3339, optional
	AllDay      bool    `json:"all_day"`
	Deadline    *string `json:"deadline"` // RFC 3339, optional
	Status      string  `json:"status"`
	Categories  string  `json:"categories"`
}

// CreateEvent adds an event to a feed.
// POST /api/events
func (h *Handler) CreateEvent(w http.ResponseWriter, r *http.Request) {
	var req createEventReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.FeedID == "" || req.Summary == "" || req.Start == "" {
		jsonError(w, "feed_id, summary, and start are required", http.StatusBadRequest)
		return
	}

	start, err := time.Parse(time.RFC3339, req.Start)
	if err != nil {
		jsonError(w, "start must be RFC 3339 format", http.StatusBadRequest)
		return
	}

	var end *time.Time
	if req.End != nil {
		t, err := time.Parse(time.RFC3339, *req.End)
		if err != nil {
			jsonError(w, "end must be RFC 3339 format", http.StatusBadRequest)
			return
		}
		end = &t
	}

	var deadline *time.Time
	if req.Deadline != nil {
		t, err := time.Parse(time.RFC3339, *req.Deadline)
		if err != nil {
			jsonError(w, "deadline must be RFC 3339 format", http.StatusBadRequest)
			return
		}
		deadline = &t
	}

	status := req.Status
	if status == "" {
		status = "CONFIRMED"
	}

	now := time.Now().UTC()
	event := &database.Event{
		ID:          uuid.New().String(),
		FeedID:      req.FeedID,
		Summary:     req.Summary,
		Description: req.Description,
		Location:    req.Location,
		URL:         req.URL,
		Start:       start,
		End:         end,
		AllDay:      req.AllDay,
		Deadline:    deadline,
		Status:      status,
		Categories:  req.Categories,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := h.db.CreateEvent(event); err != nil {
		log.Printf("error creating event: %v", err)
		jsonError(w, "failed to create event", http.StatusInternalServerError)
		return
	}

	jsonOK(w, http.StatusCreated, event)
}

// ListEvents returns all events for a feed.
// GET /api/feeds/{id}/events
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	feedID := chi.URLParam(r, "id")
	events, err := h.db.EventsByFeed(feedID)
	if err != nil {
		log.Printf("error listing events for feed %s: %v", feedID, err)
		jsonError(w, "failed to list events", http.StatusInternalServerError)
		return
	}
	if events == nil {
		events = []*database.Event{}
	}
	jsonOK(w, http.StatusOK, events)
}

// DeleteEvent removes a single event.
// DELETE /api/events/{id}
func (h *Handler) DeleteEvent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.db.DeleteEvent(id); err != nil {
		log.Printf("error deleting event %s: %v", id, err)
		jsonError(w, "failed to delete event", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func jsonOK(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

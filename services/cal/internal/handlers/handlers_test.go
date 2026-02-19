package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/jredh-dev/nexus/services/cal/internal/database"
)

func testHandler(t *testing.T) *Handler {
	t.Helper()
	path := t.TempDir() + "/test.db"
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.Remove(path)
	})
	return New(db)
}

func testRouter(h *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/{token}.ics", h.Subscribe)
	r.Route("/api", func(r chi.Router) {
		r.Post("/feeds", h.CreateFeed)
		r.Get("/feeds", h.ListFeeds)
		r.Delete("/feeds/{id}", h.DeleteFeed)
		r.Get("/feeds/{id}/events", h.ListEvents)
		r.Post("/events", h.CreateEvent)
		r.Delete("/events/{id}", h.DeleteEvent)
	})
	return r
}

func TestCreateAndListFeeds(t *testing.T) {
	h := testHandler(t)
	r := testRouter(h)

	// Create feed
	body := `{"name":"Work Calendar"}`
	req := httptest.NewRequest(http.MethodPost, "/api/feeds", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create feed: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created createFeedResp
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	if created.Name != "Work Calendar" {
		t.Errorf("expected name 'Work Calendar', got %q", created.Name)
	}
	if created.Token == "" {
		t.Error("expected non-empty token")
	}
	if created.URL == "" {
		t.Error("expected non-empty URL")
	}

	// List feeds
	req = httptest.NewRequest(http.MethodGet, "/api/feeds", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list feeds: expected 200, got %d", w.Code)
	}

	var feeds []database.Feed
	if err := json.Unmarshal(w.Body.Bytes(), &feeds); err != nil {
		t.Fatalf("unmarshal feeds: %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("expected 1 feed, got %d", len(feeds))
	}
}

func TestCreateEventAndSubscribe(t *testing.T) {
	h := testHandler(t)
	r := testRouter(h)

	// Create a feed
	body := `{"name":"Test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/feeds", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var feed createFeedResp
	if err := json.Unmarshal(w.Body.Bytes(), &feed); err != nil {
		t.Fatalf("unmarshal feed: %v", err)
	}

	// Create an event
	eventBody, _ := json.Marshal(map[string]interface{}{
		"feed_id":    feed.ID,
		"summary":    "Weekend Hackathon",
		"start":      "2026-02-21T10:00:00Z",
		"end":        "2026-02-21T18:00:00Z",
		"categories": "fun,code",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/events", bytes.NewReader(eventBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create event: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Subscribe to the feed
	req = httptest.NewRequest(http.MethodGet, "/"+feed.Token+".ics", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("subscribe: expected 200, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/calendar") {
		t.Errorf("expected Content-Type text/calendar, got %q", ct)
	}

	ics := w.Body.String()
	required := []string{
		"BEGIN:VCALENDAR",
		"BEGIN:VEVENT",
		"SUMMARY:Weekend Hackathon",
		"DTSTART:20260221T100000Z",
		"DTEND:20260221T180000Z",
		"CATEGORIES:fun,code",
		"END:VEVENT",
		"END:VCALENDAR",
	}
	for _, s := range required {
		if !strings.Contains(ics, s) {
			t.Errorf("iCal output missing %q", s)
		}
	}
}

func TestSubscribe_InvalidToken(t *testing.T) {
	h := testHandler(t)
	r := testRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent.ics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for invalid token, got %d", w.Code)
	}
}

func TestCreateFeed_ValidationErrors(t *testing.T) {
	h := testHandler(t)
	r := testRouter(h)

	// Empty name
	req := httptest.NewRequest(http.MethodPost, "/api/feeds", strings.NewReader(`{"name":""}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name, got %d", w.Code)
	}

	// Invalid JSON
	req = httptest.NewRequest(http.MethodPost, "/api/feeds", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestCreateEvent_ValidationErrors(t *testing.T) {
	h := testHandler(t)
	r := testRouter(h)

	// Missing required fields
	req := httptest.NewRequest(http.MethodPost, "/api/events", strings.NewReader(`{"summary":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing fields, got %d", w.Code)
	}

	// Bad date format
	body := `{"feed_id":"x","summary":"test","start":"not-a-date"}`
	req = httptest.NewRequest(http.MethodPost, "/api/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad date, got %d", w.Code)
	}
}

func TestDeleteFeedAndEvents(t *testing.T) {
	h := testHandler(t)
	r := testRouter(h)

	// Create feed
	req := httptest.NewRequest(http.MethodPost, "/api/feeds", strings.NewReader(`{"name":"Temp"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var feed createFeedResp
	if err := json.Unmarshal(w.Body.Bytes(), &feed); err != nil {
		t.Fatalf("unmarshal feed: %v", err)
	}

	// Delete it
	req = httptest.NewRequest(http.MethodDelete, "/api/feeds/"+feed.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}

	// Verify gone
	req = httptest.NewRequest(http.MethodGet, "/api/feeds", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var feeds []database.Feed
	if err := json.Unmarshal(w.Body.Bytes(), &feeds); err != nil {
		t.Fatalf("unmarshal feeds: %v", err)
	}
	if len(feeds) != 0 {
		t.Errorf("expected 0 feeds after delete, got %d", len(feeds))
	}
}

func TestCreateFeed_WithSlug(t *testing.T) {
	h := testHandler(t)
	r := testRouter(h)

	// Create feed with slug
	body := `{"name":"My Calendar","slug":"my-calendar"}`
	req := httptest.NewRequest(http.MethodPost, "/api/feeds", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create feed with slug: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created createFeedResp
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if created.Token != "my-calendar" {
		t.Errorf("expected token 'my-calendar', got %q", created.Token)
	}
	if created.URL != "/my-calendar.ics" {
		t.Errorf("expected URL '/my-calendar.ics', got %q", created.URL)
	}

	// Subscribe using the slug
	req = httptest.NewRequest(http.MethodGet, "/my-calendar.ics", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("subscribe with slug: expected 200, got %d", w.Code)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/calendar") {
		t.Errorf("expected text/calendar, got %q", w.Header().Get("Content-Type"))
	}
}

func TestCreateFeed_SlugCollision(t *testing.T) {
	h := testHandler(t)
	r := testRouter(h)

	// Create first feed with slug
	body := `{"name":"First","slug":"work"}`
	req := httptest.NewRequest(http.MethodPost, "/api/feeds", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("first feed: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Try duplicate slug
	body = `{"name":"Second","slug":"work"}`
	req = httptest.NewRequest(http.MethodPost, "/api/feeds", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("duplicate slug: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateFeed_InvalidSlug(t *testing.T) {
	h := testHandler(t)
	r := testRouter(h)

	cases := []struct {
		name string
		slug string
	}{
		{"uppercase", "My-Calendar"},
		{"spaces", "my calendar"},
		{"special chars", "my_calendar!"},
		{"starts with hyphen", "-my-calendar"},
		{"ends with hyphen", "my-calendar-"},
		{"single char", "x"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"name": "Test", "slug": tc.slug})
			req := httptest.NewRequest(http.MethodPost, "/api/feeds", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("slug %q: expected 400, got %d: %s", tc.slug, w.Code, w.Body.String())
			}
		})
	}
}

func TestCreateFeed_WithoutSlug_GetsUUID(t *testing.T) {
	h := testHandler(t)
	r := testRouter(h)

	body := `{"name":"No Slug"}`
	req := httptest.NewRequest(http.MethodPost, "/api/feeds", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created createFeedResp
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Token should be a UUID (36 chars with hyphens)
	if len(created.Token) != 36 {
		t.Errorf("expected UUID token (36 chars), got %q (%d chars)", created.Token, len(created.Token))
	}
}

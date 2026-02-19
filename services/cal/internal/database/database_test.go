package database

import (
	"os"
	"testing"
	"time"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	path := t.TempDir() + "/test.db"
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.Remove(path)
	})
	return db
}

func TestFeedCRUD(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC().Truncate(time.Second)

	feed := &Feed{
		ID:        "feed-1",
		Name:      "Test Feed",
		Token:     "secret-token-abc",
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Create
	if err := db.CreateFeed(feed); err != nil {
		t.Fatalf("create feed: %v", err)
	}

	// Read by token
	got, err := db.FeedByToken("secret-token-abc")
	if err != nil {
		t.Fatalf("feed by token: %v", err)
	}
	if got.ID != "feed-1" || got.Name != "Test Feed" {
		t.Errorf("feed by token returned %+v", got)
	}

	// Read by ID
	got, err = db.FeedByID("feed-1")
	if err != nil {
		t.Fatalf("feed by id: %v", err)
	}
	if got.Token != "secret-token-abc" {
		t.Errorf("feed by id returned token %q", got.Token)
	}

	// List
	feeds, err := db.ListFeeds()
	if err != nil {
		t.Fatalf("list feeds: %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("expected 1 feed, got %d", len(feeds))
	}

	// Delete
	if err := db.DeleteFeed("feed-1"); err != nil {
		t.Fatalf("delete feed: %v", err)
	}
	feeds, err = db.ListFeeds()
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(feeds) != 0 {
		t.Errorf("expected 0 feeds after delete, got %d", len(feeds))
	}
}

func TestEventCRUD(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC().Truncate(time.Second)

	feed := &Feed{
		ID: "feed-1", Name: "Test", Token: "tok",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateFeed(feed); err != nil {
		t.Fatalf("create feed: %v", err)
	}

	end := now.Add(1 * time.Hour)
	deadline := now.Add(24 * time.Hour)

	event := &Event{
		ID:          "evt-1",
		FeedID:      "feed-1",
		Summary:     "Test Event",
		Description: "A test",
		Location:    "Office",
		URL:         "https://example.com",
		Start:       now,
		End:         &end,
		AllDay:      false,
		Deadline:    &deadline,
		Status:      "CONFIRMED",
		Categories:  "work,test",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Create
	if err := db.CreateEvent(event); err != nil {
		t.Fatalf("create event: %v", err)
	}

	// Read by ID
	got, err := db.EventByID("evt-1")
	if err != nil {
		t.Fatalf("event by id: %v", err)
	}
	if got.Summary != "Test Event" {
		t.Errorf("expected summary 'Test Event', got %q", got.Summary)
	}
	if got.Deadline == nil {
		t.Error("expected deadline to be set")
	}

	// Update
	event.Summary = "Updated Event"
	event.UpdatedAt = now.Add(1 * time.Minute)
	if err := db.UpdateEvent(event); err != nil {
		t.Fatalf("update event: %v", err)
	}
	got, err = db.EventByID("evt-1")
	if err != nil {
		t.Fatalf("event by id after update: %v", err)
	}
	if got.Summary != "Updated Event" {
		t.Errorf("expected updated summary, got %q", got.Summary)
	}

	// List by feed
	events, err := db.EventsByFeed("feed-1")
	if err != nil {
		t.Fatalf("events by feed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Delete event
	if err := db.DeleteEvent("evt-1"); err != nil {
		t.Fatalf("delete event: %v", err)
	}
	events, err = db.EventsByFeed("feed-1")
	if err != nil {
		t.Fatalf("events by feed after delete: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events after delete, got %d", len(events))
	}
}

func TestCascadeDelete(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC().Truncate(time.Second)

	feed := &Feed{
		ID: "feed-1", Name: "Test", Token: "tok",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateFeed(feed); err != nil {
		t.Fatalf("create feed: %v", err)
	}

	event := &Event{
		ID: "evt-1", FeedID: "feed-1", Summary: "Test",
		Start: now, Status: "CONFIRMED",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateEvent(event); err != nil {
		t.Fatalf("create event: %v", err)
	}

	// Deleting the feed should cascade-delete events
	if err := db.DeleteFeed("feed-1"); err != nil {
		t.Fatalf("delete feed: %v", err)
	}

	_, err := db.EventByID("evt-1")
	if err == nil {
		t.Error("expected event to be deleted via cascade")
	}
}

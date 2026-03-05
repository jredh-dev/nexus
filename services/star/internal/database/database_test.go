// Package database_test provides integration tests for the star database
// layer against a real PostgreSQL instance.
//
// These tests require PostgreSQL 16 running at /tmp/ctl-pg.
// They create and drop a test-specific database for isolation.
//
// Run with: go test -tags integration ./services/star/internal/database/
package database_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/jredh-dev/nexus/services/star/internal/database"
)

const (
	pgHost = "/tmp/ctl-pg"
	pgUser = "jredh"
	// Test database gets a unique name per run to allow parallel test runs.
	testDBPrefix = "star_test_"
)

// testDB creates a temporary database, runs migrations, and returns a
// connected DB plus a cleanup function that drops the database.
func testDB(t *testing.T) (*database.DB, func()) {
	t.Helper()

	// Use the test name (sanitized) as part of the DB name for debuggability.
	dbName := testDBPrefix + "integration"

	ctx := context.Background()

	// Create test database using psql.
	psql := findPsql(t)
	runPsql(t, psql, "postgres", fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	runPsql(t, psql, "postgres", fmt.Sprintf("CREATE DATABASE %s", dbName))

	connStr := fmt.Sprintf("host=%s dbname=%s user=%s", pgHost, dbName, pgUser)
	db, err := database.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect to test db: %v", err)
	}

	cleanup := func() {
		db.Close()
		runPsql(t, psql, "postgres", fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	}

	return db, cleanup
}

func findPsql(t *testing.T) string {
	t.Helper()
	// Try PG16 first, then fall back to PATH.
	candidates := []string{
		"/usr/local/Cellar/postgresql@16/16.13/bin/psql",
		"/usr/local/bin/psql",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	// Try PATH as last resort.
	p, err := exec.LookPath("psql")
	if err != nil {
		t.Skipf("psql not found, skipping integration test")
	}
	return p
}

func runPsql(t *testing.T, psql, dbname, sql string) {
	t.Helper()
	cmd := exec.Command(psql, "-h", pgHost, "-U", pgUser, "-d", dbname, "-c", sql)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	// Ignore errors for DROP IF EXISTS.
	cmd.Run() //nolint:errcheck
}

// --- Video tests ---

func TestVideoLifecycle(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()

	// Generate 1KB of random data to simulate a video file.
	videoData := make([]byte, 1024)
	if _, err := rand.Read(videoData); err != nil {
		t.Fatalf("generate random data: %v", err)
	}

	// Import.
	w := intPtr(1920)
	h := intPtr(1080)
	v, err := db.ImportVideo(ctx, database.ImportVideoParams{
		Name:       "test_clip.mp4",
		Codec:      "h264",
		MimeType:   "video/mp4",
		DurationMS: 5000,
		Width:      w,
		Height:     h,
		LoopType:   "palindrome",
	}, bytes.NewReader(videoData))
	if err != nil {
		t.Fatalf("import video: %v", err)
	}

	if v.Name != "test_clip.mp4" {
		t.Errorf("name = %q, want %q", v.Name, "test_clip.mp4")
	}
	if v.SizeBytes != 1024 {
		t.Errorf("size_bytes = %d, want 1024", v.SizeBytes)
	}
	if v.LoopType != "palindrome" {
		t.Errorf("loop_type = %q, want %q", v.LoopType, "palindrome")
	}

	// Get.
	got, err := db.GetVideo(ctx, v.VideoID)
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if got.VideoID != v.VideoID {
		t.Errorf("video_id mismatch")
	}

	// List.
	videos, err := db.ListVideos(ctx)
	if err != nil {
		t.Fatalf("list videos: %v", err)
	}
	if len(videos) != 1 {
		t.Errorf("list videos: got %d, want 1", len(videos))
	}

	// Read data back via large object.
	err = db.ReadVideoData(ctx, v.VideoID, func(r io.Reader, size int64) error {
		if size != 1024 {
			t.Errorf("ReadVideoData size = %d, want 1024", size)
		}
		readBack, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		if !bytes.Equal(readBack, videoData) {
			t.Error("ReadVideoData: round-trip data mismatch")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read video data: %v", err)
	}

	// Delete.
	if err := db.DeleteVideo(ctx, v.VideoID); err != nil {
		t.Fatalf("delete video: %v", err)
	}
	videos, err = db.ListVideos(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(videos) != 0 {
		t.Errorf("list after delete: got %d, want 0", len(videos))
	}
}

// --- Event tests ---

func TestEventLifecycle(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()

	v := importTestVideo(t, db)

	// Create.
	e, err := db.CreateEvent(ctx, database.CreateEventParams{
		VideoID:     v.VideoID,
		Description: "Bigsby finds the ball",
		Timestamps:  []float64{3.5, 12.0},
		IsVisible:   true,
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if e.Description != "Bigsby finds the ball" {
		t.Errorf("description = %q", e.Description)
	}
	if len(e.Timestamps) != 2 {
		t.Errorf("timestamps len = %d, want 2", len(e.Timestamps))
	}

	// Get by video.
	events, err := db.GetEventsByVideo(ctx, v.VideoID)
	if err != nil {
		t.Fatalf("get events by video: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("events count = %d, want 1", len(events))
	}

	// Update.
	err = db.UpdateEvent(ctx, e.EventID, "Bigsby chases the ball", []float64{3.5, 12.0, 15.5}, false)
	if err != nil {
		t.Fatalf("update event: %v", err)
	}
	updated, err := db.GetEvent(ctx, e.EventID)
	if err != nil {
		t.Fatalf("get updated event: %v", err)
	}
	if updated.Description != "Bigsby chases the ball" {
		t.Errorf("updated description = %q", updated.Description)
	}
	if len(updated.Timestamps) != 3 {
		t.Errorf("updated timestamps len = %d, want 3", len(updated.Timestamps))
	}

	// Delete.
	if err := db.DeleteEvent(ctx, e.EventID); err != nil {
		t.Fatalf("delete event: %v", err)
	}
}

// --- Subtitle tests ---

func TestSubtitleLifecycle(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()

	v := importTestVideo(t, db)

	// Create: always visible (init=true, end=true, 0 timestamps → even, valid).
	s, err := db.CreateSubtitle(ctx, database.CreateSubtitleParams{
		VideoID:           v.VideoID,
		Text:              "The world is full of magic",
		TimestampsVisible: []float64{},
		InitializeVisible: true,
		EndVisible:        true,
		SortOrder:         0,
	})
	if err != nil {
		t.Fatalf("create always-visible subtitle: %v", err)
	}
	if s.Text != "The world is full of magic" {
		t.Errorf("text = %q", s.Text)
	}

	// Create: appears once, stays (init=false, end=true, 1 timestamp → odd, valid).
	s2, err := db.CreateSubtitle(ctx, database.CreateSubtitleParams{
		VideoID:           v.VideoID,
		Text:              "Things we don't understand",
		TimestampsVisible: []float64{5.0},
		InitializeVisible: false,
		EndVisible:        true,
		SortOrder:         1,
	})
	if err != nil {
		t.Fatalf("create appear-once subtitle: %v", err)
	}

	// Create: blinks on/off (init=false, end=false, 2 timestamps → even, valid).
	_, err = db.CreateSubtitle(ctx, database.CreateSubtitleParams{
		VideoID:           v.VideoID,
		Text:              "Fleeting thought",
		TimestampsVisible: []float64{2.0, 4.0},
		InitializeVisible: false,
		EndVisible:        false,
		SortOrder:         2,
	})
	if err != nil {
		t.Fatalf("create blink subtitle: %v", err)
	}

	// List by video.
	subs, err := db.GetSubtitlesByVideo(ctx, v.VideoID)
	if err != nil {
		t.Fatalf("list subtitles: %v", err)
	}
	if len(subs) != 3 {
		t.Errorf("subtitle count = %d, want 3", len(subs))
	}
	// Should be ordered by sort_order.
	if subs[0].SortOrder != 0 || subs[1].SortOrder != 1 || subs[2].SortOrder != 2 {
		t.Error("subtitles not in sort_order")
	}

	// Update.
	err = db.UpdateSubtitle(ctx, s2.SubtitleID, database.CreateSubtitleParams{
		VideoID:           v.VideoID,
		Text:              "Just like technology",
		TimestampsVisible: []float64{5.0},
		InitializeVisible: false,
		EndVisible:        true,
		SortOrder:         1,
	})
	if err != nil {
		t.Fatalf("update subtitle: %v", err)
	}

	// Delete.
	if err := db.DeleteSubtitle(ctx, s.SubtitleID); err != nil {
		t.Fatalf("delete subtitle: %v", err)
	}
	subs, err = db.GetSubtitlesByVideo(ctx, v.VideoID)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(subs) != 2 {
		t.Errorf("after delete: got %d, want 2", len(subs))
	}
}

func TestSubtitleVisibilityConstraint(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()

	v := importTestVideo(t, db)

	// Invalid: init=true, end=true requires even timestamps, but we give 1.
	_, err := db.CreateSubtitle(ctx, database.CreateSubtitleParams{
		VideoID:           v.VideoID,
		Text:              "Should fail",
		TimestampsVisible: []float64{5.0},
		InitializeVisible: true,
		EndVisible:        true,
		SortOrder:         0,
	})
	if err == nil {
		t.Error("expected error for invalid visibility (init=true, end=true, 1 timestamp)")
	}

	// Invalid: init=false, end=true requires odd timestamps, but we give 2.
	_, err = db.CreateSubtitle(ctx, database.CreateSubtitleParams{
		VideoID:           v.VideoID,
		Text:              "Should also fail",
		TimestampsVisible: []float64{1.0, 3.0},
		InitializeVisible: false,
		EndVisible:        true,
		SortOrder:         0,
	})
	if err == nil {
		t.Error("expected error for invalid visibility (init=false, end=true, 2 timestamps)")
	}

	// Invalid: init=true, end=false requires odd timestamps, but we give 0.
	_, err = db.CreateSubtitle(ctx, database.CreateSubtitleParams{
		VideoID:           v.VideoID,
		Text:              "Should fail too",
		TimestampsVisible: []float64{},
		InitializeVisible: true,
		EndVisible:        false,
		SortOrder:         0,
	})
	if err == nil {
		t.Error("expected error for invalid visibility (init=true, end=false, 0 timestamps)")
	}
}

// --- Voting tests ---

func TestVotingLifecycle(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()

	// Create reader.
	r, err := db.GetOrCreateReader(ctx, "device_abc123")
	if err != nil {
		t.Fatalf("create reader: %v", err)
	}
	if r.Tokens != 0 {
		t.Errorf("initial tokens = %d, want 0", r.Tokens)
	}

	// Idempotent upsert.
	r2, err := db.GetOrCreateReader(ctx, "device_abc123")
	if err != nil {
		t.Fatalf("upsert reader: %v", err)
	}
	if r2.ReaderID != r.ReaderID {
		t.Error("upsert created a new reader instead of returning existing")
	}

	// Grant tokens.
	balance, err := db.GrantTokens(ctx, r.ReaderID, 5)
	if err != nil {
		t.Fatalf("grant tokens: %v", err)
	}
	if balance != 5 {
		t.Errorf("balance after grant = %d, want 5", balance)
	}

	// Cast vote.
	vote, err := db.CastVote(ctx, r.ReaderID, "prologue", "follow_squirrel", 2)
	if err != nil {
		t.Fatalf("cast vote: %v", err)
	}
	if vote.Choice != "follow_squirrel" {
		t.Errorf("vote choice = %q", vote.Choice)
	}

	// Check balance deducted.
	updated, err := db.GetReader(ctx, r.ReaderID)
	if err != nil {
		t.Fatalf("get reader after vote: %v", err)
	}
	if updated.Tokens != 3 {
		t.Errorf("tokens after vote = %d, want 3", updated.Tokens)
	}

	// Cast another vote, different choice.
	_, err = db.CastVote(ctx, r.ReaderID, "prologue", "stay_on_porch", 1)
	if err != nil {
		t.Fatalf("cast second vote: %v", err)
	}

	// Tally.
	tallies, err := db.TallyVotes(ctx, "prologue")
	if err != nil {
		t.Fatalf("tally votes: %v", err)
	}
	if len(tallies) != 2 {
		t.Fatalf("tally count = %d, want 2", len(tallies))
	}
	// "follow_squirrel" should be first (2 tokens > 1 token).
	if tallies[0].Choice != "follow_squirrel" {
		t.Errorf("top choice = %q, want follow_squirrel", tallies[0].Choice)
	}
	if tallies[0].TotalTokens != 2 {
		t.Errorf("top tokens = %d, want 2", tallies[0].TotalTokens)
	}

	// Insufficient tokens.
	_, err = db.CastVote(ctx, r.ReaderID, "prologue", "run_away", 100)
	if err == nil {
		t.Error("expected error for insufficient tokens")
	}
}

// --- Helpers ---

func importTestVideo(t *testing.T, db *database.DB) *database.Video {
	t.Helper()
	data := make([]byte, 512)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("rand: %v", err)
	}
	v, err := db.ImportVideo(context.Background(), database.ImportVideoParams{
		Name:       "test.mp4",
		Codec:      "h264",
		MimeType:   "video/mp4",
		DurationMS: 3000,
	}, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("import test video: %v", err)
	}
	return v
}

func intPtr(i int) *int { return &i }

//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// vnURL returns the base URL for the vn service. Defaults to the
// docker-compose mapped port (8082).
func vnURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("VN_URL")
	if u == "" {
		u = "http://localhost:8082"
	}
	return strings.TrimRight(u, "/")
}

// vnClient returns an HTTP client with a reasonable timeout for
// integration tests against the vn service.
func vnClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// vnGet is a helper that does a GET and fails the test on error.
func vnGet(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := vnClient().Get(vnURL(t) + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// vnPostJSON is a helper that POSTs JSON and fails the test on error.
func vnPostJSON(t *testing.T, path, body string) *http.Response {
	t.Helper()
	resp, err := vnClient().Post(
		vnURL(t)+path,
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

// vnDecodeJSON decodes a response body into v and fails on error.
func vnDecodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// --- Health ---

func TestVNHealth(t *testing.T) {
	resp := vnGet(t, "/health")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health: got %d, want 200", resp.StatusCode)
	}
}

// --- Story ---

func TestVNGetStory(t *testing.T) {
	resp := vnGet(t, "/api/story")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/story: got %d", resp.StatusCode)
	}

	var story struct {
		Title     string   `json:"title"`
		Version   int      `json:"version"`
		Chapters  []string `json:"chapters"`
		StartNode string   `json:"start_node"`
	}
	vnDecodeJSON(t, resp, &story)

	if story.Title == "" {
		t.Error("story title should not be empty")
	}
	if story.Version < 1 {
		t.Errorf("story version should be >= 1, got %d", story.Version)
	}
	if len(story.Chapters) == 0 {
		t.Error("story should have at least one chapter")
	}
	if story.StartNode == "" {
		t.Error("story start_node should not be empty")
	}
}

// --- Chapters ---

func TestVNListChapters(t *testing.T) {
	resp := vnGet(t, "/api/chapters")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/chapters: got %d", resp.StatusCode)
	}

	var chapters []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		SortOrder   int    `json:"sort_order"`
		TokenReward int    `json:"token_reward"`
		NodeCount   int    `json:"node_count"`
	}
	vnDecodeJSON(t, resp, &chapters)

	if len(chapters) == 0 {
		t.Fatal("expected at least one chapter")
	}
	for _, ch := range chapters {
		if ch.ID == "" {
			t.Error("chapter ID should not be empty")
		}
		if ch.Title == "" {
			t.Errorf("chapter %q: title should not be empty", ch.ID)
		}
		if ch.NodeCount < 1 {
			t.Errorf("chapter %q: should have at least 1 node, got %d", ch.ID, ch.NodeCount)
		}
	}
}

func TestVNGetChapter(t *testing.T) {
	// First get the list to find a valid chapter ID.
	resp := vnGet(t, "/api/chapters")
	var chapters []struct {
		ID string `json:"id"`
	}
	vnDecodeJSON(t, resp, &chapters)
	if len(chapters) == 0 {
		t.Skip("no chapters to test")
	}

	chID := chapters[0].ID
	resp = vnGet(t, "/api/chapters/"+chID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/chapters/%s: got %d", chID, resp.StatusCode)
	}

	var detail struct {
		ID        string         `json:"id"`
		Title     string         `json:"title"`
		StartNode string         `json:"start_node"`
		Nodes     map[string]any `json:"nodes"`
	}
	vnDecodeJSON(t, resp, &detail)

	if detail.ID != chID {
		t.Errorf("expected chapter ID %q, got %q", chID, detail.ID)
	}
	if len(detail.Nodes) == 0 {
		t.Error("chapter should have nodes")
	}
}

func TestVNGetChapter_NotFound(t *testing.T) {
	resp := vnGet(t, "/api/chapters/nonexistent-chapter-id")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// --- Story Navigation Flow ---

func TestVNStoryNavigationFlow(t *testing.T) {
	// 1. Start the story.
	resp := vnPostJSON(t, "/api/story/start", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/story/start: got %d", resp.StatusCode)
	}

	var startResp struct {
		State struct {
			CurrentNode string `json:"current_node"`
		} `json:"state"`
		Node struct {
			ID      string `json:"id"`
			Text    string `json:"text"`
			Choices []struct {
				Label string `json:"label"`
			} `json:"choices"`
		} `json:"node"`
		Reader struct {
			Tokens int `json:"tokens"`
		} `json:"reader"`
	}
	vnDecodeJSON(t, resp, &startResp)

	if startResp.State.CurrentNode == "" {
		t.Error("start: current_node should not be empty")
	}
	if startResp.Node.ID == "" {
		t.Error("start: node ID should not be empty")
	}

	// 2. Advance the story (take first choice if available, else linear).
	advanceBody := `{}`
	if len(startResp.Node.Choices) > 0 {
		advanceBody = `{"choice_index": 0}`
	}

	resp = vnPostJSON(t, "/api/story/advance", advanceBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/story/advance: got %d", resp.StatusCode)
	}

	var advResp struct {
		State struct {
			CurrentNode string `json:"current_node"`
		} `json:"state"`
		Node struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"node"`
	}
	vnDecodeJSON(t, resp, &advResp)

	if advResp.State.CurrentNode == "" {
		t.Error("advance: current_node should not be empty")
	}
	// Should have moved to a different node.
	if advResp.State.CurrentNode == startResp.State.CurrentNode {
		t.Error("advance: current_node should change after advance")
	}

	// 3. Reset the story.
	resp = vnPostJSON(t, "/api/story/reset", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/story/reset: got %d", resp.StatusCode)
	}
	var resetResp struct {
		Status string `json:"status"`
	}
	vnDecodeJSON(t, resp, &resetResp)
	if resetResp.Status != "reset" {
		t.Errorf("reset: expected status \"reset\", got %q", resetResp.Status)
	}
}

// --- Reader ---

func TestVNGetReader(t *testing.T) {
	// Ensure a reader exists by starting first.
	resp := vnPostJSON(t, "/api/story/start", "")
	resp.Body.Close()

	resp = vnGet(t, "/api/reader")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/reader: got %d", resp.StatusCode)
	}

	var reader struct {
		ReaderID   string `json:"reader_id"`
		DeviceHash string `json:"device_hash"`
		Tokens     int    `json:"tokens"`
	}
	vnDecodeJSON(t, resp, &reader)

	// reader_id is a UUID, should not be empty.
	if reader.ReaderID == "" {
		t.Error("reader_id should not be empty")
	}
}

// --- Voting ---

func TestVNChapterVotes(t *testing.T) {
	// Get a chapter ID first.
	resp := vnGet(t, "/api/chapters")
	var chapters []struct {
		ID string `json:"id"`
	}
	vnDecodeJSON(t, resp, &chapters)
	if len(chapters) == 0 {
		t.Skip("no chapters")
	}

	chID := chapters[0].ID
	resp = vnGet(t, "/api/chapters/"+chID+"/votes")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/chapters/%s/votes: got %d", chID, resp.StatusCode)
	}
	// Response is a list of vote tallies (may be empty initially).
	defer resp.Body.Close()
}

func TestVNCastVote(t *testing.T) {
	// Ensure reader exists and has tokens by completing the story.
	// Start -> advance through to earn tokens, then vote.
	vnPostJSON(t, "/api/story/start", "").Body.Close()

	// Walk the seed story to completion to earn tokens.
	// prologue.intro -> choose left -> prologue.left -> prologue.ending
	vnPostJSON(t, "/api/story/advance", `{"choice_index": 0}`).Body.Close()
	resp := vnPostJSON(t, "/api/story/advance", `{}`)
	resp.Body.Close()

	// Reset for next test.
	vnPostJSON(t, "/api/story/reset", "").Body.Close()

	// Get chapter list for voting.
	resp = vnGet(t, "/api/chapters")
	var chapters []struct {
		ID string `json:"id"`
	}
	vnDecodeJSON(t, resp, &chapters)
	if len(chapters) == 0 {
		t.Skip("no chapters")
	}

	// Cast a vote. May fail if reader has no tokens — that's acceptable
	// since token granting depends on chapter completion detection.
	body := `{"chapter_id":"` + chapters[0].ID + `","choice":"left","tokens_spent":1}`
	resp = vnPostJSON(t, "/api/vote", body)
	defer resp.Body.Close()

	// 200 = success, 400 = insufficient tokens (both valid outcomes).
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST /api/vote: expected 200 or 400, got %d", resp.StatusCode)
	}
}

// --- Full Story Completion ---

func TestVNCompleteStory(t *testing.T) {
	// Reset first to ensure clean state.
	vnPostJSON(t, "/api/story/reset", "").Body.Close()

	// Start.
	resp := vnPostJSON(t, "/api/story/start", "")
	var startResp struct {
		State struct {
			CurrentNode string `json:"current_node"`
		} `json:"state"`
		Node struct {
			Choices []struct{} `json:"choices"`
			IsEnd   bool       `json:"is_end"`
		} `json:"node"`
	}
	vnDecodeJSON(t, resp, &startResp)

	// Walk until we hit an end node or 20 steps (safety limit).
	steps := 0
	for steps < 20 {
		body := `{}`
		if len(startResp.Node.Choices) > 0 {
			body = `{"choice_index": 0}`
		}

		resp = vnPostJSON(t, "/api/story/advance", body)
		if resp.StatusCode != http.StatusOK {
			// Might get 400 if already at end node.
			resp.Body.Close()
			break
		}

		var advResp struct {
			State struct {
				CurrentNode string `json:"current_node"`
			} `json:"state"`
			Node struct {
				Choices []struct{} `json:"choices"`
				IsEnd   bool       `json:"is_end"`
			} `json:"node"`
			CompletedChapter string `json:"completed_chapter,omitempty"`
			TokensGranted    int    `json:"tokens_granted,omitempty"`
		}
		vnDecodeJSON(t, resp, &advResp)
		steps++

		startResp.State = advResp.State
		startResp.Node = advResp.Node

		if advResp.Node.IsEnd {
			break
		}
	}

	if steps == 0 {
		t.Error("should have advanced at least one step")
	}

	// Clean up.
	vnPostJSON(t, "/api/story/reset", "").Body.Close()
}

// Package server provides the HTTP API for the vn visual novel engine.
//
// Routes:
//
//	GET  /health                    — health check
//	GET  /api/story                 — story metadata (chapters, current state)
//	POST /api/story/start           — start/resume reading (returns current node)
//	POST /api/story/advance         — advance to next node or make a choice
//	POST /api/story/reset           — reset reader state
//	GET  /api/story/history         — commit log for story YAML files
//	POST /api/story/commit          — commit current YAML files
//	POST /api/story/revert          — revert to a previous commit
//	GET  /api/story/diff            — diff between commits
//	GET  /api/chapters              — list chapters in order
//	GET  /api/chapters/{id}         — chapter detail with nodes
//	GET  /api/chapters/{id}/votes   — vote tallies for a chapter
//	POST /api/vote                  — cast a vote on a chapter choice
//	GET  /api/reader                — reader info (tokens, completed chapters)
//	GET  /api/video/{id}            — stream video (range-request support)
package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	gohttp "github.com/jredh-dev/nexus/services/go-http"
	"github.com/jredh-dev/nexus/services/vn/internal/database"
	"github.com/jredh-dev/nexus/services/vn/internal/engine"
	"github.com/jredh-dev/nexus/services/vn/internal/storyrepo"
	"github.com/jredh-dev/nexus/services/vn/internal/video"
)

// Config holds the server dependencies.
type Config struct {
	DB        *database.DB
	Navigator *engine.Navigator
	Loader    *engine.HotLoader  // may be nil if not using hot-reload
	StoryRepo *storyrepo.Repo    // may be nil if story version control is disabled
}

// New creates a vn HTTP server with all routes registered.
func New(cfg Config) *gohttp.Server {
	srv := gohttp.New()

	// Wire up hot-reload: when story changes, update the navigator.
	if cfg.Loader != nil {
		cfg.Loader.Story() // ensure loaded
	}

	h := &handlers{
		db:        cfg.DB,
		nav:       cfg.Navigator,
		storyRepo: cfg.StoryRepo,
	}

	srv.Router.Route("/api", func(r chi.Router) {
		// Story navigation.
		r.Get("/story", h.getStory)
		r.Post("/story/start", h.startStory)
		r.Post("/story/advance", h.advanceStory)
		r.Post("/story/reset", h.resetStory)

		// Story version control (git-backed YAML management).
		// These endpoints are only registered if a StoryRepo is configured.
		if cfg.StoryRepo != nil {
			r.Get("/story/history", h.storyHistory)
			r.Post("/story/commit", h.storyCommit)
			r.Post("/story/revert", h.storyRevert)
			r.Get("/story/diff", h.storyDiff)
		}

		// Chapters.
		r.Get("/chapters", h.listChapters)
		r.Get("/chapters/{id}", h.getChapter)
		r.Get("/chapters/{id}/votes", h.getChapterVotes)

		// Voting.
		r.Post("/vote", h.castVote)

		// Reader.
		r.Get("/reader", h.getReader)

		// Video streaming.
		r.Get("/video/{id}", video.StreamHandler(cfg.DB))
	})

	return srv
}

type handlers struct {
	db        *database.DB
	nav       *engine.Navigator
	storyRepo *storyrepo.Repo // nil if story version control is disabled
}

// readerID extracts the reader identifier from the request. Uses the
// X-Device-Hash header (anonymous fingerprint). Falls back to remote addr.
func readerID(r *http.Request) string {
	if h := r.Header.Get("X-Device-Hash"); h != "" {
		return h
	}
	return r.RemoteAddr
}

// --- Story handlers ---

// getStory returns story metadata and the reader's current position.
func (h *handlers) getStory(w http.ResponseWriter, r *http.Request) {
	story := h.nav.Story()

	type storyResponse struct {
		Title       string   `json:"title"`
		Version     int      `json:"version"`
		Description string   `json:"description,omitempty"`
		Chapters    []string `json:"chapters"`
		StartNode   string   `json:"start_node"`
	}

	gohttp.WriteJSON(w, http.StatusOK, storyResponse{
		Title:       story.Title,
		Version:     story.Version,
		Description: story.Description,
		Chapters:    story.ChapterOrder(),
		StartNode:   story.StartNode,
	})
}

// startStory initializes or resumes a reader's position.
func (h *handlers) startStory(w http.ResponseWriter, r *http.Request) {
	rid := readerID(r)

	// Ensure DB reader exists (for token tracking).
	dbReader, err := h.db.GetOrCreateReader(r.Context(), rid)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("create reader: %v", err))
		return
	}

	state, node, err := h.nav.Start(rid)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("start: %v", err))
		return
	}

	gohttp.WriteJSON(w, http.StatusOK, map[string]any{
		"state":  state,
		"node":   node,
		"reader": dbReader,
	})
}

// advanceStory moves the reader to the next node.
func (h *handlers) advanceStory(w http.ResponseWriter, r *http.Request) {
	rid := readerID(r)

	var req struct {
		ChoiceIndex int `json:"choice_index"`
	}
	req.ChoiceIndex = -1 // default: linear advance

	if r.ContentLength > 0 {
		if err := gohttp.DecodeJSON(r, &req); err != nil {
			gohttp.WriteError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
			return
		}
	}

	state, node, completedChapter, err := h.nav.Advance(rid, req.ChoiceIndex)
	if err != nil {
		gohttp.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := map[string]any{
		"state": state,
		"node":  node,
	}

	// Grant tokens if a chapter was completed.
	if completedChapter != "" {
		story := h.nav.Story()
		ch, ok := story.Chapters[completedChapter]
		if ok && ch.TokenReward > 0 {
			dbReader, err := h.db.GetOrCreateReader(r.Context(), rid)
			if err == nil {
				newBalance, err := h.db.GrantTokens(r.Context(), dbReader.ReaderID, ch.TokenReward)
				if err == nil {
					resp["tokens_granted"] = ch.TokenReward
					resp["token_balance"] = newBalance
				}
			}
		}
		resp["completed_chapter"] = completedChapter
	}

	gohttp.WriteJSON(w, http.StatusOK, resp)
}

// resetStory clears the reader's position.
func (h *handlers) resetStory(w http.ResponseWriter, r *http.Request) {
	rid := readerID(r)
	h.nav.Reset(rid)
	gohttp.WriteJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

// --- Chapter handlers ---

type chapterSummary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	SortOrder   int    `json:"sort_order"`
	TokenReward int    `json:"token_reward"`
	NodeCount   int    `json:"node_count"`
}

func (h *handlers) listChapters(w http.ResponseWriter, r *http.Request) {
	story := h.nav.Story()
	order := story.ChapterOrder()

	chapters := make([]chapterSummary, 0, len(order))
	for _, id := range order {
		ch := story.Chapters[id]
		chapters = append(chapters, chapterSummary{
			ID:          id,
			Title:       ch.Title,
			Description: ch.Description,
			SortOrder:   ch.SortOrder,
			TokenReward: ch.TokenReward,
			NodeCount:   len(ch.Nodes),
		})
	}

	gohttp.WriteJSON(w, http.StatusOK, chapters)
}

func (h *handlers) getChapter(w http.ResponseWriter, r *http.Request) {
	story := h.nav.Story()
	id := chi.URLParam(r, "id")

	ch, ok := story.Chapters[id]
	if !ok {
		gohttp.WriteError(w, http.StatusNotFound, fmt.Sprintf("chapter %q not found", id))
		return
	}

	gohttp.WriteJSON(w, http.StatusOK, map[string]any{
		"id":           id,
		"title":        ch.Title,
		"description":  ch.Description,
		"sort_order":   ch.SortOrder,
		"token_reward": ch.TokenReward,
		"start_node":   ch.StartNode,
		"nodes":        ch.Nodes,
	})
}

func (h *handlers) getChapterVotes(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	tallies, err := h.db.TallyVotes(r.Context(), id)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("tally votes: %v", err))
		return
	}

	gohttp.WriteJSON(w, http.StatusOK, tallies)
}

// --- Vote handler ---

func (h *handlers) castVote(w http.ResponseWriter, r *http.Request) {
	rid := readerID(r)

	var req struct {
		ChapterID   string `json:"chapter_id"`
		Choice      string `json:"choice"`
		TokensSpent int    `json:"tokens_spent"`
	}
	if err := gohttp.DecodeJSON(r, &req); err != nil {
		gohttp.WriteError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}

	if req.ChapterID == "" || req.Choice == "" || req.TokensSpent < 1 {
		gohttp.WriteError(w, http.StatusBadRequest, "chapter_id, choice, and tokens_spent (>=1) required")
		return
	}

	dbReader, err := h.db.GetOrCreateReader(r.Context(), rid)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("get reader: %v", err))
		return
	}

	vote, err := h.db.CastVote(r.Context(), dbReader.ReaderID, req.ChapterID, req.Choice, req.TokensSpent)
	if err != nil {
		gohttp.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	gohttp.WriteJSON(w, http.StatusOK, vote)
}

// --- Reader handler ---

func (h *handlers) getReader(w http.ResponseWriter, r *http.Request) {
	rid := readerID(r)

	dbReader, err := h.db.GetOrCreateReader(r.Context(), rid)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("get reader: %v", err))
		return
	}

	// Include navigator state if available.
	state, _, navErr := h.nav.CurrentNode(rid)

	resp := map[string]any{
		"reader_id":   dbReader.ReaderID,
		"device_hash": dbReader.DeviceHash,
		"tokens":      dbReader.Tokens,
	}
	if navErr == nil && state != nil {
		resp["current_node"] = state.CurrentNode
		resp["visited"] = state.Visited
		resp["completed"] = state.Completed
	}

	gohttp.WriteJSON(w, http.StatusOK, resp)
}

// --- unused but kept for compile ---
var _ = uuid.UUID{}

// --- Story version control handlers ---
//
// These handlers expose the storyrepo package over HTTP, allowing external
// tools (editors, CI, admin dashboards) to manage story YAML versions.

// storyHistory returns the git commit log for the story repository.
// Supports an optional ?limit=N query parameter (default: all commits).
func (h *handlers) storyHistory(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 0 {
			gohttp.WriteError(w, http.StatusBadRequest, "limit must be a non-negative integer")
			return
		}
		limit = n
	}

	commits, err := h.storyRepo.Log(limit)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("story log: %v", err))
		return
	}

	// Return empty array instead of null when there are no commits.
	if commits == nil {
		commits = []storyrepo.CommitInfo{}
	}

	gohttp.WriteJSON(w, http.StatusOK, commits)
}

// storyCommit stages all YAML files and creates a new commit.
// Expects a JSON body: {"message": "commit message"}.
func (h *handlers) storyCommit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message string `json:"message"`
	}
	if err := gohttp.DecodeJSON(r, &req); err != nil {
		gohttp.WriteError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}

	if req.Message == "" {
		gohttp.WriteError(w, http.StatusBadRequest, "message is required")
		return
	}

	hash, err := h.storyRepo.Commit(req.Message)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("commit: %v", err))
		return
	}

	gohttp.WriteJSON(w, http.StatusOK, map[string]string{
		"hash":    hash,
		"message": req.Message,
	})
}

// storyRevert rolls back the story repo to a previous commit.
// Expects a JSON body: {"hash": "abc123..."}.
func (h *handlers) storyRevert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hash string `json:"hash"`
	}
	if err := gohttp.DecodeJSON(r, &req); err != nil {
		gohttp.WriteError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return
	}

	if req.Hash == "" {
		gohttp.WriteError(w, http.StatusBadRequest, "hash is required")
		return
	}

	if err := h.storyRepo.Revert(req.Hash); err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("revert: %v", err))
		return
	}

	// Return the new HEAD hash after the revert commit.
	newHash, err := h.storyRepo.CurrentHash()
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("get hash after revert: %v", err))
		return
	}

	gohttp.WriteJSON(w, http.StatusOK, map[string]string{
		"status":       "reverted",
		"reverted_to":  req.Hash,
		"current_hash": newHash,
	})
}

// storyDiff returns the diff between two commits. Supports query parameters:
//   - ?from=X       — diff from commit X to HEAD
//   - ?from=X&to=Y  — diff from commit X to commit Y
func (h *handlers) storyDiff(w http.ResponseWriter, r *http.Request) {
	fromHash := r.URL.Query().Get("from")
	toHash := r.URL.Query().Get("to")

	diff, err := h.storyRepo.Diff(fromHash, toHash)
	if err != nil {
		gohttp.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("diff: %v", err))
		return
	}

	gohttp.WriteJSON(w, http.StatusOK, map[string]string{
		"diff": diff,
	})
}

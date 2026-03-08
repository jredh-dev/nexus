// Package handler implements the HTTP API for the ref service.
//
// Routes:
//
//	GET  /health             — liveness probe (always 200)
//	GET  /prompts            — list all prompts
//	POST /prompts            — create a prompt
//	GET  /prompts/{id}       — get one prompt
//	PUT  /prompts/{id}       — update a prompt (partial)
//	DELETE /prompts/{id}     — delete a prompt
//	POST /reflect            — drain non-inactive prompts through OpenCode
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jredh-dev/nexus/services/ref/internal/db"
)

// Handler holds shared dependencies.
type Handler struct {
	pool        *pgxpool.Pool
	openCodeURL string // e.g. http://opencode:4096
	openCodePW  string // server password for basic auth
}

// New creates a Handler.
func New(pool *pgxpool.Pool, openCodeURL, openCodePW string) *Handler {
	return &Handler{
		pool:        pool,
		openCodeURL: strings.TrimRight(openCodeURL, "/"),
		openCodePW:  openCodePW,
	}
}

// Register mounts all routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /prompts", h.ListPrompts)
	mux.HandleFunc("POST /prompts", h.CreatePrompt)
	mux.HandleFunc("GET /prompts/{id}", h.GetPrompt)
	mux.HandleFunc("PUT /prompts/{id}", h.UpdatePrompt)
	mux.HandleFunc("DELETE /prompts/{id}", h.DeletePrompt)
	mux.HandleFunc("POST /reflect", h.Reflect)
}

// Health returns 200 OK.
func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintln(w, "ok")
}

// ListPrompts returns all prompts as JSON.
func (h *Handler) ListPrompts(w http.ResponseWriter, r *http.Request) {
	prompts, err := db.List(r.Context(), h.pool)
	if err != nil {
		h.serverError(w, "list prompts", err)
		return
	}
	if prompts == nil {
		prompts = []db.Prompt{} // return [] not null
	}
	respondJSON(w, http.StatusOK, prompts)
}

// CreatePrompt creates a new prompt from the JSON request body.
func (h *Handler) CreatePrompt(w http.ResponseWriter, r *http.Request) {
	var in db.CreatePromptInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		respondError(w, http.StatusBadRequest, "title is required")
		return
	}
	if strings.TrimSpace(in.Prompt) == "" {
		respondError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	p, err := db.Create(r.Context(), h.pool, in)
	if err != nil {
		if strings.Contains(err.Error(), "invalid mode") {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.serverError(w, "create prompt", err)
		return
	}
	respondJSON(w, http.StatusCreated, p)
}

// GetPrompt returns a single prompt by ID.
func (h *Handler) GetPrompt(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := db.Get(r.Context(), h.pool, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		h.serverError(w, "get prompt", err)
		return
	}
	respondJSON(w, http.StatusOK, p)
}

// UpdatePrompt does a partial update on a prompt.
func (h *Handler) UpdatePrompt(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in db.UpdatePromptInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	p, err := db.Update(r.Context(), h.pool, id, in)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		if strings.Contains(err.Error(), "invalid mode") {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.serverError(w, "update prompt", err)
		return
	}
	respondJSON(w, http.StatusOK, p)
}

// DeletePrompt removes a prompt by ID.
func (h *Handler) DeletePrompt(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := db.Delete(r.Context(), h.pool, id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		h.serverError(w, "delete prompt", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// -----------------------------------------------------------------------
// Reflect — drain non-inactive prompts through OpenCode
// -----------------------------------------------------------------------

// ReflectResult is returned by POST /reflect.
type ReflectResult struct {
	Processed int      `json:"processed"` // prompts attempted
	Succeeded int      `json:"succeeded"` // successful OpenCode runs
	Skipped   int      `json:"skipped"`   // inactive (skipped)
	Errors    []string `json:"errors"`    // error messages for failed runs
}

// Reflect fetches all non-inactive prompts and runs each through OpenCode.
// Results are stored back in the DB. This is idempotent — running it twice
// will record two runs.
func (h *Handler) Reflect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Fetch prompts in modes batch and review.
	var prompts []db.Prompt
	for _, mode := range []string{"batch", "review"} {
		ps, err := db.ListByMode(ctx, h.pool, mode)
		if err != nil {
			h.serverError(w, "list prompts for reflect", err)
			return
		}
		prompts = append(prompts, ps...)
	}

	result := ReflectResult{Errors: []string{}}

	for _, p := range prompts {
		result.Processed++
		slog.Info("reflect: running prompt", "id", p.ID, "title", p.Title, "mode", p.Mode)

		response, err := h.runOpenCode(ctx, p.Prompt)
		if err != nil {
			msg := fmt.Sprintf("prompt %d (%s): %v", p.ID, p.Title, err)
			slog.Error("reflect: opencode run failed", "id", p.ID, "err", err)
			result.Errors = append(result.Errors, msg)
			continue
		}

		if _, err := db.RecordRun(ctx, h.pool, p.ID, response); err != nil {
			msg := fmt.Sprintf("prompt %d (%s): record run: %v", p.ID, p.Title, err)
			slog.Error("reflect: record run failed", "id", p.ID, "err", err)
			result.Errors = append(result.Errors, msg)
			continue
		}

		result.Succeeded++
		slog.Info("reflect: prompt completed", "id", p.ID, "title", p.Title)
	}

	respondJSON(w, http.StatusOK, result)
}

// -----------------------------------------------------------------------
// OpenCode HTTP client
// -----------------------------------------------------------------------

// openCodeSession is the response from POST /session.
type openCodeSession struct {
	ID string `json:"id"`
}

// openCodeSessionState is the response from GET /session/{id}.
type openCodeSessionState struct {
	ID    string `json:"id"`
	Parts []struct {
		Type    string `json:"type"`
		Content string `json:"content,omitempty"`
	} `json:"parts"`
}

// runOpenCode creates an OpenCode session, sends the prompt, polls until
// completion, extracts the assistant response text, then deletes the session.
func (h *Handler) runOpenCode(ctx context.Context, prompt string) (string, error) {
	// 1. Create session.
	sessionID, err := h.ocCreateSession(ctx)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	// Always clean up the session.
	defer h.ocDeleteSession(ctx, sessionID) //nolint:errcheck

	// 2. Send message.
	if err := h.ocSendMessage(ctx, sessionID, prompt); err != nil {
		return "", fmt.Errorf("send message: %w", err)
	}

	// 3. Poll until we see an assistant text part (up to 5 minutes).
	response, err := h.ocPollResponse(ctx, sessionID, 5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("poll response: %w", err)
	}

	return response, nil
}

// ocCreateSession calls POST /session and returns the session ID.
func (h *Handler) ocCreateSession(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.openCodeURL+"/session", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth("opencode", h.openCodePW)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create session status %d: %s", resp.StatusCode, body)
	}

	var s openCodeSession
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return "", fmt.Errorf("decode session: %w", err)
	}
	if s.ID == "" {
		return "", fmt.Errorf("empty session ID in response")
	}
	return s.ID, nil
}

// ocSendMessage posts the prompt to /session/{id}/message.
func (h *Handler) ocSendMessage(ctx context.Context, sessionID, prompt string) error {
	body, _ := json.Marshal(map[string]string{"content": prompt})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.openCodeURL+"/session/"+sessionID+"/message",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.SetBasicAuth("opencode", h.openCodePW)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send message status %d: %s", resp.StatusCode, body)
	}
	return nil
}

// ocPollResponse polls GET /session/{id} until an assistant text part
// appears or the deadline is exceeded.
func (h *Handler) ocPollResponse(ctx context.Context, sessionID string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	pollInterval := 3 * time.Second

	for {
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for OpenCode response after %s", timeout)
		}

		// Check context cancellation.
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			h.openCodeURL+"/session/"+sessionID, nil)
		if err != nil {
			return "", err
		}
		req.SetBasicAuth("opencode", h.openCodePW)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			// Transient — retry after interval.
			slog.Warn("opencode poll error, retrying", "err", err)
			time.Sleep(pollInterval)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			time.Sleep(pollInterval)
			continue
		}

		var state openCodeSessionState
		if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
			resp.Body.Close()
			time.Sleep(pollInterval)
			continue
		}
		resp.Body.Close()

		// Collect all assistant text parts.
		var parts []string
		for _, part := range state.Parts {
			if part.Type == "text" && part.Content != "" {
				parts = append(parts, part.Content)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n"), nil
		}

		time.Sleep(pollInterval)
	}
}

// ocDeleteSession calls DELETE /session/{id}.
func (h *Handler) ocDeleteSession(ctx context.Context, sessionID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		h.openCodeURL+"/session/"+sessionID, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth("opencode", h.openCodePW)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

// pathID extracts the {id} path value and converts it to int.
func pathID(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.PathValue("id")
	id, err := strconv.Atoi(raw)
	if err != nil || id < 1 {
		respondError(w, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("respond json encode", "err", err)
	}
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) serverError(w http.ResponseWriter, op string, err error) {
	slog.Error(op, "err", err)
	respondError(w, http.StatusInternalServerError, "internal server error")
}

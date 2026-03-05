package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jredh-dev/nexus/services/secrets/internal/store"
	"github.com/jredh-dev/nexus/services/secrets/internal/wall"
)

// Handler holds dependencies for secrets HTTP handlers.
type Handler struct {
	store *store.Store
	wall  *wall.Wall
}

// New creates a new Handler with a rotating wall.
func New(s *store.Store) *Handler {
	return &Handler{
		store: s,
		wall:  wall.New(s),
	}
}

type submitReq struct {
	Value       string `json:"value"`
	SubmittedBy string `json:"submitted_by"`
}

// Submit handles POST /api/secrets
//
//	@Summary      Submit a secret
//	@Description  Submit a value to be checked for equivalence. If no matching
//	              secret exists, it becomes a new secret (count=1). If an
//	              equivalent secret exists, its count increments.
//	@Tags         secrets
//	@Accept       json
//	@Produce      json
//	@Param        body  body      submitReq  true  "Secret submission"
//	@Success      200   {object}  store.SubmitResult
//	@Failure      400   {object}  map[string]string
//	@Router       /api/secrets [post]
func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	var req submitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Value == "" {
		jsonError(w, "value is required", http.StatusBadRequest)
		return
	}
	if req.SubmittedBy == "" {
		req.SubmittedBy = "anonymous"
	}

	result := h.store.Submit(req.Value, req.SubmittedBy)

	log.Printf("submit: value=%q by=%s new=%v count=%d",
		req.Value, req.SubmittedBy, result.WasNew, result.Secret.Count)

	jsonOK(w, http.StatusOK, result)
}

// Get handles GET /api/secrets/{id}
//
//	@Summary      Get a secret by ID
//	@Description  Returns a single secret by its ID.
//	@Tags         secrets
//	@Produce      json
//	@Param        id   path      string  true  "Secret ID"
//	@Success      200  {object}  store.Secret
//	@Failure      404  {object}  map[string]string
//	@Router       /api/secrets/{id} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sec, ok := h.store.Get(id)
	if !ok {
		jsonError(w, "secret not found", http.StatusNotFound)
		return
	}
	jsonOK(w, http.StatusOK, sec)
}

// List handles GET /api/secrets — returns secrets in randomized order.
//
//	@Summary      List all secrets
//	@Description  Returns all secrets in randomized order.
//	@Tags         secrets
//	@Produce      json
//	@Success      200  {array}  store.Secret
//	@Router       /api/secrets [get]
func (h *Handler) List(w http.ResponseWriter, _ *http.Request) {
	secrets := h.store.List()
	if secrets == nil {
		secrets = []*store.Secret{}
	}
	jsonOK(w, http.StatusOK, secrets)
}

// Stats handles GET /api/stats
//
//	@Summary      Get aggregate stats
//	@Description  Returns counts of total secrets, still-secret entries, exposed entries, and lens count.
//	@Tags         secrets
//	@Produce      json
//	@Success      200  {object}  store.Stats
//	@Router       /api/stats [get]
func (h *Handler) Stats(w http.ResponseWriter, _ *http.Request) {
	jsonOK(w, http.StatusOK, h.store.Stats())
}

// Riddle handles GET /api/riddle — the entry point.
//
//	@Summary      Get the riddle
//	@Description  Returns the riddle that explains the secrets game, along with current stats.
//	@Tags         secrets
//	@Produce      json
//	@Success      200  {object}  map[string]interface{}
//	@Router       /api/riddle [get]
func (h *Handler) Riddle(w http.ResponseWriter, _ *http.Request) {
	riddle := map[string]interface{}{
		"riddle": "A secret admitted once remains a secret. " +
			"Admitted again, it's no longer secret — everyone knows. " +
			"But beware: some words are the same word wearing a different face.",
		"rules": []string{
			"Submit a secret. If no one has said it before, it's a secret (count=1).",
			"If something equivalent has been admitted, the count goes up.",
			"Once count > 1, it's no longer a secret.",
			"Equivalence is... flexible. Discovering how is the game.",
		},
		"hint":     "How many ways can you say the same thing?",
		"endpoint": "POST /api/secrets {\"value\": \"...\", \"submitted_by\": \"...\"}",
		"stats":    h.store.Stats(),
	}
	jsonOK(w, http.StatusOK, riddle)
}

// Exposed handles GET /api/exposed — rotating page of no-longer-secret entries.
//
//	@Summary      Get exposed secrets
//	@Description  Returns a rotating plain-text page of secrets that have been admitted more than once.
//	@Tags         secrets
//	@Produce      plain
//	@Success      200  {string}  string  "Plain text wall of exposed secrets"
//	@Header       200  {int}     X-Exposed-Total  "Total number of exposed secrets"
//	@Header       200  {int}     X-Exposed-Page   "Current page index"
//	@Header       200  {int}     X-Exposed-Pages  "Total number of pages"
//	@Router       /api/exposed [get]
func (h *Handler) Exposed(w http.ResponseWriter, _ *http.Request) {
	text, pageIdx, totalPages, totalExposed := h.wall.Page()

	if totalExposed == 0 {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Exposed-Total", "0")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("No exposed secrets yet. Submit one to begin.")) //nolint:errcheck
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Exposed-Total", itoa(totalExposed))
	w.Header().Set("X-Exposed-Page", itoa(pageIdx))
	w.Header().Set("X-Exposed-Pages", itoa(totalPages))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(text)) //nolint:errcheck
}

// Stop shuts down the background wall worker.
func (h *Handler) Stop() {
	h.wall.Stop()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

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

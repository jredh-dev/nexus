package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jredh-dev/nexus/services/go-http/internal/store"
	"github.com/jredh-dev/nexus/services/go-http/internal/wall"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	store *store.Store
	wall  *wall.Wall
}

// New creates a new Handler with a rotating lies wall.
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

type submitResp struct {
	Secret       *store.Secret `json:"secret"`
	WasNew       bool          `json:"was_new"`
	SelfBetrayal bool          `json:"self_betrayal,omitempty"`
	Message      string        `json:"message"`
}

// Submit handles POST /api/secrets
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

	resp := submitResp{
		Secret:       result.Secret,
		WasNew:       result.WasNew,
		SelfBetrayal: result.SelfBetrayal,
	}

	switch {
	case result.SelfBetrayal:
		resp.Message = "This secret betrayed itself."
	case result.Exposed != nil:
		resp.Message = "A secret has been exposed as a lie."
	case result.WasNew:
		resp.Message = "A new truth has been recorded."
	}

	log.Printf("submit: value=%q by=%s new=%v betrayal=%v exposed=%v",
		req.Value, req.SubmittedBy, result.WasNew, result.SelfBetrayal, result.Exposed != nil)

	jsonOK(w, http.StatusOK, resp)
}

// Get handles GET /api/secrets/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sec, ok := h.store.Get(id)
	if !ok {
		jsonError(w, "secret not found", http.StatusNotFound)
		return
	}
	jsonOK(w, http.StatusOK, sec)
}

// List handles GET /api/secrets
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	secrets := h.store.List()
	if secrets == nil {
		secrets = []*store.Secret{}
	}
	jsonOK(w, http.StatusOK, secrets)
}

// Stats handles GET /api/stats
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, http.StatusOK, h.store.Stats())
}

// Riddle handles GET /api/riddle — the entry point for curious humans.
func (h *Handler) Riddle(w http.ResponseWriter, r *http.Request) {
	riddle := map[string]interface{}{
		"riddle": "A secret said once is truth. Said again, it becomes a lie. " +
			"But beware: some words betray themselves the moment they are spoken. " +
			"And some words are the same word wearing a different face.",
		"rules": []string{
			"Submit a secret. If it has never been said, it is truth.",
			"If something equivalent has been said before, the original becomes a lie.",
			"Equivalence is... flexible. Discovering how is the game.",
		},
		"hint":     "How many ways can you say the same thing?",
		"endpoint": "POST /api/secrets {\"value\": \"...\", \"submitted_by\": \"...\"}",
		"stats":    h.store.Stats(),
	}
	jsonOK(w, http.StatusOK, riddle)
}

// Lies handles GET /api/lies — returns a rotating page of exposed lies as raw text.
// Each request gets a different page (round-robin). The response includes metadata
// headers so clients know their position in the rotation.
func (h *Handler) Lies(w http.ResponseWriter, r *http.Request) {
	text, pageIdx, totalPages, totalLies := h.wall.Page()

	if totalLies == 0 {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Lies-Total", "0")
		w.Header().Set("X-Lies-Page", "0")
		w.Header().Set("X-Lies-Pages", "0")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("No lies yet. Submit a secret to begin."))
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Lies-Total", itoa(totalLies))
	w.Header().Set("X-Lies-Page", itoa(pageIdx))
	w.Header().Set("X-Lies-Pages", itoa(totalPages))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(text))
}

// Stop shuts down the background lies wall worker.
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

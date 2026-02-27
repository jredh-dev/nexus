package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jredh-dev/nexus/services/secrets/internal/store"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	store *store.Store
}

// New creates a new Handler.
func New(s *store.Store) *Handler {
	return &Handler{store: s}
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

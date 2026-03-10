// Package ctl implements the HTTP control server for the watcher service.
//
// Endpoints:
//
//	POST /trigger    — run a full diff of all visible files against HEAD and send to OpenCode
//	POST /pause      — pause the watcher (fsnotify events are still drained but batches are held)
//	POST /resume     — resume the watcher
//	POST /interrupt  — immediately flush pending batch without waiting for the quiet period
//	GET  /status     — return JSON status (paused, pending file count, last batch time)
package ctl

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Handler is the HTTP control server. It holds shared state flags that the
// main watcher loop consults and modifies.
type Handler struct {
	mu            sync.RWMutex
	paused        bool
	lastBatchTime time.Time
	pendingCount  int // updated by the watcher loop via SetPending

	// triggerCh receives a signal to run a full manual trigger.
	// Buffered(1) so POST /trigger returns immediately.
	triggerCh chan struct{}

	// interruptCh signals the batchLoop to flush pending files now.
	interruptCh chan struct{}
}

// New creates a Handler with initialized channels.
func New() *Handler {
	return &Handler{
		triggerCh:   make(chan struct{}, 1),
		interruptCh: make(chan struct{}, 1),
	}
}

// TriggerCh returns the read-only trigger channel for the main loop.
func (h *Handler) TriggerCh() <-chan struct{} { return h.triggerCh }

// InterruptCh returns the read-only interrupt channel for the batch loop.
func (h *Handler) InterruptCh() <-chan struct{} { return h.interruptCh }

// IsPaused returns true if the watcher is currently paused.
func (h *Handler) IsPaused() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.paused
}

// SetPending updates the pending file count (called by the watcher loop).
func (h *Handler) SetPending(n int) {
	h.mu.Lock()
	h.pendingCount = n
	h.mu.Unlock()
}

// SetLastBatch records the time of the most recent batch dispatch.
func (h *Handler) SetLastBatch(t time.Time) {
	h.mu.Lock()
	h.lastBatchTime = t
	h.mu.Unlock()
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/trigger":
		h.handleTrigger(w, r)
	case "/pause":
		h.handlePause(w, r)
	case "/resume":
		h.handleResume(w, r)
	case "/interrupt":
		h.handleInterrupt(w, r)
	case "/status":
		h.handleStatus(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	select {
	case h.triggerCh <- struct{}{}:
		slog.Info("ctl: trigger requested")
		jsonOK(w, "trigger queued")
	default:
		// Already a trigger pending — idempotent.
		jsonOK(w, "trigger already pending")
	}
}

func (h *Handler) handlePause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	h.mu.Lock()
	h.paused = true
	h.mu.Unlock()
	slog.Info("ctl: watcher paused")
	jsonOK(w, "paused")
}

func (h *Handler) handleResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	h.mu.Lock()
	h.paused = false
	h.mu.Unlock()
	slog.Info("ctl: watcher resumed")
	jsonOK(w, "resumed")
}

func (h *Handler) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	select {
	case h.interruptCh <- struct{}{}:
		slog.Info("ctl: interrupt requested — flushing pending batch")
		jsonOK(w, "interrupt queued")
	default:
		jsonOK(w, "interrupt already pending")
	}
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	h.mu.RLock()
	paused := h.paused
	pending := h.pendingCount
	last := h.lastBatchTime
	h.mu.RUnlock()

	var lastStr string
	if !last.IsZero() {
		lastStr = last.UTC().Format(time.RFC3339)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"paused":          paused,
		"pending_files":   pending,
		"last_batch_time": lastStr,
	})
}

func jsonOK(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": msg})
}

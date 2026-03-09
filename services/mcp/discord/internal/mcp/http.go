package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
)

// ServeHTTP starts the MCP server on the given address.
// POST /mcp — JSON-RPC requests (Streamable HTTP transport)
// GET  /mcp — SSE stream for server-initiated messages
// GET  /health — health check
func ServeHTTP(s *Server, addr string) error {
	logger := log.New(log.Default().Writer(), "", log.LstdFlags)
	logger.Printf("[mcp] starting HTTP transport on %s", addr)

	h := &httpHandler{server: s, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /mcp", h.handlePost)
	mux.HandleFunc("GET /mcp", h.handleSSE)
	mux.HandleFunc("DELETE /mcp", h.handleSessionEnd)
	mux.HandleFunc("GET /health", h.handleHealth)

	return http.ListenAndServe(addr, mux)
}

type httpHandler struct {
	server    *Server
	logger    *log.Logger
	sessionID atomic.Int64
	mu        sync.RWMutex
	sessions  map[int64]*sseSession
}

type sseSession struct {
	id     int64
	notify chan []byte
	done   chan struct{}
}

// handlePost processes JSON-RPC requests (single or newline-delimited batch).
func (h *httpHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	var responses []json.RawMessage
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if resp := h.server.HandleRequest(line); resp != nil {
			responses = append(responses, resp)
		}
	}

	switch len(responses) {
	case 0:
		w.WriteHeader(http.StatusAccepted)
	case 1:
		w.Header().Set("Content-Type", "application/json")
		w.Write(responses[0])
	default:
		w.Header().Set("Content-Type", "application/json")
		for i, resp := range responses {
			if i > 0 {
				w.Write([]byte("\n"))
			}
			w.Write(resp)
		}
	}
}

// handleSSE opens a server-sent events stream (keepalive / notification channel).
func (h *httpHandler) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sess := &sseSession{
		id:     h.sessionID.Add(1),
		notify: make(chan []byte, 16),
		done:   make(chan struct{}),
	}

	h.mu.Lock()
	if h.sessions == nil {
		h.sessions = make(map[int64]*sseSession)
	}
	h.sessions[sess.id] = sess
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.sessions, sess.id)
		h.mu.Unlock()
		close(sess.done)
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Endpoint event tells the client where to POST.
	fmt.Fprintf(w, "event: endpoint\ndata: /mcp\n\n")
	flusher.Flush()

	for {
		select {
		case msg := <-sess.notify:
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (h *httpHandler) handleSessionEnd(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (h *httpHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","server":"%s","version":"%s"}`, ServerName, Version)
}

// Package server wires HTTP routes for the realtime service onto the go-http
// scaffold.  Routes:
//
//	GET  /health   — 200 OK (provided by go-http scaffold)
//	POST /publish  — encrypt and publish a caller-supplied event to Kafka
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"

	kafkatypes "github.com/jredh-dev/nexus/internal/kafka"
	gohttp "github.com/jredh-dev/nexus/services/go-http"
)

// Publisher is the interface required by the server to publish events.
// The producer package satisfies this interface.
type Publisher interface {
	Publish(ctx context.Context, traceID string, e kafkatypes.Event) error
}

// Server holds the go-http scaffold and the Kafka producer reference.
type Server struct {
	srv      *gohttp.Server
	producer Publisher
	// ready is false when the service is misconfigured (e.g. missing key).
	ready bool
}

// New builds a Server and registers routes.
// If producer is nil, /publish will return 503.
func New(producer Publisher, ready bool) *Server {
	s := &Server{
		srv:      gohttp.New(),
		producer: producer,
		ready:    ready,
	}
	s.srv.Router.Post("/publish", s.handlePublish)
	return s
}

// Router returns the underlying chi router so callers can mount additional
// routes if needed.
func (s *Server) Router() interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
} {
	return s.srv.Router
}

// OnStop registers a shutdown callback with the go-http scaffold.
func (s *Server) OnStop(fn func()) {
	s.srv.OnStop(fn)
}

// ListenAndServe starts the HTTP server on addr and blocks until shutdown.
func (s *Server) ListenAndServe(addr string) error {
	return s.srv.ListenAndServe(addr)
}

// publishRequest is the JSON body for POST /publish.
type publishRequest struct {
	Level   string            `json:"level"`
	Message string            `json:"msg"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// publishResponse is the JSON body returned on success.
type publishResponse struct {
	TraceID string `json:"trace_id"`
}

// handlePublish accepts a JSON event body, validates it, publishes it to
// Kafka, and returns the generated trace ID.
func (s *Server) handlePublish(w http.ResponseWriter, r *http.Request) {
	// If the service is misconfigured (e.g. REALTIME_KEY missing), reject early.
	if !s.ready {
		gohttp.WriteError(w, http.StatusServiceUnavailable, "service not ready: REALTIME_KEY missing or invalid")
		return
	}

	var req publishRequest
	if err := gohttp.DecodeJSON(r, &req); err != nil {
		gohttp.WriteError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	// Default to INFO if level is empty or unknown.
	level := req.Level
	switch level {
	case kafkatypes.LevelInfo, kafkatypes.LevelWarn, kafkatypes.LevelError:
		// valid
	default:
		level = kafkatypes.LevelInfo
	}

	event := kafkatypes.Event{
		Level:   level,
		Message: req.Message,
		Fields:  req.Fields,
	}

	traceID := uuid.New().String()
	if err := s.producer.Publish(r.Context(), traceID, event); err != nil {
		log.Printf("[server] publish error: %v", err)
		gohttp.WriteError(w, http.StatusInternalServerError, "publish failed")
		return
	}

	// Return the trace ID so callers can correlate downstream.
	if err := json.NewEncoder(w).Encode(publishResponse{TraceID: traceID}); err != nil {
		log.Printf("[server] encode response: %v", err)
	}
}

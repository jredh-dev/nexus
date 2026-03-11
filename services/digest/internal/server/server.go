// Package server wires HTTP routes for the digest service onto the go-http
// scaffold.  Routes:
//
//	GET /health  — 200 OK (provided by go-http scaffold)
//	GET /tiles   — current TileSnapshot as JSON
package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jredh-dev/nexus/services/digest/internal/tiles"
	gohttp "github.com/jredh-dev/nexus/services/go-http"
)

// TileProvider is satisfied by consumer.Consumer.
type TileProvider interface {
	Tiles() []tiles.TileValue
}

// Server wraps the go-http scaffold and exposes the tile state.
type Server struct {
	srv      *gohttp.Server
	provider TileProvider
}

// New creates a Server, registering the /tiles route.
func New(provider TileProvider) *Server {
	s := &Server{
		srv:      gohttp.New(),
		provider: provider,
	}
	s.srv.Router.Get("/tiles", s.handleTiles)
	return s
}

// OnStop registers a shutdown callback with the go-http scaffold.
func (s *Server) OnStop(fn func()) {
	s.srv.OnStop(fn)
}

// ListenAndServe starts the HTTP server on addr and blocks until shutdown.
func (s *Server) ListenAndServe(addr string) error {
	return s.srv.ListenAndServe(addr)
}

// handleTiles returns the current TileSnapshot as JSON.
func (s *Server) handleTiles(w http.ResponseWriter, r *http.Request) {
	tvs := s.provider.Tiles()
	snap := tiles.TileSnapshot{
		Tiles:      tvs,
		ComputedAt: time.Now().UTC(),
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snap); err != nil {
		// Headers already written; just log.
		_ = err
	}
}

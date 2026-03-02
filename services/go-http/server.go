// Package gohttp provides a reusable HTTP server scaffold for nexus services.
//
// It sets up a chi router with standard middleware (request ID, real IP,
// logging, recovery, timeout, CORS), a /health endpoint, and graceful
// shutdown. Services import this package and register their own routes.
package gohttp

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server is a reusable HTTP server with standard middleware and graceful shutdown.
type Server struct {
	Router *chi.Mux
	srv    *http.Server
	onStop []func()
}

// New creates a Server with standard middleware already applied.
// The returned Router is ready for route registration.
func New() *Server {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cors)

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK")) //nolint:errcheck
	})

	return &Server{Router: r}
}

// OnStop registers a function to call during graceful shutdown.
func (s *Server) OnStop(fn func()) {
	s.onStop = append(s.onStop, fn)
}

// ListenAndServe starts the server on addr and blocks until shutdown.
// It handles SIGINT/SIGTERM for graceful shutdown.
func (s *Server) ListenAndServe(addr string) error {
	s.srv = &http.Server{
		Addr:         addr,
		Handler:      s.Router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		log.Println("Shutting down server...")
		for _, fn := range s.onStop {
			fn()
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Printf("server starting on %s", addr)
	if err := s.srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	log.Println("Server stopped")
	return nil
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

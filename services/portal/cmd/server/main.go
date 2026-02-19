package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jredh-dev/nexus/services/portal/config"
	"github.com/jredh-dev/nexus/services/portal/internal/auth"
	"github.com/jredh-dev/nexus/services/portal/internal/database"
	"github.com/jredh-dev/nexus/services/portal/internal/web/handlers"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("portal-server %s\n", version)
		fmt.Printf("Commit: %s\n", commit)
		fmt.Printf("Built: %s\n", buildDate)
		os.Exit(0)
	}

	cfg := config.Load()

	if cfg.Session.Secret == "" {
		log.Println("WARNING: SESSION_SECRET is empty — using insecure default (set SESSION_SECRET in production)")
		cfg.Session.Secret = "insecure-dev-secret-change-me"
	}

	// Initialize SQLite database.
	db, err := database.New(cfg.DB.Path)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize auth service.
	authService := auth.New(db, cfg)

	// Seed admin user in development if DB is empty.
	if cfg.Server.Env == "development" {
		seedDevUsers(authService)
	}

	// Initialize router.
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Health check.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Initialize handlers.
	h := handlers.New(db, cfg, authService)

	// Public routes.
	r.Get("/", h.Home)
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.Login)
	r.Get("/logout", h.Logout)

	// Protected routes.
	r.Group(func(r chi.Router) {
		r.Use(handlers.AuthMiddleware(authService))
		r.Get("/dashboard", h.Dashboard)
	})

	// Start server.
	addr := fmt.Sprintf(":%s", cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		log.Println("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Server shutdown error: %v", err)
		}
	}()

	log.Printf("Portal server starting on %s (env: %s)", addr, cfg.Server.Env)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}

// seedDevUsers creates default users if the database is empty.
func seedDevUsers(authService *auth.Service) {
	users := []struct {
		email    string
		password string
		name     string
	}{
		{"admin@localhost", "admin", "Admin"},
		{"demo@localhost", "demo", "Demo User"},
	}

	for _, u := range users {
		// If login succeeds, user already exists.
		_, err := authService.Login(u.email, u.password, "seed", "seed")
		if err == nil {
			continue
		}

		created, err := authService.CreateUser(u.email, u.password, u.name)
		if err != nil {
			log.Printf("Seed user %s skipped (may already exist): %v", u.email, err)
			continue
		}
		log.Printf("Seeded user: %s (%s)", created.Email, created.ID)
	}
}

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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

	// Seed demo user in all environments so visitors can log in.
	seedDemoUser(authService)

	// Seed admin user in development only.
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

	// Static file serving.
	staticDir := filepath.Join("services", "portal", "static")
	fileServer := http.FileServer(http.Dir(staticDir))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Public routes.
	r.Get("/", h.Home)
	r.Get("/about", h.About)
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.Login)
	r.Get("/signup", h.SignupPage)
	r.Post("/signup", h.Signup)
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

// seedDemoUser ensures the demo account exists in all environments.
func seedDemoUser(authService *auth.Service) {
	_, err := authService.Login("demo@demo.com", "demo", "seed", "seed")
	if err == nil {
		return // already exists
	}

	created, err := authService.CreateUser("demo@demo.com", "demo", "Demo User")
	if err != nil {
		log.Printf("Demo user skipped (may already exist): %v", err)
		return
	}
	log.Printf("Seeded demo user: %s (%s)", created.Email, created.ID)
}

// seedDevUsers creates admin users for development only.
func seedDevUsers(authService *auth.Service) {
	_, err := authService.Login("admin@admin.com", "admin", "seed", "seed")
	if err == nil {
		return
	}

	created, err := authService.CreateUser("admin@admin.com", "admin", "Admin")
	if err != nil {
		log.Printf("Admin user skipped (may already exist): %v", err)
		return
	}
	log.Printf("Seeded admin user: %s (%s)", created.Email, created.ID)
}

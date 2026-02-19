package main

import (
	"context"
	cryptoRand "crypto/rand"
	"encoding/hex"
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

	// Initialize giveaway database.
	giveawayDB, err := database.NewGiveaway(cfg.Giveaway.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize giveaway database: %v", err)
	}
	defer giveawayDB.Close()

	// Initialize auth service.
	authService := auth.New(db, cfg)

	// Seed demo user in all environments so visitors can log in.
	seedDemoUser(authService)

	// Seed admin user (dev@jredh.com) in all environments.
	seedAdminUser(db, authService)

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
	h := handlers.New(db, giveawayDB, cfg, authService)

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
	r.Get("/auth/magic", h.MagicLogin)

	// Public giveaway routes (no auth required).
	r.Get("/giveaway", h.GiveawayList)
	r.Get("/giveaway/{id}", h.GiveawayItem)
	r.Post("/giveaway/{id}/claim", h.GiveawayClaimSubmit)

	// Public JSON API.
	r.Route("/api", func(r chi.Router) {
		r.Get("/items", h.APIListItems)
		r.Get("/fee", h.APICalculateFee)
		r.Get("/actions", h.SearchActions)
		r.Post("/claims", h.APICreateClaim)
	})

	// Protected routes (login required).
	r.Group(func(r chi.Router) {
		r.Use(handlers.AuthMiddleware(authService))
		r.Get("/dashboard", h.Dashboard)
	})

	// Admin routes (login + admin role required).
	r.Group(func(r chi.Router) {
		r.Use(handlers.AuthMiddleware(authService))
		r.Use(handlers.AdminMiddleware)

		// Admin giveaway management.
		r.Get("/admin/giveaway", h.AdminGiveawayList)
		r.Get("/admin/giveaway/new", h.AdminGiveawayNew)
		r.Get("/admin/giveaway/{id}/edit", h.AdminGiveawayEdit)
		r.Post("/admin/giveaway/save", h.AdminGiveawaySave)
		r.Post("/admin/giveaway/{id}/delete", h.AdminGiveawayDelete)
		r.Post("/admin/giveaway/claims/{id}", h.AdminClaimUpdate)

		// Admin utilities.
		r.Post("/admin/magic-link", h.AdminGenerateMagicLink)
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

// seedAdminUser ensures dev@jredh.com exists as an admin in all environments.
// Uses a random password since the primary login method is magic links.
func seedAdminUser(db *database.DB, authService *auth.Service) {
	const adminEmail = "dev@jredh.com"

	// Check if the user already exists.
	existing, err := authService.Login(adminEmail, "not-the-real-password", "seed", "seed")
	if existing != "" {
		return // somehow logged in, user exists — shouldn't happen with random pw
	}
	_ = err // expected to fail

	// Try to look up by email directly (login will fail with wrong password).
	user, lookupErr := db.GetUserByEmail(adminEmail)
	if lookupErr != nil {
		log.Printf("Error looking up admin user: %v", lookupErr)
		return
	}

	if user != nil {
		// User exists, ensure admin role.
		if err := db.UpdateUserRole(user.ID, "admin"); err != nil {
			log.Printf("Failed to set admin role for %s: %v", adminEmail, err)
		} else {
			log.Printf("Admin role ensured for existing user: %s", adminEmail)
		}
		return
	}

	// Create the admin user with a random password (magic links are primary auth).
	created, err := authService.CreateUser(adminEmail, randomPassword(), "Jared Hooper")
	if err != nil {
		log.Printf("Admin user skipped (may already exist): %v", err)
		return
	}

	if err := db.UpdateUserRole(created.ID, "admin"); err != nil {
		log.Printf("Failed to set admin role for %s: %v", adminEmail, err)
		return
	}
	log.Printf("Seeded admin user: %s (%s) with role=admin", created.Email, created.ID)
}

// randomPassword generates a 32-byte hex-encoded random password.
func randomPassword() string {
	b := make([]byte, 32)
	if _, err := cryptoRand.Read(b); err != nil {
		// Fallback — shouldn't happen.
		return "fallback-password-change-me"
	}
	return hex.EncodeToString(b)
}

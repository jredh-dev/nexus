package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/jredh-dev/nexus/services/portal/config"
	"github.com/jredh-dev/nexus/services/portal/internal/auth"
	"github.com/jredh-dev/nexus/services/portal/internal/database"
	"github.com/jredh-dev/nexus/services/portal/internal/token"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	db        *database.DB
	cfg       *config.Config
	auth      *auth.Service
	token     *token.Service
	templates map[string]*template.Template
}

// New creates a new handler
func New(db *database.DB, cfg *config.Config, tokenService *token.Service) *Handler {
	// Load templates
	templates := make(map[string]*template.Template)
	basePath := filepath.Join("services", "portal", "internal", "web", "templates", "base.html")

	pageTemplates := []string{
		"welcome.html", // Renamed from home.html
		"login.html",   // Standalone template
	}

	for _, page := range pageTemplates {
		pagePath := filepath.Join("services", "portal", "internal", "web", "templates", page)

		// Login page is standalone, others use base.html
		if page == "login.html" {
			templates[page] = template.Must(template.New(page).ParseFiles(pagePath))
		} else {
			templates[page] = template.Must(template.New(page).ParseFiles(basePath, pagePath))
		}
	}

	// Initialize auth service
	authService := auth.New(db, cfg)

	return &Handler{
		db:        db,
		cfg:       cfg,
		auth:      authService,
		token:     tokenService,
		templates: templates,
	}
}

// AuthService returns the auth service instance
func (h *Handler) AuthService() *auth.Service {
	return h.auth
}

// Home renders the public home page (redirects to welcome if logged in)
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	// Check if user has session
	cookie, err := r.Cookie("session")
	if err == nil && cookie.Value != "" {
		// User is logged in, redirect to welcome
		http.Redirect(w, r, "/welcome", http.StatusSeeOther)
		return
	}

	// Show login page
	h.renderTemplate(w, "login.html", nil)
}

// Welcome renders the authenticated welcome page
func (h *Handler) Welcome(w http.ResponseWriter, r *http.Request) {
	// Get user info from context (set by auth middleware)
	userEmail := r.Context().Value("user_email")
	userName := r.Context().Value("user_name")

	h.renderTemplate(w, "welcome.html", map[string]interface{}{
		"Title": "Welcome",
		"Email": userEmail,
		"Name":  userName,
	})
}

// LoginPage renders the login page
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	h.renderTemplate(w, "login.html", nil)
}

// Login handles login form submission
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse form data
	if err := r.ParseForm(); err != nil {
		log.Printf("Error parsing login form: %v", err)
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")

	if email == "" || password == "" {
		http.Error(w, "Email and password are required", http.StatusBadRequest)
		return
	}

	// Attempt login with Firebase
	customToken, err := h.auth.Login(ctx, email, password, false)
	if err != nil {
		log.Printf("Login failed for %s: %v", email, err)
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	// Create session cookie from custom token
	expiresIn := time.Hour * 24 * 7 // 7 days
	sessionCookie, err := h.db.Auth.SessionCookie(ctx, customToken, expiresIn)
	if err != nil {
		log.Printf("Error creating session cookie: %v", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionCookie,
		Path:     "/",
		MaxAge:   int(expiresIn.Seconds()),
		HttpOnly: true,
		Secure:   h.cfg.Server.Env == "production",
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to welcome page
	http.Redirect(w, r, "/welcome", http.StatusSeeOther)
}

// Logout handles user logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	// Clear session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1, // Delete cookie
		HttpOnly: true,
		Secure:   h.cfg.Server.Env == "production",
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to home page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// GetToken generates a JWT token for the authenticated user
// This endpoint is used by the frontend after login to get a token for microservice calls
func (h *Handler) GetToken(w http.ResponseWriter, r *http.Request) {
	// Get user info from context (set by auth middleware)
	userID, ok := r.Context().Value("user_id").(string)
	if !ok || userID == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userEmail, _ := r.Context().Value("user_email").(string)

	// Get user roles from Firestore
	// TODO: Implement role retrieval from database
	roles := []string{"user"} // Default role

	// Generate JWT token (valid for 1 hour)
	tokenString, err := h.token.GenerateToken(userID, userEmail, roles, time.Hour)
	if err != nil {
		log.Printf("Error generating token: %v", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	// Return token as JSON
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      tokenString,
		"expires_in": 3600, // 1 hour in seconds
		"token_type": "Bearer",
	}); err != nil {
		log.Printf("Error encoding token response: %v", err)
	}
}

// ValidateToken validates a JWT token (used by microservices)
func (h *Handler) ValidateToken(w http.ResponseWriter, r *http.Request) {
	// Get token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		if err := json.NewEncoder(w).Encode(token.ValidationResponse{
			Valid: false,
			Error: "Missing Authorization header",
		}); err != nil {
			log.Printf("Error encoding validation response: %v", err)
		}
		return
	}

	// Extract token (format: "Bearer <token>")
	var tokenString string
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		tokenString = authHeader[7:]
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		if err := json.NewEncoder(w).Encode(token.ValidationResponse{
			Valid: false,
			Error: "Invalid Authorization header format",
		}); err != nil {
			log.Printf("Error encoding validation response: %v", err)
		}
		return
	}

	// Validate token
	claims, err := h.token.ValidateToken(tokenString)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		if err := json.NewEncoder(w).Encode(token.ValidationResponse{
			Valid: false,
			Error: fmt.Sprintf("Invalid token: %v", err),
		}); err != nil {
			log.Printf("Error encoding validation response: %v", err)
		}
		return
	}

	// Return validation success
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(token.ValidationResponse{
		Valid:  true,
		UserID: claims.UserID,
		Email:  claims.Email,
		Roles:  claims.Roles,
	}); err != nil {
		log.Printf("Error encoding validation response: %v", err)
	}
}

// GetUserInfo returns user information for the authenticated user
// This is a demo endpoint that microservices can call to test authentication
func (h *Handler) GetUserInfo(w http.ResponseWriter, r *http.Request) {
	// Get user info from context (set by auth middleware)
	userID, _ := r.Context().Value("user_id").(string)
	userEmail, _ := r.Context().Value("user_email").(string)
	userName, _ := r.Context().Value("user_name").(string)

	// Return user info as JSON
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id": userID,
		"email":   userEmail,
		"name":    userName,
	}); err != nil {
		log.Printf("Error encoding user info response: %v", err)
	}
}

// Helper methods

func (h *Handler) renderTemplate(w http.ResponseWriter, name string, data interface{}) {
	tmpl, ok := h.templates[name]
	if !ok {
		http.Error(w, fmt.Sprintf("template %s not found", name), http.StatusInternalServerError)
		return
	}

	// Execute the page template
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

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
	templates := make(map[string]*template.Template)
	basePath := filepath.Join("services", "portal", "internal", "web", "templates", "base.html")

	// All pages use base.html layout
	for _, page := range []string{"home.html", "login.html"} {
		pagePath := filepath.Join("services", "portal", "internal", "web", "templates", page)
		templates[page] = template.Must(
			template.New(page).ParseFiles(basePath, pagePath),
		)
	}

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

// Home renders the public landing page
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	h.renderTemplate(w, "home.html", map[string]interface{}{
		"Year": time.Now().Year(),
	})
}

// LoginPage renders the login page
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	h.renderTemplate(w, "login.html", map[string]interface{}{
		"Title": "Login",
		"Year":  time.Now().Year(),
	})
}

// Login handles login form submission
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		log.Printf("Error parsing login form: %v", err)
		h.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Login",
			"Year":  time.Now().Year(),
			"Error": "Invalid form data.",
		})
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")

	if email == "" || password == "" {
		h.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Login",
			"Year":  time.Now().Year(),
			"Error": "Email and password are required.",
		})
		return
	}

	customToken, err := h.auth.Login(ctx, email, password, false)
	if err != nil {
		log.Printf("Login failed for %s: %v", email, err)
		h.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Login",
			"Year":  time.Now().Year(),
			"Error": "Invalid email or password.",
		})
		return
	}

	expiresIn := time.Hour * 24 * 7
	sessionCookie, err := h.db.Auth.SessionCookie(ctx, customToken, expiresIn)
	if err != nil {
		log.Printf("Error creating session cookie: %v", err)
		h.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Login",
			"Year":  time.Now().Year(),
			"Error": "Failed to create session. Please try again.",
		})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionCookie,
		Path:     "/",
		MaxAge:   int(expiresIn.Seconds()),
		HttpOnly: true,
		Secure:   h.cfg.Server.Env == "production",
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to home after login
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Logout handles user logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cfg.Server.Env == "production",
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ValidateToken validates a JWT token (used by microservices)
func (h *Handler) ValidateToken(w http.ResponseWriter, r *http.Request) {
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

// Helper methods

func (h *Handler) renderTemplate(w http.ResponseWriter, name string, data interface{}) {
	tmpl, ok := h.templates[name]
	if !ok {
		http.Error(w, fmt.Sprintf("template %s not found", name), http.StatusInternalServerError)
		return
	}

	// All templates use the "base" block
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("Error rendering template %s: %v", name, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

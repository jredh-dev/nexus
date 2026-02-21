package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/jredh-dev/nexus/services/portal/config"
	"github.com/jredh-dev/nexus/services/portal/internal/actions"
	"github.com/jredh-dev/nexus/services/portal/internal/auth"
	"github.com/jredh-dev/nexus/services/portal/internal/database"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	db      *database.DB
	cfg     *config.Config
	auth    *auth.Service
	actions *actions.Registry
}

// New creates a new handler.
func New(db *database.DB, cfg *config.Config, authService *auth.Service, registry *actions.Registry) *Handler {
	return &Handler{
		db:      db,
		cfg:     cfg,
		auth:    authService,
		actions: registry,
	}
}

// AuthService returns the auth service instance.
func (h *Handler) AuthService() *auth.Service {
	return h.auth
}

// Login handles login form submission.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		log.Printf("Error parsing login form: %v", err)
		h.jsonError(w, "Invalid form data.", http.StatusBadRequest)
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")

	if email == "" || password == "" {
		h.redirectWithError(w, r, "/login", "Email and password are required.")
		return
	}

	sessionID, err := h.auth.Login(email, password, r.RemoteAddr, r.UserAgent())
	if err != nil {
		log.Printf("Login failed for %s: %v", email, err)
		h.redirectWithError(w, r, "/login", "Invalid email or password.")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   h.cfg.Session.MaxAge,
		HttpOnly: true,
		Secure:   h.cfg.Server.Env == "production",
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// Logout clears the session cookie and deletes the server-side session.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		_ = h.auth.Logout(cookie.Value)
	}

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

// Signup handles signup form submission.
func (h *Handler) Signup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		log.Printf("Error parsing signup form: %v", err)
		h.jsonError(w, "Invalid form data.", http.StatusBadRequest)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(r.FormValue("email"))
	phone := strings.TrimSpace(r.FormValue("phone"))
	password := r.FormValue("password")
	name := strings.TrimSpace(r.FormValue("name"))

	if username == "" || email == "" || phone == "" || password == "" {
		h.redirectWithError(w, r, "/signup", "Username, email, phone number, and password are required.")
		return
	}

	_, err := h.auth.Signup(username, email, phone, password, name)
	if err != nil {
		log.Printf("Signup failed for %s: %v", email, err)

		var msg string
		switch {
		case errors.Is(err, auth.ErrUsernameTaken):
			msg = "This username is already taken."
		case errors.Is(err, auth.ErrEmailTaken):
			msg = "An account with this email already exists."
		case errors.Is(err, auth.ErrPhoneTaken):
			msg = "An account with this phone number already exists."
		default:
			msg = "Something went wrong. Please try again."
		}
		h.redirectWithError(w, r, "/signup", msg)
		return
	}

	// Auto-login after signup.
	sessionID, err := h.auth.Login(email, password, r.RemoteAddr, r.UserAgent())
	if err != nil {
		log.Printf("Auto-login after signup failed for %s: %v", email, err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   h.cfg.Session.MaxAge,
		HttpOnly: true,
		Secure:   h.cfg.Server.Env == "production",
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// MagicLogin handles GET /auth/magic?token=X — validates a magic login token,
// creates a session, and redirects to the dashboard.
func (h *Handler) MagicLogin(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Missing token", http.StatusBadRequest)
		return
	}

	sessionID, err := h.auth.ValidateMagicToken(token, r.RemoteAddr, r.UserAgent())
	if err != nil {
		log.Printf("Magic login failed: %v", err)
		if errors.Is(err, auth.ErrInvalidMagicToken) {
			http.Error(w, "Invalid or expired magic login link.", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   h.cfg.Session.MaxAge,
		HttpOnly: true,
		Secure:   h.cfg.Server.Env == "production",
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// AdminGenerateMagicLink handles POST /admin/magic-link — generates a magic
// login token for a given email and returns it as JSON. Requires admin role.
func (h *Handler) AdminGenerateMagicLink(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		http.Error(w, "Email is required", http.StatusBadRequest)
		return
	}

	token, err := h.auth.CreateMagicToken(email)
	if err != nil {
		log.Printf("Failed to generate magic link for %s: %v", email, err)
		if errors.Is(err, auth.ErrUserNotFound) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Build the magic link URL.
	scheme := "https"
	if h.cfg.Server.Env != "production" {
		scheme = "http"
	}
	link := fmt.Sprintf("%s://%s/auth/magic?token=%s", scheme, r.Host, token)

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"link":%q}`, link)
}

// SearchActions returns actions matching the query parameter "q".
// Results are filtered by auth state — admin actions only for admins,
// login/signup hidden when logged in, logout/dashboard hidden when logged out.
func (h *Handler) SearchActions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	// Determine auth context from session cookie (best-effort, no redirect).
	ctx := actions.SearchContext{}
	if cookie, err := r.Cookie("session"); err == nil && cookie.Value != "" {
		if user, _, err := h.auth.ValidateSession(cookie.Value); err == nil && user != nil {
			ctx.LoggedIn = true
			ctx.IsAdmin = user.IsAdmin()
		}
	}

	results := h.actions.Search(query, ctx)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(results); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// --- helpers ---

func (h *Handler) isLoggedIn(r *http.Request) bool {
	cookie, err := r.Cookie("session")
	if err != nil || cookie.Value == "" {
		return false
	}
	user, _, err := h.auth.ValidateSession(cookie.Value)
	return err == nil && user != nil
}

// jsonError writes a JSON error response.
func (h *Handler) jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}

// redirectWithError redirects to the given path with an error query param.
func (h *Handler) redirectWithError(w http.ResponseWriter, r *http.Request, path, msg string) {
	target := path + "?error=" + strings.ReplaceAll(msg, " ", "+")
	http.Redirect(w, r, target, http.StatusSeeOther)
}

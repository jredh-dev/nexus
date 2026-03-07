package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jredh-dev/nexus/internal/authmw"
	"github.com/jredh-dev/nexus/services/portal/config"
	"github.com/jredh-dev/nexus/services/portal/internal/actions"
	"github.com/jredh-dev/nexus/services/portal/internal/auth"
	"github.com/jredh-dev/nexus/services/portal/internal/database"
	"github.com/jredh-dev/nexus/services/portal/pkg/models"
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
//
//	@Summary      Login via form
//	@Description  Authenticates a user with email and password. Sets a session cookie and redirects to /dashboard.
//	@Tags         auth
//	@Accept       application/x-www-form-urlencoded
//	@Param        email     formData  string  true  "User email"
//	@Param        password  formData  string  true  "User password"
//	@Success      303  "Redirect to /dashboard"
//	@Failure      303  "Redirect to /login with error"
//	@Router       /login [post]
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

	// Mint cross-service JWT so *.jredh.com subdomains share auth.
	if user, _, err := h.auth.ValidateSession(sessionID); err == nil && user != nil {
		h.setJWTCookie(w, user)
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// Logout clears the session cookie and deletes the server-side session.
//
//	@Summary      Logout
//	@Description  Clears the session cookie and deletes the server-side session. Redirects to /.
//	@Tags         auth
//	@Success      303  "Redirect to /"
//	@Router       /logout [get]
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

	// Clear the cross-service JWT cookie too.
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "",
		Path:     "/",
		Domain:   h.cfg.JWT.CookieDomain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cfg.Server.Env == "production",
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Signup handles signup form submission.
//
//	@Summary      Sign up via form
//	@Description  Creates a new user account, auto-logs in, and redirects to /dashboard.
//	@Tags         auth
//	@Accept       application/x-www-form-urlencoded
//	@Param        username  formData  string  true   "Username"
//	@Param        email     formData  string  true   "Email address"
//	@Param        phone     formData  string  true   "Phone number"
//	@Param        password  formData  string  true   "Password"
//	@Param        name      formData  string  false  "Display name"
//	@Success      303  "Redirect to /dashboard"
//	@Failure      303  "Redirect to /signup with error"
//	@Router       /signup [post]
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

	// Mint cross-service JWT so *.jredh.com subdomains share auth.
	if user, _, err := h.auth.ValidateSession(sessionID); err == nil && user != nil {
		h.setJWTCookie(w, user)
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// MagicLogin handles GET /auth/magic?token=X — validates a magic login token,
// creates a session, and redirects to the dashboard.
//
//	@Summary      Magic link login
//	@Description  Validates a magic login token, creates a session, and redirects to /dashboard.
//	@Tags         auth
//	@Param        token  query  string  true  "Magic login token"
//	@Success      303  "Redirect to /dashboard"
//	@Failure      400  {string}  string  "Missing token"
//	@Failure      401  {string}  string  "Invalid or expired token"
//	@Router       /auth/magic [get]
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

	// Mint cross-service JWT so *.jredh.com subdomains share auth.
	if user, _, err := h.auth.ValidateSession(sessionID); err == nil && user != nil {
		h.setJWTCookie(w, user)
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// AdminGenerateMagicLink handles POST /admin/magic-link — generates a magic
// login token for a given email and returns it as JSON. Requires admin role.
//
//	@Summary      Generate magic login link (admin)
//	@Description  Generates a magic login URL for a given email. Requires admin role.
//	@Tags         admin
//	@Accept       application/x-www-form-urlencoded
//	@Produce      json
//	@Param        email  formData  string  true  "User email"
//	@Success      200    {object}  map[string]string  "Contains 'link' field"
//	@Failure      400    {string}  string
//	@Failure      404    {string}  string  "User not found"
//	@Router       /admin/magic-link [post]
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
//
//	@Summary      Search available actions
//	@Description  Returns actions matching the query, filtered by auth state.
//	@Tags         actions
//	@Produce      json
//	@Param        q  query     string  false  "Search query"
//	@Success      200  {array}  actions.Action
//	@Router       /api/actions [get]
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

// setJWTCookie mints a cross-service JWT for the given user and sets it as the
// "token" cookie. The cookie uses Domain=cfg.JWT.CookieDomain so all
// *.jredh.com subdomains receive it in production. In development the key may
// be empty — in that case the cookie is still set (for portal-internal use)
// but will be an unsigned placeholder that other services ignore (they use the
// dev bypass instead).
func (h *Handler) setJWTCookie(w http.ResponseWriter, user *models.User) {
	if h.cfg.JWT.SigningKey == "" {
		// Dev: no signing key configured — skip JWT (other services use dev bypass).
		return
	}

	claims := &authmw.Claims{
		Sub:   user.ID,
		Email: user.Email,
		Name:  user.Name,
		Role:  user.Role,
		Exp:   time.Now().Add(time.Duration(h.cfg.Session.MaxAge) * time.Second).Unix(),
	}

	token, err := authmw.MintToken(claims, h.cfg.JWT.SigningKey)
	if err != nil {
		log.Printf("setJWTCookie: mint token for user %s: %v", user.ID, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		Domain:   h.cfg.JWT.CookieDomain, // "jredh.com" in prod, "" locally
		MaxAge:   h.cfg.Session.MaxAge,
		HttpOnly: true,
		Secure:   h.cfg.Server.Env == "production",
		SameSite: http.SameSiteLaxMode,
	})
}

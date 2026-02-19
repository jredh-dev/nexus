package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jredh-dev/nexus/services/portal/config"
	"github.com/jredh-dev/nexus/services/portal/internal/actions"
	"github.com/jredh-dev/nexus/services/portal/internal/auth"
	"github.com/jredh-dev/nexus/services/portal/internal/database"
	"github.com/jredh-dev/nexus/services/portal/internal/web/templates"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	db         *database.DB
	giveawayDB *database.GiveawayDB
	cfg        *config.Config
	auth       *auth.Service
	templates  map[string]*template.Template
	actions    *actions.Registry
}

// New creates a new handler with parsed templates.
func New(db *database.DB, giveawayDB *database.GiveawayDB, cfg *config.Config, authService *auth.Service) *Handler {
	tmplMap := make(map[string]*template.Template)

	// Collect shared templates: base.html + all partials.
	shared := []string{"base.html"}
	partials, err := fs.Glob(templates.FS, "partials/*.html")
	if err != nil {
		log.Fatalf("Error globbing partials: %v", err)
	}
	shared = append(shared, partials...)

	for _, page := range []string{
		"home.html", "login.html", "signup.html", "dashboard.html", "about.html",
		"giveaway.html", "giveaway_item.html",
		"admin_giveaway.html", "admin_giveaway_edit.html",
	} {
		files := make([]string, 0, len(shared)+1)
		files = append(files, shared...)
		files = append(files, page)

		tmplMap[page] = template.Must(
			template.New(page).ParseFS(templates.FS, files...),
		)
	}

	return &Handler{
		db:         db,
		giveawayDB: giveawayDB,
		cfg:        cfg,
		auth:       authService,
		templates:  tmplMap,
		actions:    actions.New(),
	}
}

// AuthService returns the auth service instance.
func (h *Handler) AuthService() *auth.Service {
	return h.auth
}

// Home renders the public landing page.
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	h.renderTemplate(w, "home.html", map[string]interface{}{
		"Year":     time.Now().Year(),
		"LoggedIn": h.isLoggedIn(r),
	})
}

// LoginPage renders the login form.
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	h.renderTemplate(w, "login.html", map[string]interface{}{
		"Title": "Login",
		"Year":  time.Now().Year(),
	})
}

// Login handles login form submission.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		log.Printf("Error parsing login form: %v", err)
		h.loginError(w, "Invalid form data.")
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")

	if email == "" || password == "" {
		h.loginError(w, "Email and password are required.")
		return
	}

	sessionID, err := h.auth.Login(email, password, r.RemoteAddr, r.UserAgent())
	if err != nil {
		log.Printf("Login failed for %s: %v", email, err)
		h.loginError(w, "Invalid email or password.")
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

// SignupPage renders the signup form.
func (h *Handler) SignupPage(w http.ResponseWriter, r *http.Request) {
	h.renderTemplate(w, "signup.html", map[string]interface{}{
		"Title": "Sign Up",
		"Year":  time.Now().Year(),
	})
}

// Signup handles signup form submission.
func (h *Handler) Signup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		log.Printf("Error parsing signup form: %v", err)
		h.signupError(w, "Invalid form data.", r)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(r.FormValue("email"))
	phone := strings.TrimSpace(r.FormValue("phone"))
	password := r.FormValue("password")
	name := strings.TrimSpace(r.FormValue("name"))

	if username == "" || email == "" || phone == "" || password == "" {
		h.signupError(w, "Username, email, phone number, and password are required.", r)
		return
	}

	_, err := h.auth.Signup(username, email, phone, password, name)
	if err != nil {
		log.Printf("Signup failed for %s: %v", email, err)

		switch {
		case errors.Is(err, auth.ErrUsernameTaken):
			h.signupError(w, "This username is already taken.", r)
		case errors.Is(err, auth.ErrEmailTaken):
			h.signupError(w, "An account with this email already exists.", r)
		case errors.Is(err, auth.ErrPhoneTaken):
			h.signupError(w, "An account with this phone number already exists.", r)
		default:
			h.signupError(w, "Something went wrong. Please try again.", r)
		}
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

// Dashboard renders the authenticated dashboard page.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user, ok := GetUserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	sessions, err := h.auth.GetSessionsByUserID(user.ID)
	if err != nil {
		log.Printf("Error fetching sessions for user %s: %v", user.ID, err)
		sessions = nil
	}

	h.renderTemplate(w, "dashboard.html", map[string]interface{}{
		"Title":    "Dashboard",
		"Year":     time.Now().Year(),
		"User":     user,
		"Sessions": sessions,
		"LoggedIn": true,
	})
}

// About renders the about page.
func (h *Handler) About(w http.ResponseWriter, r *http.Request) {
	h.renderTemplate(w, "about.html", map[string]interface{}{
		"Title":    "About",
		"Year":     time.Now().Year(),
		"LoggedIn": h.isLoggedIn(r),
	})
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

func (h *Handler) loginError(w http.ResponseWriter, msg string) {
	h.renderTemplate(w, "login.html", map[string]interface{}{
		"Title": "Login",
		"Year":  time.Now().Year(),
		"Error": msg,
	})
}

func (h *Handler) signupError(w http.ResponseWriter, msg string, r *http.Request) {
	h.renderTemplate(w, "signup.html", map[string]interface{}{
		"Title":    "Sign Up",
		"Year":     time.Now().Year(),
		"Error":    msg,
		"Username": r.FormValue("username"),
		"Email":    r.FormValue("email"),
		"Phone":    r.FormValue("phone"),
		"Name":     r.FormValue("name"),
	})
}

func (h *Handler) renderTemplate(w http.ResponseWriter, name string, data interface{}) {
	tmpl, ok := h.templates[name]
	if !ok {
		http.Error(w, fmt.Sprintf("template %s not found", name), http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("Error rendering template %s: %v", name, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

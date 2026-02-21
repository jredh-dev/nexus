package integration

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jredh-dev/nexus/services/portal/config"
	"github.com/jredh-dev/nexus/services/portal/internal/actions"
	"github.com/jredh-dev/nexus/services/portal/internal/auth"
	"github.com/jredh-dev/nexus/services/portal/internal/database"
	"github.com/jredh-dev/nexus/services/portal/internal/web/handlers"
)

// testServer spins up a full portal stack backed by a temp SQLite file.
// Matches the production router: no Go-rendered pages (Astro owns those).
// Caller must defer cleanup().
func testServer(t *testing.T) (srv *httptest.Server, client *http.Client, cleanup func()) {
	t.Helper()

	root := findMonorepoRoot(t)
	origDir, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir to monorepo root: %v", err)
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	cfg := &config.Config{
		Server:  config.ServerConfig{Port: "0", Env: "test"},
		DB:      config.DBConfig{Path: dbPath},
		Session: config.SessionConfig{Secret: "test-secret", MaxAge: 3600},
	}

	authService := auth.New(db, cfg)
	h := handlers.New(db, cfg, authService, actions.New())

	r := chi.NewRouter()
	// Portal owns: form auth POSTs, logout, magic link, admin, API.
	// Astro owns: GET /, /login, /signup, /about, /dashboard.
	r.Post("/login", h.Login)
	r.Post("/signup", h.Signup)
	r.Get("/logout", h.Logout)
	r.Get("/auth/magic", h.MagicLogin)
	r.Get("/api/actions", h.SearchActions)
	r.Group(func(r chi.Router) {
		r.Use(handlers.AuthMiddleware(authService))
		r.Use(handlers.AdminMiddleware)
		r.Post("/admin/magic-link", h.AdminGenerateMagicLink)
	})

	srv = httptest.NewServer(r)

	jar, _ := cookiejar.New(nil)
	client = &http.Client{
		Jar: jar,
		// Stop at redirects to pages that Astro would serve (404 in test).
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	cleanup = func() {
		srv.Close()
		db.Close()
		_ = os.Chdir(origDir)
	}

	return srv, client, cleanup
}

// findMonorepoRoot walks up directories until it finds go.mod with the nexus module.
func findMonorepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		gomod := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(gomod); err == nil {
			if strings.Contains(string(data), "module github.com/jredh-dev/nexus") {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find monorepo root (go.mod with github.com/jredh-dev/nexus)")
		}
		dir = parent
	}
}

func postForm(client *http.Client, url string, values url.Values) (*http.Response, error) {
	return client.Post(url, "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
}

// --- Tests ---

func TestSignupAndLogin(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	// POST /signup with valid data should redirect to /dashboard (auto-login).
	resp, err := postForm(client, srv.URL+"/signup", url.Values{
		"username": {"testuser"},
		"email":    {"test@example.com"},
		"phone":    {"5551234567"},
		"password": {"securepassword"},
		"name":     {"Test User"},
	})
	if err != nil {
		t.Fatalf("POST /signup: %v", err)
	}
	resp.Body.Close()

	// Portal redirects to /dashboard; Astro would serve that page.
	// In test, we just verify the redirect points to /dashboard.
	if !strings.HasSuffix(resp.Request.URL.Path, "/dashboard") {
		t.Errorf("after signup, redirected to %s, want /dashboard", resp.Request.URL.Path)
	}

	// Logout clears cookie, redirects to /.
	resp, err = client.Get(srv.URL + "/logout")
	if err != nil {
		t.Fatalf("GET /logout: %v", err)
	}
	resp.Body.Close()

	// Login with the credentials we just created.
	resp, err = postForm(client, srv.URL+"/login", url.Values{
		"email":    {"test@example.com"},
		"password": {"securepassword"},
	})
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}
	resp.Body.Close()

	if !strings.HasSuffix(resp.Request.URL.Path, "/dashboard") {
		t.Errorf("after login, redirected to %s, want /dashboard", resp.Request.URL.Path)
	}
}

func TestSignupDuplicateEmail(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	resp, err := postForm(client, srv.URL+"/signup", url.Values{
		"username": {"user1"},
		"email":    {"dup@example.com"},
		"phone":    {"5551111111"},
		"password": {"password1"},
		"name":     {"User One"},
	})
	if err != nil {
		t.Fatalf("first signup: %v", err)
	}
	resp.Body.Close()

	resp, err = client.Get(srv.URL + "/logout")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	resp.Body.Close()

	// Same email (different username/phone) should be rejected.
	resp, err = postForm(client, srv.URL+"/signup", url.Values{
		"username": {"user2"},
		"email":    {"dup@example.com"},
		"phone":    {"5552222222"},
		"password": {"password2"},
		"name":     {"User Two"},
	})
	if err != nil {
		t.Fatalf("duplicate email signup: %v", err)
	}
	resp.Body.Close()

	// Redirects back to /signup with error query param.
	if !strings.Contains(resp.Request.URL.String(), "/signup") {
		t.Errorf("duplicate email: redirected to %s, want /signup", resp.Request.URL)
	}
}

func TestSignupDuplicateGmailAlias(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	resp, err := postForm(client, srv.URL+"/signup", url.Values{
		"username": {"gmailuser"},
		"email":    {"testuser@gmail.com"},
		"phone":    {"5553333333"},
		"password": {"password1"},
		"name":     {"Gmail User"},
	})
	if err != nil {
		t.Fatalf("first signup: %v", err)
	}
	resp.Body.Close()

	resp, err = client.Get(srv.URL + "/logout")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	resp.Body.Close()

	resp, err = postForm(client, srv.URL+"/signup", url.Values{
		"username": {"gmailuser2"},
		"email":    {"test.user+alias@gmail.com"},
		"phone":    {"5554444444"},
		"password": {"password2"},
		"name":     {"Gmail Alias"},
	})
	if err != nil {
		t.Fatalf("gmail alias signup: %v", err)
	}
	resp.Body.Close()

	if !strings.Contains(resp.Request.URL.String(), "/signup") {
		t.Errorf("gmail alias dedup: redirected to %s, want /signup", resp.Request.URL)
	}
}

func TestSignupDuplicatePhone(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	resp, err := postForm(client, srv.URL+"/signup", url.Values{
		"username": {"phoneuser1"},
		"email":    {"phone1@example.com"},
		"phone":    {"(555) 999-8888"},
		"password": {"password1"},
		"name":     {"Phone User 1"},
	})
	if err != nil {
		t.Fatalf("first signup: %v", err)
	}
	resp.Body.Close()

	resp, err = client.Get(srv.URL + "/logout")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	resp.Body.Close()

	resp, err = postForm(client, srv.URL+"/signup", url.Values{
		"username": {"phoneuser2"},
		"email":    {"phone2@example.com"},
		"phone":    {"+15559998888"},
		"password": {"password2"},
		"name":     {"Phone User 2"},
	})
	if err != nil {
		t.Fatalf("duplicate phone signup: %v", err)
	}
	resp.Body.Close()

	if !strings.Contains(resp.Request.URL.String(), "/signup") {
		t.Errorf("phone dedup: redirected to %s, want /signup", resp.Request.URL)
	}
}

func TestSignupDuplicateUsername(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	resp, err := postForm(client, srv.URL+"/signup", url.Values{
		"username": {"takenname"},
		"email":    {"user1@example.com"},
		"phone":    {"5550000001"},
		"password": {"password1"},
		"name":     {"First"},
	})
	if err != nil {
		t.Fatalf("first signup: %v", err)
	}
	resp.Body.Close()

	resp, err = client.Get(srv.URL + "/logout")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	resp.Body.Close()

	resp, err = postForm(client, srv.URL+"/signup", url.Values{
		"username": {"takenname"},
		"email":    {"user2@example.com"},
		"phone":    {"5550000002"},
		"password": {"password2"},
		"name":     {"Second"},
	})
	if err != nil {
		t.Fatalf("duplicate username signup: %v", err)
	}
	resp.Body.Close()

	if !strings.Contains(resp.Request.URL.String(), "/signup") {
		t.Errorf("username dedup: redirected to %s, want /signup", resp.Request.URL)
	}
}

func TestSignupMissingFields(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	resp, err := postForm(client, srv.URL+"/signup", url.Values{
		"username": {"incomplete"},
		"email":    {"incomplete@example.com"},
		"password": {"password"},
		"name":     {"Incomplete"},
	})
	if err != nil {
		t.Fatalf("missing phone signup: %v", err)
	}
	resp.Body.Close()

	if !strings.Contains(resp.Request.URL.String(), "/signup") {
		t.Errorf("missing field: redirected to %s, want /signup", resp.Request.URL)
	}
}

func TestLoginBadCredentials(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	resp, err := postForm(client, srv.URL+"/login", url.Values{
		"email":    {"nobody@example.com"},
		"password": {"wrong"},
	})
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}
	resp.Body.Close()

	// Redirects to /login?error=...
	if !strings.Contains(resp.Request.URL.String(), "/login") {
		t.Errorf("bad login: redirected to %s, want /login", resp.Request.URL)
	}
}

// TestSessionRoundTrip verifies signup sets a session cookie and logout clears it.
func TestSessionRoundTrip(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	resp, err := postForm(client, srv.URL+"/signup", url.Values{
		"username": {"sessionuser"},
		"email":    {"session@example.com"},
		"phone":    {"5557777777"},
		"password": {"password"},
		"name":     {"Session User"},
	})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	resp.Body.Close()

	// Portal should redirect toward /dashboard.
	if !strings.HasSuffix(resp.Request.URL.Path, "/dashboard") {
		t.Fatalf("after signup: redirected to %s, want /dashboard", resp.Request.URL.Path)
	}

	// Verify session cookie exists.
	u, _ := url.Parse(srv.URL)
	cookies := client.Jar.Cookies(u)
	var found bool
	for _, c := range cookies {
		if c.Name == "session" && c.Value != "" {
			found = true
			break
		}
	}
	if !found {
		t.Error("no session cookie set after signup")
	}

	// Logout should clear cookie and redirect to /.
	resp, err = client.Get(srv.URL + "/logout")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	resp.Body.Close()

	cookies = client.Jar.Cookies(u)
	for _, c := range cookies {
		if c.Name == "session" && c.Value != "" {
			t.Error("session cookie still set after logout")
		}
	}
}

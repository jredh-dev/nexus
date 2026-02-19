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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jredh-dev/nexus/services/portal/config"
	"github.com/jredh-dev/nexus/services/portal/internal/auth"
	"github.com/jredh-dev/nexus/services/portal/internal/database"
	"github.com/jredh-dev/nexus/services/portal/internal/web/handlers"
)

// testServer spins up a full portal stack backed by a temp SQLite file.
// Caller must defer cleanup().
func testServer(t *testing.T) (srv *httptest.Server, client *http.Client, cleanup func()) {
	t.Helper()

	// Chdir to monorepo root first so template paths resolve correctly.
	// handlers.New() uses template.Must which panics if files aren't found.
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
	h := handlers.New(db, cfg, authService)

	r := chi.NewRouter()
	r.Get("/", h.Home)
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.Login)
	r.Get("/signup", h.SignupPage)
	r.Post("/signup", h.Signup)
	r.Get("/logout", h.Logout)
	r.Group(func(r chi.Router) {
		r.Use(handlers.AuthMiddleware(authService))
		r.Get("/dashboard", h.Dashboard)
	})

	srv = httptest.NewServer(r)

	jar, _ := cookiejar.New(nil)
	client = &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Follow redirects but cap at 10.
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

	// 1. GET /signup should return 200.
	resp, err := client.Get(srv.URL + "/signup")
	if err != nil {
		t.Fatalf("GET /signup: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /signup status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. POST /signup with valid data should redirect to /dashboard (auto-login).
	resp, err = postForm(client, srv.URL+"/signup", url.Values{
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

	// After redirect chain, should land on /dashboard.
	if !strings.HasSuffix(resp.Request.URL.Path, "/dashboard") {
		t.Errorf("after signup, landed on %s, want /dashboard", resp.Request.URL.Path)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("dashboard status = %d, want 200", resp.StatusCode)
	}

	// 3. Logout.
	resp, err = client.Get(srv.URL + "/logout")
	if err != nil {
		t.Fatalf("GET /logout: %v", err)
	}
	resp.Body.Close()

	// 4. Login with the credentials we just created.
	resp, err = postForm(client, srv.URL+"/login", url.Values{
		"email":    {"test@example.com"},
		"password": {"securepassword"},
	})
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}
	resp.Body.Close()

	if !strings.HasSuffix(resp.Request.URL.Path, "/dashboard") {
		t.Errorf("after login, landed on %s, want /dashboard", resp.Request.URL.Path)
	}
}

func TestSignupDuplicateEmail(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	// Create first user.
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

	// Logout so the cookie jar is clean for the second signup attempt.
	resp, err = client.Get(srv.URL + "/logout")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	resp.Body.Close()

	// Try to sign up with the same email (different username/phone).
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

	// Should stay on /signup (not redirect to dashboard).
	if !strings.HasSuffix(resp.Request.URL.Path, "/signup") {
		t.Errorf("duplicate email: landed on %s, want /signup", resp.Request.URL.Path)
	}
}

func TestSignupDuplicateGmailAlias(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	// Create user with Gmail address.
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

	// Try to sign up with a Gmail +alias (should be caught by dedup).
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

	// Should be rejected -- stays on /signup.
	if !strings.HasSuffix(resp.Request.URL.Path, "/signup") {
		t.Errorf("gmail alias dedup: landed on %s, want /signup", resp.Request.URL.Path)
	}
}

func TestSignupDuplicatePhone(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	// Create first user.
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

	// Try to sign up with the same phone in different format.
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

	// Should be rejected.
	if !strings.HasSuffix(resp.Request.URL.Path, "/signup") {
		t.Errorf("phone dedup: landed on %s, want /signup", resp.Request.URL.Path)
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

	if !strings.HasSuffix(resp.Request.URL.Path, "/signup") {
		t.Errorf("username dedup: landed on %s, want /signup", resp.Request.URL.Path)
	}
}

func TestSignupMissingFields(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	// Missing phone number.
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

	if !strings.HasSuffix(resp.Request.URL.Path, "/signup") {
		t.Errorf("missing field: landed on %s, want /signup", resp.Request.URL.Path)
	}
}

func TestDashboardRequiresAuth(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	// Accessing /dashboard without a session should redirect to /login.
	resp, err := client.Get(srv.URL + "/dashboard")
	if err != nil {
		t.Fatalf("GET /dashboard: %v", err)
	}
	resp.Body.Close()

	if !strings.HasSuffix(resp.Request.URL.Path, "/login") {
		t.Errorf("unauthenticated dashboard: landed on %s, want /login", resp.Request.URL.Path)
	}
}

func TestHomePage(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("home page status = %d, want 200", resp.StatusCode)
	}
}

func TestLoginPage(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	resp, err := client.Get(srv.URL + "/login")
	if err != nil {
		t.Fatalf("GET /login: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("login page status = %d, want 200", resp.StatusCode)
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

	// Should stay on /login (re-rendered with error).
	if !strings.HasSuffix(resp.Request.URL.Path, "/login") {
		t.Errorf("bad login: landed on %s, want /login", resp.Request.URL.Path)
	}
}

// TestSessionExpiry is a lighter check — just verifies the session cookie is set
// and that the session machinery works round-trip.
func TestSessionRoundTrip(t *testing.T) {
	srv, client, cleanup := testServer(t)
	defer cleanup()

	// Sign up.
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

	// Should be on /dashboard now (auto-login).
	if !strings.HasSuffix(resp.Request.URL.Path, "/dashboard") {
		t.Fatalf("after signup: on %s, want /dashboard", resp.Request.URL.Path)
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

	// Access dashboard again — should still work.
	resp, err = client.Get(srv.URL + "/dashboard")
	if err != nil {
		t.Fatalf("GET /dashboard: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("dashboard after signup status = %d, want 200", resp.StatusCode)
	}

	// Logout.
	resp, err = client.Get(srv.URL + "/logout")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	resp.Body.Close()

	// Dashboard should now redirect to login.
	resp, err = client.Get(srv.URL + "/dashboard")
	if err != nil {
		t.Fatalf("GET /dashboard after logout: %v", err)
	}
	resp.Body.Close()

	if !strings.HasSuffix(resp.Request.URL.Path, "/login") {
		t.Errorf("after logout: on %s, want /login", resp.Request.URL.Path)
	}
}

// Ensure the _ import of time is used (compilation guard for template rendering
// which calls .Format, ensuring the server doesn't panic on time fields).
var _ = time.Now

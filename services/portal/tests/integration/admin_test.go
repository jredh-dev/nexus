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
	"github.com/jredh-dev/nexus/services/portal/pkg/models"
)

// testServerWithDB returns the full test server plus the raw DB and auth service
// so tests can manipulate roles and create magic tokens.
func testServerWithDB(t *testing.T) (srv *httptest.Server, client *http.Client, db *database.DB, authSvc *auth.Service, cleanup func()) {
	t.Helper()

	root := findMonorepoRoot(t)
	origDir, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir to monorepo root: %v", err)
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	var err error
	db, err = database.New(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	cfg := &config.Config{
		Server:  config.ServerConfig{Port: "0", Env: "test"},
		DB:      config.DBConfig{Path: dbPath},
		Session: config.SessionConfig{Secret: "test-secret", MaxAge: 3600},
	}

	authSvc = auth.New(db, cfg)
	h := handlers.New(db, cfg, authSvc, actions.New())

	r := chi.NewRouter()
	// Portal owns: form auth POSTs, logout, magic link, admin, API.
	// Astro owns: GET /, /login, /signup, /about, /dashboard.
	r.Post("/login", h.Login)
	r.Post("/signup", h.Signup)
	r.Get("/logout", h.Logout)
	r.Get("/auth/magic", h.MagicLogin)
	r.Group(func(r chi.Router) {
		r.Use(handlers.AuthMiddleware(authSvc))
		r.Use(handlers.AdminMiddleware)
		r.Post("/admin/magic-link", h.AdminGenerateMagicLink)
	})

	srv = httptest.NewServer(r)

	jar, _ := cookiejar.New(nil)
	client = &http.Client{
		Jar: jar,
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

	return srv, client, db, authSvc, cleanup
}

// signupAndLogin creates a user via the signup form and returns with an active session.
func signupAndLogin(t *testing.T, client *http.Client, srvURL, username, email, phone, password, name string) {
	t.Helper()
	resp, err := postForm(client, srvURL+"/signup", url.Values{
		"username": {username},
		"email":    {email},
		"phone":    {phone},
		"password": {password},
		"name":     {name},
	})
	if err != nil {
		t.Fatalf("signup %s: %v", email, err)
	}
	resp.Body.Close()
}

// --- Permission leakage tests ---

func TestAdminRoutes_UnauthenticatedRedirectsToLogin(t *testing.T) {
	srv, client, _, _, cleanup := testServerWithDB(t)
	defer cleanup()

	// Unauthenticated POST to admin route should redirect to /login.
	resp, err := postForm(client, srv.URL+"/admin/magic-link", url.Values{
		"email": {"nobody@example.com"},
	})
	if err != nil {
		t.Fatalf("POST /admin/magic-link: %v", err)
	}
	resp.Body.Close()

	if !strings.HasSuffix(resp.Request.URL.Path, "/login") {
		t.Errorf("unauthenticated POST /admin/magic-link: landed on %s, want /login", resp.Request.URL.Path)
	}
}

func TestAdminRoutes_NonAdminGetsForbidden(t *testing.T) {
	srv, client, _, _, cleanup := testServerWithDB(t)
	defer cleanup()

	// Create a regular user and log in.
	signupAndLogin(t, client, srv.URL, "regularuser", "regular@example.com", "5551111111", "password", "Regular User")

	// Non-admin should get 403 on admin routes.
	// We need a client that doesn't follow redirects for the admin check
	// since AuthMiddleware does redirects but AdminMiddleware returns 403.
	noRedirectClient := &http.Client{
		Jar: client.Jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := noRedirectClient.Post(
		srv.URL+"/admin/magic-link",
		"application/x-www-form-urlencoded",
		strings.NewReader(url.Values{"email": {"regular@example.com"}}.Encode()),
	)
	if err != nil {
		t.Fatalf("POST /admin/magic-link: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin POST /admin/magic-link: status = %d, want 403", resp.StatusCode)
	}
}

func TestAdminRoutes_AdminCanAccess(t *testing.T) {
	srv, client, db, _, cleanup := testServerWithDB(t)
	defer cleanup()

	// Create a user, promote to admin, log in.
	signupAndLogin(t, client, srv.URL, "adminuser", "admin@example.com", "5552222222", "password", "Admin User")

	// Look up and promote to admin.
	user, err := db.GetUserByEmail("admin@example.com")
	if err != nil || user == nil {
		t.Fatalf("lookup admin user: %v", err)
	}
	if err := db.UpdateUserRole(user.ID, models.RoleAdmin); err != nil {
		t.Fatalf("promote to admin: %v", err)
	}

	// Logout and re-login so the session picks up the new role.
	resp, err := client.Get(srv.URL + "/logout")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	resp.Body.Close()

	resp, err = postForm(client, srv.URL+"/login", url.Values{
		"email":    {"admin@example.com"},
		"password": {"password"},
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()

	// Admin should be able to access admin routes.
	noRedirectClient := &http.Client{
		Jar: client.Jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err = noRedirectClient.Post(
		srv.URL+"/admin/magic-link",
		"application/x-www-form-urlencoded",
		strings.NewReader(url.Values{"email": {"admin@example.com"}}.Encode()),
	)
	if err != nil {
		t.Fatalf("POST /admin/magic-link: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("admin POST /admin/magic-link: status = %d, want 200", resp.StatusCode)
	}
}

func TestMagicLogin_ValidToken(t *testing.T) {
	srv, client, _, authSvc, cleanup := testServerWithDB(t)
	defer cleanup()

	// Create a user first.
	_, err := authSvc.CreateUser("magic@example.com", "password", "Magic User")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Generate a magic token.
	token, err := authSvc.CreateMagicToken("magic@example.com")
	if err != nil {
		t.Fatalf("create magic token: %v", err)
	}

	// Use the magic link to log in.
	resp, err := client.Get(srv.URL + "/auth/magic?token=" + token)
	if err != nil {
		t.Fatalf("GET /auth/magic: %v", err)
	}
	resp.Body.Close()

	// Should redirect to /dashboard.
	if !strings.HasSuffix(resp.Request.URL.Path, "/dashboard") {
		t.Errorf("magic login: landed on %s, want /dashboard", resp.Request.URL.Path)
	}
}

func TestMagicLogin_InvalidToken(t *testing.T) {
	srv, _, _, _, cleanup := testServerWithDB(t)
	defer cleanup()

	// Use a non-redirect client so we can see the 401.
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(srv.URL + "/auth/magic?token=bogus-token-12345")
	if err != nil {
		t.Fatalf("GET /auth/magic: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("invalid magic token: status = %d, want 401", resp.StatusCode)
	}
}

func TestMagicLogin_TokenCannotBeReused(t *testing.T) {
	srv, _, _, authSvc, cleanup := testServerWithDB(t)
	defer cleanup()

	// Create a user.
	_, err := authSvc.CreateUser("reuse@example.com", "password", "Reuse User")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	token, err := authSvc.CreateMagicToken("reuse@example.com")
	if err != nil {
		t.Fatalf("create magic token: %v", err)
	}

	// First use should work.
	jar1, _ := cookiejar.New(nil)
	client1 := &http.Client{
		Jar: jar1,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	resp, err := client1.Get(srv.URL + "/auth/magic?token=" + token)
	if err != nil {
		t.Fatalf("first magic login: %v", err)
	}
	resp.Body.Close()

	if !strings.HasSuffix(resp.Request.URL.Path, "/dashboard") {
		t.Errorf("first use: landed on %s, want /dashboard", resp.Request.URL.Path)
	}

	// Second use should fail.
	jar2, _ := cookiejar.New(nil)
	client2 := &http.Client{
		Jar: jar2,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err = client2.Get(srv.URL + "/auth/magic?token=" + token)
	if err != nil {
		t.Fatalf("second magic login: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("reused magic token: status = %d, want 401", resp.StatusCode)
	}
}

func TestMagicLogin_MissingToken(t *testing.T) {
	srv, _, _, _, cleanup := testServerWithDB(t)
	defer cleanup()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(srv.URL + "/auth/magic")
	if err != nil {
		t.Fatalf("GET /auth/magic: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing token: status = %d, want 400", resp.StatusCode)
	}
}

func TestAdminMagicLinkGeneration_NonAdminForbidden(t *testing.T) {
	srv, client, _, _, cleanup := testServerWithDB(t)
	defer cleanup()

	// Create and login as regular user.
	signupAndLogin(t, client, srv.URL, "linkuser", "link@example.com", "5553333333", "password", "Link User")

	noRedirectClient := &http.Client{
		Jar: client.Jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := noRedirectClient.Post(
		srv.URL+"/admin/magic-link",
		"application/x-www-form-urlencoded",
		strings.NewReader(url.Values{"email": {"link@example.com"}}.Encode()),
	)
	if err != nil {
		t.Fatalf("POST /admin/magic-link: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin magic-link generation: status = %d, want 403", resp.StatusCode)
	}
}

func TestUserRole_DefaultIsUser(t *testing.T) {
	_, _, db, authSvc, cleanup := testServerWithDB(t)
	defer cleanup()

	// Create a user via Signup (user-facing method).
	user, err := authSvc.Signup("roletest", "role@example.com", "5554444444", "password", "Role Test")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}

	if user.Role != models.RoleUser {
		t.Errorf("new user role = %q, want %q", user.Role, models.RoleUser)
	}

	// Verify via DB lookup.
	dbUser, err := db.GetUserByEmail("role@example.com")
	if err != nil || dbUser == nil {
		t.Fatalf("lookup user: %v", err)
	}
	if dbUser.Role != models.RoleUser {
		t.Errorf("db user role = %q, want %q", dbUser.Role, models.RoleUser)
	}

	// Promote and verify.
	if err := db.UpdateUserRole(dbUser.ID, models.RoleAdmin); err != nil {
		t.Fatalf("update role: %v", err)
	}
	dbUser, _ = db.GetUserByEmail("role@example.com")
	if dbUser.Role != models.RoleAdmin {
		t.Errorf("after promotion role = %q, want %q", dbUser.Role, models.RoleAdmin)
	}
	if !dbUser.IsAdmin() {
		t.Error("IsAdmin() returned false after promotion")
	}
}

package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jredh-dev/nexus/services/portal/config"
	"github.com/jredh-dev/nexus/services/portal/internal/auth"
	"github.com/jredh-dev/nexus/services/portal/internal/database"
	"github.com/jredh-dev/nexus/services/portal/internal/web/handlers"
	"github.com/jredh-dev/nexus/services/portal/pkg/models"
)

// testServerWithActions sets up a test server that includes the /api/actions route.
func testServerWithActions(t *testing.T) (srv *httptest.Server, client *http.Client, db *database.DB, authSvc *auth.Service, cleanup func()) {
	t.Helper()

	root := findMonorepoRoot(t)
	origDir, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir to monorepo root: %v", err)
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	giveawayDBPath := filepath.Join(dir, "giveaway_test.db")

	var err error
	db, err = database.New(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	giveawayDB, err := database.NewGiveaway(giveawayDBPath)
	if err != nil {
		t.Fatalf("open giveaway test db: %v", err)
	}

	cfg := &config.Config{
		Server:   config.ServerConfig{Port: "0", Env: "test"},
		DB:       config.DBConfig{Path: dbPath},
		Giveaway: config.GiveawayConfig{DBPath: giveawayDBPath},
		Session:  config.SessionConfig{Secret: "test-secret", MaxAge: 3600},
	}

	authSvc = auth.New(db, cfg)
	h := handlers.New(db, giveawayDB, cfg, authSvc)

	r := chi.NewRouter()
	r.Get("/", h.Home)
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.Login)
	r.Get("/signup", h.SignupPage)
	r.Post("/signup", h.Signup)
	r.Get("/logout", h.Logout)
	r.Get("/api/actions", h.SearchActions)
	r.Group(func(r chi.Router) {
		r.Use(handlers.AuthMiddleware(authSvc))
		r.Get("/dashboard", h.Dashboard)
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
		giveawayDB.Close()
		_ = os.Chdir(origDir)
	}

	return srv, client, db, authSvc, cleanup
}

// actionResult is a minimal struct for deserializing API responses.
type actionResult struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Target      string `json:"target"`
}

func getActions(t *testing.T, client *http.Client, srvURL, query string) []actionResult {
	t.Helper()
	resp, err := client.Get(srvURL + "/api/actions?q=" + url.QueryEscape(query))
	if err != nil {
		t.Fatalf("GET /api/actions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/actions status = %d, want 200", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var results []actionResult
	if err := json.Unmarshal(body, &results); err != nil {
		t.Fatalf("unmarshal actions: %v (body: %s)", err, string(body))
	}
	return results
}

func actionIDs(results []actionResult) map[string]bool {
	ids := make(map[string]bool)
	for _, r := range results {
		ids[r.ID] = true
	}
	return ids
}

func TestSearchActions_AnonymousEmptyQuery(t *testing.T) {
	srv, client, _, _, cleanup := testServerWithActions(t)
	defer cleanup()

	results := getActions(t, client, srv.URL, "")
	ids := actionIDs(results)

	// Anonymous user should see public + logged-out actions.
	for _, want := range []string{"nav-home", "nav-about", "nav-giveaway", "nav-login", "nav-signup"} {
		if !ids[want] {
			t.Errorf("expected action %q for anonymous user, not found", want)
		}
	}
	for _, notWant := range []string{"nav-dashboard", "nav-admin-giveaway", "fn-logout"} {
		if ids[notWant] {
			t.Errorf("action %q should not be visible to anonymous user", notWant)
		}
	}
}

func TestSearchActions_AnonymousWithQuery(t *testing.T) {
	srv, client, _, _, cleanup := testServerWithActions(t)
	defer cleanup()

	results := getActions(t, client, srv.URL, "home")
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'home', got %d", len(results))
	}
	if results[0].ID != "nav-home" {
		t.Errorf("result id = %q, want nav-home", results[0].ID)
	}
}

func TestSearchActions_LoggedInUser(t *testing.T) {
	srv, client, _, _, cleanup := testServerWithActions(t)
	defer cleanup()

	// Sign up and log in.
	signupAndLogin(t, client, srv.URL, "actionsuser", "actions@example.com", "5559999999", "password", "Actions User")

	results := getActions(t, client, srv.URL, "")
	ids := actionIDs(results)

	// Logged-in user should see public + logged-in actions, NOT logged-out actions.
	for _, want := range []string{"nav-home", "nav-about", "nav-giveaway", "nav-dashboard", "fn-logout"} {
		if !ids[want] {
			t.Errorf("expected action %q for logged-in user, not found", want)
		}
	}
	for _, notWant := range []string{"nav-login", "nav-signup", "nav-admin-giveaway"} {
		if ids[notWant] {
			t.Errorf("action %q should not be visible to non-admin logged-in user", notWant)
		}
	}
}

func TestSearchActions_AdminUser(t *testing.T) {
	srv, client, db, _, cleanup := testServerWithActions(t)
	defer cleanup()

	// Create user, promote to admin, re-login.
	signupAndLogin(t, client, srv.URL, "adminactions", "adminactions@example.com", "5558888888", "password", "Admin Actions")

	user, err := db.GetUserByEmail("adminactions@example.com")
	if err != nil || user == nil {
		t.Fatalf("lookup user: %v", err)
	}
	if err := db.UpdateUserRole(user.ID, models.RoleAdmin); err != nil {
		t.Fatalf("promote to admin: %v", err)
	}

	// Logout and re-login so session picks up admin role.
	resp, err := client.Get(srv.URL + "/logout")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	resp.Body.Close()

	resp, err = postForm(client, srv.URL+"/login", url.Values{
		"email":    {"adminactions@example.com"},
		"password": {"password"},
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()

	results := getActions(t, client, srv.URL, "")
	ids := actionIDs(results)

	// Admin should see everything except logged-out actions.
	for _, want := range []string{"nav-home", "nav-about", "nav-giveaway", "nav-dashboard", "nav-admin-giveaway", "fn-logout"} {
		if !ids[want] {
			t.Errorf("expected action %q for admin user, not found", want)
		}
	}
	for _, notWant := range []string{"nav-login", "nav-signup"} {
		if ids[notWant] {
			t.Errorf("action %q should not be visible to logged-in admin", notWant)
		}
	}
}

func TestSearchActions_LogoutHiddenForAnonymous(t *testing.T) {
	srv, client, _, _, cleanup := testServerWithActions(t)
	defer cleanup()

	results := getActions(t, client, srv.URL, "logout")
	if len(results) != 0 {
		t.Errorf("anonymous search for 'logout': expected 0 results, got %d", len(results))
	}
}

func TestSearchActions_NoMatch(t *testing.T) {
	srv, client, _, _, cleanup := testServerWithActions(t)
	defer cleanup()

	results := getActions(t, client, srv.URL, "xyznonexistent123")
	if len(results) != 0 {
		t.Errorf("expected 0 results for nonsense query, got %d", len(results))
	}
}

func TestSearchActions_ResponseFormat(t *testing.T) {
	srv, client, _, _, cleanup := testServerWithActions(t)
	defer cleanup()

	results := getActions(t, client, srv.URL, "about")
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'about'")
	}

	r := results[0]
	if r.ID == "" {
		t.Error("action id is empty")
	}
	if r.Type == "" {
		t.Error("action type is empty")
	}
	if r.Title == "" {
		t.Error("action title is empty")
	}
	if r.Description == "" {
		t.Error("action description is empty")
	}
	if r.Target == "" {
		t.Error("action target is empty")
	}
}

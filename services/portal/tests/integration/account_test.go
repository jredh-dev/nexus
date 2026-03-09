//go:build integration

package integration

// Email change and account management integration tests.
//
// These tests require:
//   - Mailpit running with SMTP on localhost:1025 and HTTP API on localhost:8025
//     (or the addresses configured via MAILPIT_SMTP_HOST/PORT and MAILPIT_URL)
//   - The portal SMTP config pointing at that Mailpit instance
//
// Run with:
//
//	go test -tags integration ./services/portal/tests/integration/... -run TestAccount -v

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jredh-dev/nexus/services/portal/config"
	"github.com/jredh-dev/nexus/services/portal/internal/actions"
	"github.com/jredh-dev/nexus/services/portal/internal/auth"
	"github.com/jredh-dev/nexus/services/portal/internal/database"
	"github.com/jredh-dev/nexus/services/portal/internal/web/handlers"
)

// --- Mailpit helpers ---

// mailpitURL returns the base URL for the Mailpit HTTP API.
func mailpitURL() string {
	if u := os.Getenv("MAILPIT_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:8025"
}

// mailpitSMTPHost returns the SMTP host for Mailpit.
func mailpitSMTPHost() string {
	if h := os.Getenv("MAILPIT_SMTP_HOST"); h != "" {
		return h
	}
	return "localhost"
}

// mailpitSMTPPort returns the SMTP port for Mailpit.
func mailpitSMTPPort() string {
	if p := os.Getenv("MAILPIT_SMTP_PORT"); p != "" {
		return p
	}
	return "1025"
}

// purgeMailpit deletes all messages from Mailpit before a test run.
func purgeMailpit(t *testing.T) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, mailpitURL()+"/api/v1/messages", nil)
	if err != nil {
		t.Fatalf("mailpit purge request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mailpit purge: %v (is Mailpit running at %s?)", err, mailpitURL())
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mailpit purge status = %d, want 200", resp.StatusCode)
	}
}

// mailpitMessage is the summary shape returned by GET /api/v1/messages.
type mailpitMessage struct {
	ID      string `json:"ID"`
	Subject string `json:"Subject"`
	To      []struct {
		Address string `json:"Address"`
	} `json:"To"`
}

// mailpitListResponse is the top-level shape of GET /api/v1/messages.
type mailpitListResponse struct {
	Messages []mailpitMessage `json:"messages"`
	Total    int              `json:"total"`
}

// mailpitFullMessage is the shape returned by GET /api/v1/message/{id}.
type mailpitFullMessage struct {
	ID   string `json:"ID"`
	Text string `json:"Text"`
}

// waitForMail polls Mailpit until at least one message arrives addressed to
// toAddr, or the timeout is reached. Returns the first matching message.
func waitForMail(t *testing.T, toAddr string, timeout time.Duration) mailpitMessage {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(mailpitURL() + "/api/v1/messages")
		if err != nil {
			t.Fatalf("mailpit list: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var list mailpitListResponse
		if err := json.Unmarshal(body, &list); err != nil {
			t.Fatalf("mailpit list unmarshal: %v (body: %s)", err, string(body))
		}

		for _, msg := range list.Messages {
			for _, to := range msg.To {
				if strings.EqualFold(to.Address, toAddr) {
					return msg
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for email to %s (mailpit at %s)", toAddr, mailpitURL())
	return mailpitMessage{} // unreachable
}

// fetchMailText fetches the plain-text body of a Mailpit message by ID.
func fetchMailText(t *testing.T, msgID string) string {
	t.Helper()
	resp, err := http.Get(mailpitURL() + "/api/v1/message/" + msgID)
	if err != nil {
		t.Fatalf("mailpit fetch message %s: %v", msgID, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var full mailpitFullMessage
	if err := json.Unmarshal(body, &full); err != nil {
		t.Fatalf("mailpit message unmarshal: %v (body: %s)", err, string(body))
	}
	return full.Text
}

// extractConfirmURL finds the first http(s) URL in text that contains the path
// /auth/email-change. This is the confirmation link we sent in the email.
func extractConfirmURL(t *testing.T, text string) string {
	t.Helper()
	re := regexp.MustCompile(`https?://[^\s]+/auth/email-change\?token=[^\s]+`)
	match := re.FindString(text)
	if match == "" {
		t.Fatalf("no /auth/email-change link found in email body:\n%s", text)
	}
	return match
}

// --- Test server with account routes ---

// testServerWithAccount spins up a full portal stack wired with the account
// management routes, pointing SMTP at local Mailpit.
func testServerWithAccount(t *testing.T) (srv *httptest.Server, client *http.Client, db *database.DB, authSvc *auth.Service, cleanup func()) {
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
		SMTP: config.SMTPConfig{
			Host: mailpitSMTPHost(),
			Port: mailpitSMTPPort(),
			From: "noreply@test.local",
		},
	}

	authSvc = auth.New(db, cfg)
	h := handlers.New(db, cfg, authSvc, actions.New())

	r := chi.NewRouter()
	r.Post("/login", h.Login)
	r.Post("/signup", h.Signup)
	r.Get("/logout", h.Logout)
	r.Get("/auth/magic", h.MagicLogin)
	r.Get("/auth/email-change", h.ConfirmEmailChange)
	r.Get("/api/actions", h.SearchActions)
	r.Route("/api/me", func(r chi.Router) {
		r.Use(handlers.APIAuthMiddleware(authSvc))
		r.Get("/", h.GetMe)
		r.Post("/email", h.ChangeEmail)
		r.Delete("/", h.DeleteAccount)
	})
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

// --- Tests ---

// TestGetMe_Unauthenticated checks that GET /api/me returns 401 JSON when no
// session is present (not an HTML redirect, which would break fetch() callers).
func TestGetMe_Unauthenticated(t *testing.T) {
	srv, _, _, _, cleanup := testServerWithAccount(t)
	defer cleanup()

	jar, _ := cookiejar.New(nil)
	bare := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := bare.Get(srv.URL + "/api/me/")
	if err != nil {
		t.Fatalf("GET /api/me/: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated GET /api/me/: status = %d, want 401", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// TestGetMe_Authenticated verifies the profile endpoint returns correct fields
// after a signup+login.
func TestGetMe_Authenticated(t *testing.T) {
	srv, client, _, _, cleanup := testServerWithAccount(t)
	defer cleanup()

	signupAndLogin(t, client, srv.URL, "meuser", "me@example.com", "5550000100", "password", "Me User")

	resp, err := client.Get(srv.URL + "/api/me/")
	if err != nil {
		t.Fatalf("GET /api/me/: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /api/me/ status = %d, body: %s", resp.StatusCode, body)
	}

	var profile struct {
		Email    string `json:"email"`
		Username string `json:"username"`
		IsAdmin  bool   `json:"is_admin"`
		IsActive bool   `json:"is_active"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		t.Fatalf("decode /api/me/ response: %v", err)
	}
	if profile.Email != "me@example.com" {
		t.Errorf("email = %q, want me@example.com", profile.Email)
	}
	if profile.Username != "meuser" {
		t.Errorf("username = %q, want meuser", profile.Username)
	}
	if profile.IsAdmin {
		t.Error("is_admin = true, want false for regular user")
	}
	if !profile.IsActive {
		t.Error("is_active = false, want true for user with a role")
	}
}

// TestEmailChange_FullFlow is the main workflow test:
//  1. Sign up + log in
//  2. POST /api/me/email → portal sends verification email via Mailpit SMTP
//  3. Poll Mailpit HTTP API to retrieve the email
//  4. Extract the confirmation link from the email body
//  5. Rewrite the link to point at the httptest server and GET it
//  6. Verify portal redirects to /account?success=email-changed
//  7. Confirm the DB now stores the new email via GET /api/me
func TestEmailChange_FullFlow(t *testing.T) {
	srv, client, _, _, cleanup := testServerWithAccount(t)
	defer cleanup()
	purgeMailpit(t)

	const (
		originalEmail = "original@example.com"
		newEmail      = "new-address@example.com"
	)

	signupAndLogin(t, client, srv.URL, "changeuser", originalEmail, "5550000200", "password", "Change User")

	// Request email change — portal should dispatch to Mailpit SMTP.
	reqBody, _ := json.Marshal(map[string]string{"new_email": newEmail})
	resp, err := client.Post(srv.URL+"/api/me/email", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /api/me/email: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/me/email status = %d, body: %s", resp.StatusCode, body)
	}

	// Poll Mailpit for the verification email (up to 5 seconds).
	msg := waitForMail(t, newEmail, 5*time.Second)
	if !strings.Contains(msg.Subject, "Confirm") {
		t.Errorf("subject = %q, want something containing 'Confirm'", msg.Subject)
	}

	// Fetch full message body and extract the confirmation link.
	text := fetchMailText(t, msg.ID)
	confirmURL := extractConfirmURL(t, text)

	// The link was built with the httptest server's host, so we can hit it directly.
	// Parse it to ensure we call the test server, not some other host.
	parsed, err := url.Parse(confirmURL)
	if err != nil {
		t.Fatalf("parse confirm URL %q: %v", confirmURL, err)
	}
	srvParsed, _ := url.Parse(srv.URL)
	parsed.Scheme = srvParsed.Scheme
	parsed.Host = srvParsed.Host
	localConfirm := parsed.String()

	// A fresh client (no session) hits the confirmation link.
	confirmJar, _ := cookiejar.New(nil)
	confirmClient := &http.Client{
		Jar: confirmJar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	resp, err = confirmClient.Get(localConfirm)
	if err != nil {
		t.Fatalf("GET confirm link: %v", err)
	}
	resp.Body.Close()

	// Portal should redirect to /account?success=email-changed.
	if !strings.Contains(resp.Request.URL.String(), "success=email-changed") {
		t.Errorf("after confirm, landed on %s, want /account?success=email-changed", resp.Request.URL)
	}

	// Verify the email was actually updated — original session should reflect new email.
	resp, err = client.Get(srv.URL + "/api/me/")
	if err != nil {
		t.Fatalf("GET /api/me/ after email change: %v", err)
	}
	defer resp.Body.Close()

	var profile struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		t.Fatalf("decode /api/me/: %v", err)
	}
	if profile.Email != newEmail {
		t.Errorf("after email change: email = %q, want %q", profile.Email, newEmail)
	}
}

// TestEmailChange_TokenCannotBeReused verifies the confirmation link is one-time-use.
func TestEmailChange_TokenCannotBeReused(t *testing.T) {
	srv, client, _, _, cleanup := testServerWithAccount(t)
	defer cleanup()
	purgeMailpit(t)

	signupAndLogin(t, client, srv.URL, "reuseuser", "reuse-original@example.com", "5550000300", "password", "Reuse User")

	reqBody, _ := json.Marshal(map[string]string{"new_email": "reuse-new@example.com"})
	resp, err := client.Post(srv.URL+"/api/me/email", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /api/me/email: %v", err)
	}
	resp.Body.Close()

	msg := waitForMail(t, "reuse-new@example.com", 5*time.Second)
	text := fetchMailText(t, msg.ID)
	confirmURL := extractConfirmURL(t, text)

	// Rewrite to test server host.
	parsed, _ := url.Parse(confirmURL)
	srvParsed, _ := url.Parse(srv.URL)
	parsed.Scheme = srvParsed.Scheme
	parsed.Host = srvParsed.Host
	localConfirm := parsed.String()

	// First use should succeed.
	resp, err = http.Get(localConfirm)
	if err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	resp.Body.Close()
	if !strings.Contains(resp.Request.URL.String(), "success=email-changed") {
		t.Errorf("first use: landed on %s, want email-changed", resp.Request.URL)
	}

	// Second use on same token should return 401.
	noRedirect := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err = noRedirect.Get(localConfirm)
	if err != nil {
		t.Fatalf("second confirm: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("reused confirm token: status = %d, want 401", resp.StatusCode)
	}
}

// TestEmailChange_DuplicateEmail checks that requesting a change to an address
// already owned by another account returns 409 Conflict.
func TestEmailChange_DuplicateEmail(t *testing.T) {
	srv, client, _, authSvc, cleanup := testServerWithAccount(t)
	defer cleanup()
	purgeMailpit(t)

	// Create a second user that already owns the target address.
	_, err := authSvc.CreateUser("taken@example.com", "password", "Taken User")
	if err != nil {
		t.Fatalf("create taken user: %v", err)
	}

	signupAndLogin(t, client, srv.URL, "dupuser", "dup-original@example.com", "5550000400", "password", "Dup User")

	reqBody, _ := json.Marshal(map[string]string{"new_email": "taken@example.com"})
	noRedirect := &http.Client{
		Jar: client.Jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := noRedirect.Post(srv.URL+"/api/me/email", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /api/me/email: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("duplicate email change: status = %d, want 409 (body: %s)", resp.StatusCode, body)
	}
}

// TestDeleteAccount_FullFlow verifies that DELETE /api/me removes the account,
// clears the session cookie, and subsequent GET /api/me returns 401.
func TestDeleteAccount_FullFlow(t *testing.T) {
	srv, client, _, _, cleanup := testServerWithAccount(t)
	defer cleanup()

	signupAndLogin(t, client, srv.URL, "deleteuser", "delete@example.com", "5550000500", "password", "Delete User")

	// Sanity-check: authenticated before delete.
	resp, err := client.Get(srv.URL + "/api/me/")
	if err != nil {
		t.Fatalf("GET /api/me/ before delete: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pre-delete /api/me/ status = %d, want 200", resp.StatusCode)
	}

	// Issue DELETE /api/me/.
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/me/", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/me/: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /api/me/ status = %d, body: %s", resp.StatusCode, body)
	}

	// After deletion, GET /api/me/ should return 401 — session and user are gone.
	noRedirect := &http.Client{
		Jar: client.Jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err = noRedirect.Get(srv.URL + "/api/me/")
	if err != nil {
		t.Fatalf("GET /api/me/ after delete: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("after account deletion GET /api/me/: status = %d, want 401", resp.StatusCode)
	}
}

// TestDeleteAccount_Unauthenticated checks that DELETE /api/me without a
// session returns 401 JSON, not a redirect.
func TestDeleteAccount_Unauthenticated(t *testing.T) {
	srv, _, _, _, cleanup := testServerWithAccount(t)
	defer cleanup()

	noRedirect := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/me/", nil)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/me/: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated DELETE /api/me/: status = %d, want 401", resp.StatusCode)
	}
}

// TestEmailChange_Unauthenticated checks POST /api/me/email without session.
func TestEmailChange_Unauthenticated(t *testing.T) {
	srv, _, _, _, cleanup := testServerWithAccount(t)
	defer cleanup()

	reqBody, _ := json.Marshal(map[string]string{"new_email": "anything@example.com"})
	noRedirect := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := noRedirect.Post(srv.URL+"/api/me/email", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /api/me/email: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated POST /api/me/email: status = %d, want 401", resp.StatusCode)
	}
}

// TestEmailChange_InvalidToken checks GET /auth/email-change with a bogus token.
func TestEmailChange_InvalidToken(t *testing.T) {
	srv, _, _, _, cleanup := testServerWithAccount(t)
	defer cleanup()

	noRedirect := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := noRedirect.Get(fmt.Sprintf("%s/auth/email-change?token=bogus-token-xyz", srv.URL))
	if err != nil {
		t.Fatalf("GET /auth/email-change: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("invalid email change token: status = %d, want 401", resp.StatusCode)
	}
}

// TestEmailChange_MissingToken checks GET /auth/email-change with no token param.
func TestEmailChange_MissingToken(t *testing.T) {
	srv, _, _, _, cleanup := testServerWithAccount(t)
	defer cleanup()

	noRedirect := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := noRedirect.Get(srv.URL + "/auth/email-change")
	if err != nil {
		t.Fatalf("GET /auth/email-change: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing token: status = %d, want 400", resp.StatusCode)
	}
}

package authmw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jredh-dev/nexus/internal/authmw"
)

// --- MintToken / verifyToken (via round-trip) ---

func TestMintAndVerify(t *testing.T) {
	claims := &authmw.Claims{
		Sub:   "abc123",
		Email: "user@example.com",
		Name:  "Test User",
		Role:  "user",
		Exp:   time.Now().Add(time.Hour).Unix(),
	}

	token, err := authmw.MintToken(claims, "supersecret")
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}

	// Round-trip via Middleware in production mode.
	var gotClaims *authmw.Claims
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := authmw.ClaimsFromContext(r.Context())
		if !ok {
			t.Error("claims not in context")
		}
		gotClaims = c
	})

	mw := authmw.Middleware("production", "supersecret")(next)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	mw.ServeHTTP(httptest.NewRecorder(), req)

	if gotClaims == nil {
		t.Fatal("claims is nil after middleware")
	}
	if gotClaims.Sub != "abc123" {
		t.Errorf("sub: got %q want %q", gotClaims.Sub, "abc123")
	}
	if gotClaims.Email != "user@example.com" {
		t.Errorf("email: got %q want %q", gotClaims.Email, "user@example.com")
	}
}

func TestWrongKey(t *testing.T) {
	claims := &authmw.Claims{Sub: "x", Exp: time.Now().Add(time.Hour).Unix()}
	token, _ := authmw.MintToken(claims, "key-a")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with wrong key")
	})
	mw := authmw.Middleware("production", "key-b")(next)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d want 401", rec.Code)
	}
}

func TestExpiredToken(t *testing.T) {
	claims := &authmw.Claims{Sub: "x", Exp: time.Now().Add(-time.Hour).Unix()} // expired
	token, _ := authmw.MintToken(claims, "key")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with expired token")
	})
	mw := authmw.Middleware("production", "key")(next)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d want 401", rec.Code)
	}
}

func TestNoCookie(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called without cookie")
	})
	mw := authmw.Middleware("production", "key")(next)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d want 401", rec.Code)
	}
}

func TestDevBypass(t *testing.T) {
	var gotClaims *authmw.Claims
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := authmw.ClaimsFromContext(r.Context())
		if !ok {
			t.Error("dev user not in context")
		}
		gotClaims = c
	})

	// Any env other than "production" should bypass.
	for _, env := range []string{"development", "dev", "test", ""} {
		mw := authmw.Middleware(env, "key")(next)
		req := httptest.NewRequest("GET", "/", nil) // no cookie
		mw.ServeHTTP(httptest.NewRecorder(), req)

		if gotClaims == nil {
			t.Fatalf("env=%q: dev user not injected", env)
		}
		if gotClaims.Sub != "dev" {
			t.Errorf("env=%q: sub: got %q want dev", env, gotClaims.Sub)
		}
	}
}

func TestMintEmptyKey(t *testing.T) {
	_, err := authmw.MintToken(&authmw.Claims{Sub: "x"}, "")
	if err == nil {
		t.Error("expected error with empty signing key")
	}
}

func TestIsAdmin(t *testing.T) {
	admin := &authmw.Claims{Role: "admin"}
	user := &authmw.Claims{Role: "user"}
	if !admin.IsAdmin() {
		t.Error("admin.IsAdmin() should be true")
	}
	if user.IsAdmin() {
		t.Error("user.IsAdmin() should be false")
	}
}

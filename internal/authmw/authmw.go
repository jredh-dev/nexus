// Package authmw provides JWT-based authentication middleware shared across
// nexus services. In non-production environments the middleware bypasses all
// validation and injects a static dev user, so local Docker stacks require
// zero auth configuration.
//
// Cloud (ENV=production):
//
//	Cookie "token" must carry a valid HMAC-SHA256 signed JWT issued by portal.
//	The same JWT_SIGNING_KEY secret is shared by every service that uses this
//	middleware — it is stored in GCP Secret Manager as "jwt-signing-key-dev".
//
// Local (any other ENV):
//
//	No cookie required. DevUser is injected into context automatically.
//	This gives every local service a uniform "logged in" state without
//	running portal or managing tokens.
package authmw

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// contextKey is a private type for context keys to avoid collisions.
type contextKey string

const (
	// UserKey is the context key for the authenticated Claims.
	UserKey contextKey = "authmw_user"
)

// Claims contains the fields encoded in the JWT payload.
type Claims struct {
	Sub   string `json:"sub"`   // user ID
	Email string `json:"email"` // user email
	Name  string `json:"name"`  // display name
	Role  string `json:"role"`  // "user" or "admin"
	Exp   int64  `json:"exp"`   // unix timestamp expiry
}

// IsAdmin returns true if the user has the admin role.
func (c *Claims) IsAdmin() bool { return c.Role == "admin" }

// devUser is the static identity injected in non-production environments.
// Every local service sees the same user without any portal dependency.
var devUser = &Claims{
	Sub:   "dev",
	Email: "dev@local",
	Name:  "Dev User",
	Role:  "admin", // admin locally so all management endpoints are accessible
}

// Middleware returns an HTTP middleware that validates the JWT in the "token"
// cookie. In non-production (ENV != "production") it skips validation and
// injects devUser into context instead.
//
// On validation failure the request is rejected with 401 Unauthorized.
// Use RequireAuth (below) as the middleware; use ClaimsFromContext to read the
// user downstream.
func Middleware(env, signingKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// --- dev bypass ---
			if env != "production" {
				ctx := context.WithValue(r.Context(), UserKey, devUser)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// --- production: validate JWT cookie ---
			cookie, err := r.Cookie("token")
			if err != nil || cookie.Value == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			claims, err := verifyToken(cookie.Value, signingKey)
			if err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromContext extracts the authenticated Claims from context.
// Returns (nil, false) if the context was not populated by Middleware.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(UserKey).(*Claims)
	return c, ok && c != nil
}

// --- JWT implementation (HMAC-SHA256, no external deps) ---
//
// We implement a minimal JWT subset rather than pulling in a third-party
// library. Format: base64url(header).base64url(payload).base64url(sig)
// where sig = HMAC-SHA256(signingKey, header.payload).

const jwtHeader = `{"alg":"HS256","typ":"JWT"}`

// MintToken creates a signed JWT for the given claims. exp is the absolute
// unix expiry timestamp (e.g. time.Now().Add(7*24*time.Hour).Unix()).
func MintToken(claims *Claims, signingKey string) (string, error) {
	if signingKey == "" {
		return "", fmt.Errorf("authmw: signing key is empty")
	}

	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(jwtHeader))

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("authmw: marshal claims: %w", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)

	sigInput := headerB64 + "." + payloadB64
	sig := hmacSHA256([]byte(signingKey), []byte(sigInput))
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return sigInput + "." + sigB64, nil
}

// verifyToken validates a JWT string and returns its claims.
// Returns an error if the signature is invalid or the token is expired.
func verifyToken(token, signingKey string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed token")
	}

	// Verify signature.
	sigInput := parts[0] + "." + parts[1]
	expectedSig := hmacSHA256([]byte(signingKey), []byte(sigInput))
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode sig: %w", err)
	}
	if !hmac.Equal(expectedSig, gotSig) {
		return nil, fmt.Errorf("invalid signature")
	}

	// Decode payload.
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	// Check expiry.
	if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("token expired")
	}

	return &claims, nil
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

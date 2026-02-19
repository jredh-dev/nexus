package handlers

import (
	"context"
	"log"
	"net/http"

	"github.com/jredh-dev/nexus/services/portal/internal/auth"
	"github.com/jredh-dev/nexus/services/portal/internal/database"
	"github.com/jredh-dev/nexus/services/portal/pkg/models"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// UserContextKey is the key for storing user in request context
	UserContextKey contextKey = "user"
)

// AuthMiddleware creates a middleware that requires authentication
func AuthMiddleware(db *database.DB, authService *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Get session cookie
			cookie, err := r.Cookie("session")
			if err != nil {
				// No session cookie - redirect to login
				log.Printf("No session cookie found, redirecting to login")
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			// Verify session cookie with Firebase
			token, err := authService.VerifySessionCookie(ctx, cookie.Value)
			if err != nil {
				// Invalid session - clear cookie and redirect to login
				log.Printf("Invalid session cookie: %v", err)
				http.SetCookie(w, &http.Cookie{
					Name:   "session",
					Value:  "",
					Path:   "/",
					MaxAge: -1,
				})
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			// Get user from database
			user, err := authService.GetUserByID(ctx, token.UID)
			if err != nil {
				// User not found - clear session and redirect
				log.Printf("User not found for UID %s: %v", token.UID, err)
				http.SetCookie(w, &http.Cookie{
					Name:   "session",
					Value:  "",
					Path:   "/",
					MaxAge: -1,
				})
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			// Add user to request context
			ctx = context.WithValue(ctx, UserContextKey, user)
			r = r.WithContext(ctx)

			// Call next handler
			next.ServeHTTP(w, r)
		})
	}
}

// GetUserFromContext extracts the authenticated user from request context
func GetUserFromContext(ctx context.Context) (*models.User, bool) {
	user, ok := ctx.Value(UserContextKey).(*models.User)
	return user, ok
}

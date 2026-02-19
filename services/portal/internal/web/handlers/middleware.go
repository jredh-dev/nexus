package handlers

import (
	"context"
	"log"
	"net/http"

	"github.com/jredh-dev/nexus/services/portal/internal/auth"
	"github.com/jredh-dev/nexus/services/portal/pkg/models"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	// UserContextKey stores the authenticated user in request context.
	UserContextKey contextKey = "user"
)

// AuthMiddleware requires a valid session cookie.
// On failure it redirects to /login.
func AuthMiddleware(authService *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session")
			if err != nil || cookie.Value == "" {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			user, _, err := authService.ValidateSession(cookie.Value)
			if err != nil {
				log.Printf("Session validation error: %v", err)
				clearSessionCookie(w)
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			if user == nil {
				clearSessionCookie(w)
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserFromContext extracts the authenticated user from request context.
func GetUserFromContext(ctx context.Context) (*models.User, bool) {
	user, ok := ctx.Value(UserContextKey).(*models.User)
	return user, ok
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

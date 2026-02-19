package actions

import "strings"

// ActionType categorizes what an action does when executed.
type ActionType string

const (
	TypeNavigation ActionType = "navigation"
	TypeFunction   ActionType = "function"
)

// Visibility controls when an action appears based on auth state.
type Visibility int

const (
	VisibleAlways    Visibility = iota // Everyone sees it
	VisibleLoggedOut                   // Only when not logged in
	VisibleLoggedIn                    // Only when logged in
	VisibleAdmin                       // Only admins
)

// Action represents a single executable action available in the magic bar.
type Action struct {
	ID          string     `json:"id"`
	Type        ActionType `json:"type"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	// For navigation actions: the URL to navigate to.
	// For function actions: a client-side function identifier.
	Target     string     `json:"target"`
	Keywords   []string   `json:"keywords"`
	Visibility Visibility `json:"-"` // Not serialized — server-side filtering only
}

// SearchContext provides auth state for filtering actions.
type SearchContext struct {
	LoggedIn bool
	IsAdmin  bool
}

// Registry holds all available actions and supports filtered search.
type Registry struct {
	actions []Action
}

// New creates a Registry pre-populated with the default portal actions.
func New() *Registry {
	return &Registry{
		actions: defaultActions(),
	}
}

// Search returns actions matching the query that are visible given the context.
// An empty query returns all visible actions. Matching is case-insensitive substring.
func (r *Registry) Search(query string, ctx SearchContext) []Action {
	q := strings.ToLower(strings.TrimSpace(query))
	var results []Action

	for _, a := range r.actions {
		if !isVisible(a, ctx) {
			continue
		}
		if q == "" || matchesQuery(a, q) {
			results = append(results, a)
		}
	}
	return results
}

func isVisible(a Action, ctx SearchContext) bool {
	switch a.Visibility {
	case VisibleAlways:
		return true
	case VisibleLoggedOut:
		return !ctx.LoggedIn
	case VisibleLoggedIn:
		return ctx.LoggedIn
	case VisibleAdmin:
		return ctx.IsAdmin
	default:
		return true
	}
}

func matchesQuery(a Action, q string) bool {
	if strings.Contains(strings.ToLower(a.Title), q) {
		return true
	}
	if strings.Contains(strings.ToLower(a.Description), q) {
		return true
	}
	for _, kw := range a.Keywords {
		if strings.Contains(strings.ToLower(kw), q) {
			return true
		}
	}
	return false
}

// defaultActions returns the built-in set of portal actions.
func defaultActions() []Action {
	return []Action{
		// Public navigation — always visible
		{
			ID:          "nav-home",
			Type:        TypeNavigation,
			Title:       "Home",
			Description: "Go to the home page",
			Target:      "/",
			Keywords:    []string{"home", "landing", "start", "main"},
			Visibility:  VisibleAlways,
		},
		{
			ID:          "nav-about",
			Type:        TypeNavigation,
			Title:       "About",
			Description: "Learn about Hooper Works",
			Target:      "/about",
			Keywords:    []string{"about", "bio", "jared", "hooper", "info"},
			Visibility:  VisibleAlways,
		},
		{
			ID:          "nav-giveaway",
			Type:        TypeNavigation,
			Title:       "Free Stuff",
			Description: "Browse free items available for pickup or delivery",
			Target:      "/giveaway",
			Keywords:    []string{"giveaway", "free", "stuff", "items", "pickup", "delivery", "claim"},
			Visibility:  VisibleAlways,
		},

		// Auth pages — only when logged out
		{
			ID:          "nav-login",
			Type:        TypeNavigation,
			Title:       "Login",
			Description: "Sign in to your account",
			Target:      "/login",
			Keywords:    []string{"login", "sign in", "signin", "account", "auth"},
			Visibility:  VisibleLoggedOut,
		},
		{
			ID:          "nav-signup",
			Type:        TypeNavigation,
			Title:       "Sign Up",
			Description: "Create a new account",
			Target:      "/signup",
			Keywords:    []string{"signup", "sign up", "register", "create account", "new account"},
			Visibility:  VisibleLoggedOut,
		},

		// Logged-in navigation
		{
			ID:          "nav-dashboard",
			Type:        TypeNavigation,
			Title:       "Dashboard",
			Description: "View your account dashboard",
			Target:      "/dashboard",
			Keywords:    []string{"dashboard", "account", "profile", "settings", "sessions"},
			Visibility:  VisibleLoggedIn,
		},

		// Admin navigation
		{
			ID:          "nav-admin-giveaway",
			Type:        TypeNavigation,
			Title:       "Manage Giveaways",
			Description: "Admin panel for giveaway items and claims",
			Target:      "/admin/giveaway",
			Keywords:    []string{"admin", "manage", "giveaway", "items", "claims", "crud"},
			Visibility:  VisibleAdmin,
		},

		// Function actions — logged in only
		{
			ID:          "fn-logout",
			Type:        TypeFunction,
			Title:       "Logout",
			Description: "Sign out of your account",
			Target:      "logout",
			Keywords:    []string{"logout", "log out", "sign out", "signout", "exit"},
			Visibility:  VisibleLoggedIn,
		},
	}
}

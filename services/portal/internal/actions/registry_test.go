package actions

import (
	"testing"
)

func TestNew_ReturnsNonEmptyRegistry(t *testing.T) {
	reg := New()
	results := reg.Search("", SearchContext{})
	if len(results) == 0 {
		t.Fatal("expected default actions, got none")
	}
}

func TestSearch_EmptyQueryReturnsVisibleActions(t *testing.T) {
	reg := New()

	tests := []struct {
		name     string
		ctx      SearchContext
		wantMin  int
		wantMax  int
		mustHave []string
		mustNot  []string
	}{
		{
			name:     "anonymous sees public + logged-out actions",
			ctx:      SearchContext{},
			mustHave: []string{"nav-home", "nav-about", "nav-giveaway", "nav-login", "nav-signup"},
			mustNot:  []string{"nav-dashboard", "nav-admin-giveaway", "fn-logout"},
		},
		{
			name:     "logged-in user sees public + logged-in actions",
			ctx:      SearchContext{LoggedIn: true},
			mustHave: []string{"nav-home", "nav-about", "nav-giveaway", "nav-dashboard", "fn-logout"},
			mustNot:  []string{"nav-login", "nav-signup", "nav-admin-giveaway"},
		},
		{
			name:     "admin sees public + logged-in + admin actions",
			ctx:      SearchContext{LoggedIn: true, IsAdmin: true},
			mustHave: []string{"nav-home", "nav-about", "nav-giveaway", "nav-dashboard", "nav-admin-giveaway", "fn-logout"},
			mustNot:  []string{"nav-login", "nav-signup"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := reg.Search("", tt.ctx)
			ids := make(map[string]bool)
			for _, a := range results {
				ids[a.ID] = true
			}

			for _, id := range tt.mustHave {
				if !ids[id] {
					t.Errorf("expected action %q in results, but not found", id)
				}
			}
			for _, id := range tt.mustNot {
				if ids[id] {
					t.Errorf("action %q should not be in results for context %+v", id, tt.ctx)
				}
			}
		})
	}
}

func TestSearch_QueryFiltering(t *testing.T) {
	reg := New()

	tests := []struct {
		name    string
		query   string
		ctx     SearchContext
		wantIDs []string
	}{
		{
			name:    "search for home",
			query:   "home",
			ctx:     SearchContext{},
			wantIDs: []string{"nav-home"},
		},
		{
			name:    "search for giveaway matches free stuff",
			query:   "giveaway",
			ctx:     SearchContext{},
			wantIDs: []string{"nav-giveaway"},
		},
		{
			name:    "search for free matches giveaway via keywords",
			query:   "free",
			ctx:     SearchContext{},
			wantIDs: []string{"nav-giveaway"},
		},
		{
			name:    "case insensitive search",
			query:   "HOME",
			ctx:     SearchContext{},
			wantIDs: []string{"nav-home"},
		},
		{
			name:    "search for logout when logged in",
			query:   "logout",
			ctx:     SearchContext{LoggedIn: true},
			wantIDs: []string{"fn-logout"},
		},
		{
			name:    "search for logout when logged out returns nothing",
			query:   "logout",
			ctx:     SearchContext{},
			wantIDs: []string{},
		},
		{
			name:    "search for admin when not admin returns nothing",
			query:   "admin",
			ctx:     SearchContext{LoggedIn: true},
			wantIDs: []string{},
		},
		{
			name:    "search for admin when admin returns manage giveaways",
			query:   "admin",
			ctx:     SearchContext{LoggedIn: true, IsAdmin: true},
			wantIDs: []string{"nav-admin-giveaway"},
		},
		{
			name:    "no match returns empty",
			query:   "xyznonexistent",
			ctx:     SearchContext{},
			wantIDs: []string{},
		},
		{
			name:    "whitespace-only query returns all visible",
			query:   "   ",
			ctx:     SearchContext{},
			wantIDs: []string{"nav-home", "nav-about", "nav-giveaway", "nav-login", "nav-signup"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := reg.Search(tt.query, tt.ctx)
			ids := make(map[string]bool)
			for _, a := range results {
				ids[a.ID] = true
			}

			if len(tt.wantIDs) == 0 && len(results) != 0 {
				t.Errorf("expected no results, got %d: %v", len(results), resultIDs(results))
				return
			}

			for _, id := range tt.wantIDs {
				if !ids[id] {
					t.Errorf("expected action %q in results, got %v", id, resultIDs(results))
				}
			}

			if len(tt.wantIDs) > 0 && len(results) != len(tt.wantIDs) {
				// Only check exact count when we have specific expected IDs (not the whitespace test)
				if tt.query != "   " {
					t.Errorf("expected %d results, got %d: %v", len(tt.wantIDs), len(results), resultIDs(results))
				}
			}
		})
	}
}

func TestSearch_ActionFields(t *testing.T) {
	reg := New()
	results := reg.Search("home", SearchContext{})
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'home'")
	}

	home := results[0]
	if home.ID != "nav-home" {
		t.Errorf("id = %q, want nav-home", home.ID)
	}
	if home.Type != TypeNavigation {
		t.Errorf("type = %q, want navigation", home.Type)
	}
	if home.Title != "Home" {
		t.Errorf("title = %q, want Home", home.Title)
	}
	if home.Target != "/" {
		t.Errorf("target = %q, want /", home.Target)
	}
	if len(home.Keywords) == 0 {
		t.Error("expected non-empty keywords")
	}
}

func TestSearch_FunctionActionType(t *testing.T) {
	reg := New()
	results := reg.Search("logout", SearchContext{LoggedIn: true})
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'logout', got %d", len(results))
	}

	logout := results[0]
	if logout.Type != TypeFunction {
		t.Errorf("type = %q, want function", logout.Type)
	}
	if logout.Target != "logout" {
		t.Errorf("target = %q, want logout", logout.Target)
	}
}

func resultIDs(actions []Action) []string {
	ids := make([]string, len(actions))
	for i, a := range actions {
		ids[i] = a.ID
	}
	return ids
}

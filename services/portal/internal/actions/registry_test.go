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

func TestSearch_QueryFiltering(t *testing.T) {
	reg := New()

	tests := []struct {
		name    string
		query   string
		ctx     SearchContext
		wantIDs []string
	}{
		{
			name:    "title match",
			query:   "home",
			ctx:     SearchContext{},
			wantIDs: []string{"nav-home"},
		},
		{
			name:    "case insensitive",
			query:   "HOME",
			ctx:     SearchContext{},
			wantIDs: []string{"nav-home"},
		},
		{
			name:    "keyword match",
			query:   "landing",
			ctx:     SearchContext{},
			wantIDs: []string{"nav-home"},
		},
		{
			name:    "description match",
			query:   "hooper",
			ctx:     SearchContext{},
			wantIDs: []string{"nav-about"},
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
			wantIDs: []string{"nav-home", "nav-about", "nav-login", "nav-signup"},
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

func resultIDs(actions []Action) []string {
	ids := make([]string, len(actions))
	for i, a := range actions {
		ids[i] = a.ID
	}
	return ids
}

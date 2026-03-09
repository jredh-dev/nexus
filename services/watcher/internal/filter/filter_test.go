package filter

import "testing"

func TestIsVisible(t *testing.T) {
	tests := []struct {
		path    string
		visible bool
	}{
		// Always visible — FORM.md variants
		{"FORM.md", true},
		{"subdir/FORM.md", true},
		{"Agents.FORM.md", true},
		{"subdir/Topic.FORM.md", true},

		// All-uppercase stems
		{"PLAN.md", true},
		{"CONTEXT", true},
		{"GOALS.txt", true},
		{"PHILOSOPHY.md", true},

		// Lowercase .md = private
		{"notes.md", false},
		{"philosophy.md", false},
		{"readme.md", false},

		// Numeric-stem .md = private journal
		{"042.md", false},
		{"7.md", false},

		// _ and - prefix at any path level
		{"_scratch.md", false},
		{"-draft.md", false},
		{"subdir/_foo/PLAN.md", false},
		{"-archive/GOALS.md", false},

		// .1 suffix
		{"PLAN.md.1", false},
		{"notes.md.1", false},

		// Mixed case (not all-upper) = not visible
		{"Plan.md", false},
		{"plan.MD", false},
	}

	for _, tt := range tests {
		got := IsVisible(tt.path)
		if got != tt.visible {
			t.Errorf("IsVisible(%q) = %v, want %v", tt.path, got, tt.visible)
		}
	}
}

func TestIsFormFile(t *testing.T) {
	if !IsFormFile("FORM.md") {
		t.Error("expected FORM.md to be form file")
	}
	if !IsFormFile("subdir/deep/FORM.md") {
		t.Error("expected nested FORM.md to be form file")
	}
	if !IsFormFile("Agents.FORM.md") {
		t.Error("expected Agents.FORM.md to be form file")
	}
	if !IsFormFile("Topic.FORM.md") {
		t.Error("expected Topic.FORM.md to be form file")
	}
	if IsFormFile("form.md") {
		t.Error("expected lowercase form.md to NOT be form file")
	}
}

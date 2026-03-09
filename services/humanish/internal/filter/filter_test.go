package filter

import "testing"

func TestIsVisible(t *testing.T) {
	tests := []struct {
		path    string
		visible bool
	}{
		// Always visible — AGENTS.md variants
		{"AGENTS.md", true},
		{"subdir/AGENTS.md", true},
		{"Agents.AGENTS.md", true},
		{"subdir/Topic.AGENTS.md", true},

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

func TestIsAgentsFile(t *testing.T) {
	if !IsAgentsFile("AGENTS.md") {
		t.Error("expected AGENTS.md to be agents file")
	}
	if !IsAgentsFile("subdir/deep/AGENTS.md") {
		t.Error("expected nested AGENTS.md to be agents file")
	}
	if !IsAgentsFile("Agents.AGENTS.md") {
		t.Error("expected Agents.AGENTS.md to be agents file")
	}
	if !IsAgentsFile("Topic.AGENTS.md") {
		t.Error("expected Topic.AGENTS.md to be agents file")
	}
	if IsAgentsFile("agents.md") {
		t.Error("expected lowercase agents.md to NOT be agents file")
	}
}

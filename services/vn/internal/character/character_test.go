package character_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jredh-dev/nexus/services/vn/internal/character"
	"github.com/jredh-dev/nexus/services/vn/internal/state"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hero.yaml", `
id: hero
name: "The Hero"
description: "Brave and bold."
age: 30
role: protagonist
traits:
  - brave
  - stubborn
notes: "Has a scar on the left hand."
`)

	c, err := state.Load[character.Character](filepath.Join(dir, "hero.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if c.ID != "hero" {
		t.Errorf("ID = %q, want hero", c.ID)
	}
	if c.Name != "The Hero" {
		t.Errorf("Name = %q, want The Hero", c.Name)
	}
	if c.Age != 30 {
		t.Errorf("Age = %d, want 30", c.Age)
	}
	if c.Role != "protagonist" {
		t.Errorf("Role = %q, want protagonist", c.Role)
	}
	if len(c.Traits) != 2 {
		t.Errorf("Traits len = %d, want 2", len(c.Traits))
	}
	if c.Notes != "Has a scar on the left hand." {
		t.Errorf("Notes = %q", c.Notes)
	}
}

func TestLoad_MissingID(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.yaml", `
name: "No ID"
`)
	_, err := state.Load[character.Character](filepath.Join(dir, "bad.yaml"))
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestLoad_MissingName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.yaml", `
id: nameless
`)
	_, err := state.Load[character.Character](filepath.Join(dir, "bad.yaml"))
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", `
id: alpha
name: "Alpha"
role: leader
`)
	writeFile(t, dir, "b.yaml", `
id: beta
name: "Beta"
traits:
  - loyal
`)

	chars, err := state.LoadDir[character.Character](dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	if len(chars) != 2 {
		t.Fatalf("got %d characters, want 2", len(chars))
	}
	if chars["alpha"] == nil {
		t.Error("alpha not found")
	}
	if chars["beta"] == nil {
		t.Error("beta not found")
	}
	if len(chars["beta"].Traits) != 1 {
		t.Errorf("beta traits = %v, want [loyal]", chars["beta"].Traits)
	}
}

func TestLoadDir_DuplicateID(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", `
id: same
name: "First"
`)
	writeFile(t, dir, "b.yaml", `
id: same
name: "Second"
`)

	_, err := state.LoadDir[character.Character](dir)
	if err == nil {
		t.Fatal("expected error for duplicate id")
	}
}

func TestLoadDir_Empty(t *testing.T) {
	dir := t.TempDir()
	_, err := state.LoadDir[character.Character](dir)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}

func TestLoadDir_IgnoresNonYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "readme.md", "# not yaml")
	writeFile(t, dir, "char.yaml", `
id: only
name: "Only One"
`)

	chars, err := state.LoadDir[character.Character](dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(chars) != 1 {
		t.Errorf("got %d characters, want 1", len(chars))
	}
}

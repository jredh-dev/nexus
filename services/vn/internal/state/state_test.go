package state

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// testState is a minimal State implementation for testing the generic loader.
type testState struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	Val  int    `yaml:"val,omitempty"`
}

func (s testState) StateID() string { return s.ID }

func (s testState) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("id is required")
	}
	if s.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "item.yaml", `
id: sword
name: "Iron Sword"
val: 42
`)
	s, err := Load[testState](filepath.Join(dir, "item.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.ID != "sword" {
		t.Errorf("ID = %q, want sword", s.ID)
	}
	if s.Name != "Iron Sword" {
		t.Errorf("Name = %q", s.Name)
	}
	if s.Val != 42 {
		t.Errorf("Val = %d, want 42", s.Val)
	}
}

func TestLoad_ValidationError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.yaml", `
name: "No ID"
`)
	_, err := Load[testState](filepath.Join(dir, "bad.yaml"))
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load[testState]("/nonexistent/path.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.yaml", `
id: [broken
`)
	_, err := Load[testState](filepath.Join(dir, "bad.yaml"))
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", `
id: alpha
name: "Alpha"
val: 1
`)
	writeFile(t, dir, "b.yml", `
id: beta
name: "Beta"
val: 2
`)

	result, err := LoadDir[testState](dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d, want 2", len(result))
	}
	if result["alpha"].Val != 1 {
		t.Errorf("alpha val = %d", result["alpha"].Val)
	}
	if result["beta"].Val != 2 {
		t.Errorf("beta val = %d", result["beta"].Val)
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
	_, err := LoadDir[testState](dir)
	if err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestLoadDir_Empty(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadDir[testState](dir)
	if err == nil {
		t.Fatal("expected error for empty dir")
	}
}

func TestLoadDir_IgnoresNonYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "readme.md", "# nope")
	writeFile(t, dir, "x.yaml", `
id: only
name: "Only"
`)
	result, err := LoadDir[testState](dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("got %d, want 1", len(result))
	}
}

func TestLoadDir_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "x.yaml", `
id: root
name: "Root"
`)
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := LoadDir[testState](dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("got %d, want 1", len(result))
	}
}

func TestLoadDir_ValidationErrorInFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "good.yaml", `
id: ok
name: "Good"
`)
	writeFile(t, dir, "bad.yaml", `
id: broken
`)
	_, err := LoadDir[testState](dir)
	if err == nil {
		t.Fatal("expected validation error from bad file")
	}
}

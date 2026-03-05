package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- Entry.ID ---

func TestEntryID(t *testing.T) {
	e := Entry{"id": "hero", "name": "The Hero"}
	if got := e.ID(); got != "hero" {
		t.Errorf("ID() = %q, want hero", got)
	}
}

func TestEntryID_Missing(t *testing.T) {
	e := Entry{"name": "No ID"}
	if got := e.ID(); got != "" {
		t.Errorf("ID() = %q, want empty", got)
	}
}

func TestEntryID_NonString(t *testing.T) {
	e := Entry{"id": 42}
	if got := e.ID(); got != "" {
		t.Errorf("ID() = %q, want empty for non-string id", got)
	}
}

// --- Save ---

func TestSave(t *testing.T) {
	dir := t.TempDir()
	e := Entry{"id": "sword", "damage": 10, "name": "Iron Sword"}

	if err := Save(dir, e); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file was created with correct name.
	path := filepath.Join(dir, "sword.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}

	// Verify it round-trips back through Load.
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if loaded.ID() != "sword" {
		t.Errorf("loaded ID = %q, want sword", loaded.ID())
	}
}

func TestSave_NoID(t *testing.T) {
	dir := t.TempDir()
	e := Entry{"name": "Missing ID"}
	if err := Save(dir, e); err == nil {
		t.Fatal("expected error for entry with no id")
	}
}

func TestSave_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deep")
	e := Entry{"id": "item"}
	if err := Save(dir, e); err != nil {
		t.Fatalf("Save to nested dir: %v", err)
	}
}

// --- Load ---

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hero.yaml", `
id: hero
name: "The Hero"
age: 30
traits:
  - brave
  - stubborn
`)

	e, err := Load(filepath.Join(dir, "hero.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if e.ID() != "hero" {
		t.Errorf("ID = %q, want hero", e.ID())
	}
	if e["name"] != "The Hero" {
		t.Errorf("name = %v", e["name"])
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.yaml", `id: [broken`)
	_, err := Load(filepath.Join(dir, "bad.yaml"))
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoad_NoID(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "noid.yaml", `name: "No ID"`)
	_, err := Load(filepath.Join(dir, "noid.yaml"))
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

// --- LoadDir ---

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", `
id: alpha
name: "Alpha"
`)
	writeFile(t, dir, "b.yml", `
id: beta
name: "Beta"
`)

	result, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d entries, want 2", len(result))
	}
	if result["alpha"].ID() != "alpha" {
		t.Error("alpha not found")
	}
	if result["beta"].ID() != "beta" {
		t.Error("beta not found")
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
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestLoadDir_Empty(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadDir(dir)
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
	result, err := LoadDir(dir)
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
	result, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("got %d, want 1", len(result))
	}
}

// --- FromJSON / ToJSON ---

func TestFromJSON(t *testing.T) {
	raw := `{"id":"npc-01","name":"Guard","hp":100}`
	e, err := FromJSON([]byte(raw))
	if err != nil {
		t.Fatalf("FromJSON: %v", err)
	}
	if e.ID() != "npc-01" {
		t.Errorf("ID = %q", e.ID())
	}
}

func TestFromJSON_NoID(t *testing.T) {
	raw := `{"name":"No ID"}`
	_, err := FromJSON([]byte(raw))
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestFromJSON_InvalidJSON(t *testing.T) {
	_, err := FromJSON([]byte(`{broken`))
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestToJSON(t *testing.T) {
	e := Entry{"id": "item", "count": 5}
	data, err := ToJSON(e)
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}

	// Verify it's valid JSON that round-trips.
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("ToJSON produced invalid JSON: %v", err)
	}
	if parsed["id"] != "item" {
		t.Errorf("id = %v", parsed["id"])
	}
}

// --- Round-trip: JSON → YAML → JSON ---

func TestRoundTrip_JSONtoYAMLtoJSON(t *testing.T) {
	// Simulate the full API flow: receive JSON, save as YAML, load back, serve as JSON.
	dir := t.TempDir()
	input := `{"id":"char-42","name":"The Fool","age":22,"traits":["curious","reckless"]}`

	// Ingest from JSON.
	e, err := FromJSON([]byte(input))
	if err != nil {
		t.Fatalf("FromJSON: %v", err)
	}

	// Persist as YAML.
	if err := Save(dir, e); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load back from YAML.
	loaded, err := Load(filepath.Join(dir, "char-42.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Serve as JSON.
	out, err := ToJSON(loaded)
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}

	// Verify key fields survived the round-trip.
	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("final JSON parse: %v", err)
	}
	if result["id"] != "char-42" {
		t.Errorf("id = %v", result["id"])
	}
	if result["name"] != "The Fool" {
		t.Errorf("name = %v", result["name"])
	}
}

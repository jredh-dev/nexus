// Package state provides schema-free YAML persistence for versioned
// entity state. Any JSON payload with an "id" field can be saved as
// YAML and read back as JSON-serializable data.
//
// The package is intentionally untyped — it stores arbitrary key-value
// maps. Concrete entity schemas (characters, items, locations, etc.)
// are defined by the creative content, not the engine. The engine just
// preserves whatever state it receives.
//
// Flow:
//
//	API (JSON in) → Save (YAML on disk) → Load (YAML from disk) → API (JSON out)
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Entry is a schema-free state record. The only required field is "id",
// used as the map key and filename stem. All other fields are preserved
// as-is through the JSON→YAML→JSON round-trip.
type Entry map[string]interface{}

// ID returns the entry's "id" field, or "" if missing/non-string.
func (e Entry) ID() string {
	v, ok := e["id"]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// Save writes an Entry to disk as YAML. The file is named <id>.yaml
// inside dir. Returns an error if the entry has no id or the write fails.
func Save(dir string, e Entry) error {
	id := e.ID()
	if id == "" {
		return fmt.Errorf("state: entry has no id field")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("state: mkdir %s: %w", dir, err)
	}

	data, err := yaml.Marshal(e)
	if err != nil {
		return fmt.Errorf("state: marshal %s: %w", id, err)
	}

	path := filepath.Join(dir, id+".yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("state: write %s: %w", path, err)
	}

	return nil
}

// Load reads a single YAML file and returns it as an Entry.
// Returns an error if the file can't be read, parsed, or has no id.
func Load(path string) (Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("state: read %s: %w", path, err)
	}

	var e Entry
	if err := yaml.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("state: parse %s: %w", path, err)
	}

	if e.ID() == "" {
		return nil, fmt.Errorf("state: %s has no id field", path)
	}

	return e, nil
}

// LoadDir reads all .yaml/.yml files in a directory and returns them
// keyed by id. Returns an error on duplicate ids, parse failures, or
// if the directory is empty of YAML files.
func LoadDir(dir string) (map[string]Entry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("state: read dir %s: %w", dir, err)
	}

	result := make(map[string]Entry)
	for _, f := range entries {
		if f.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		e, err := Load(filepath.Join(dir, f.Name()))
		if err != nil {
			return nil, err
		}

		id := e.ID()
		if _, exists := result[id]; exists {
			return nil, fmt.Errorf("state: duplicate id %q in %s", id, f.Name())
		}
		result[id] = e
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("state: no YAML files in %s", dir)
	}

	return result, nil
}

// FromJSON converts a JSON byte slice into an Entry. This is the
// ingestion path: API handlers call FromJSON on the request body,
// then Save to persist as YAML.
func FromJSON(data []byte) (Entry, error) {
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("state: parse json: %w", err)
	}
	if e.ID() == "" {
		return nil, fmt.Errorf("state: json has no id field")
	}
	return e, nil
}

// ToJSON converts an Entry to a JSON byte slice. This is the serving
// path: handlers call ToJSON on loaded entries to write HTTP responses.
func ToJSON(e Entry) ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("state: marshal json: %w", err)
	}
	return data, nil
}

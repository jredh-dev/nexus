// Package character loads and manages YAML character definitions.
//
// Characters are simple versioned structs — one YAML file per character,
// stored in a directory and optionally git-tracked via storyrepo. The
// struct is intentionally minimal; it will grow as the creative model
// solidifies.
package character

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Character is the core definition loaded from YAML.
type Character struct {
	ID          string   `yaml:"id" json:"id"`
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Age         int      `yaml:"age,omitempty" json:"age,omitempty"`
	Role        string   `yaml:"role,omitempty" json:"role,omitempty"`
	Traits      []string `yaml:"traits,omitempty" json:"traits,omitempty"`
	Notes       string   `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// Load reads a single character YAML file.
func Load(path string) (*Character, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read character %s: %w", path, err)
	}

	var c Character
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse character %s: %w", path, err)
	}

	if c.ID == "" {
		return nil, fmt.Errorf("character in %s: id is required", path)
	}
	if c.Name == "" {
		return nil, fmt.Errorf("character %q in %s: name is required", c.ID, path)
	}

	return &c, nil
}

// LoadDir reads all .yaml/.yml files in a directory and returns characters
// keyed by ID. Returns an error if any file fails to parse or if duplicate
// IDs are found.
func LoadDir(dir string) (map[string]*Character, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read character dir %s: %w", dir, err)
	}

	characters := make(map[string]*Character)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		c, err := Load(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}

		if _, exists := characters[c.ID]; exists {
			return nil, fmt.Errorf("duplicate character id %q in %s", c.ID, e.Name())
		}
		characters[c.ID] = c
	}

	if len(characters) == 0 {
		return nil, fmt.Errorf("no character YAML files in %s", dir)
	}

	return characters, nil
}

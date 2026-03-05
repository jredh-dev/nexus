// Package state provides a generic YAML loader for versioned state
// definitions. Any type that implements the State interface can be
// loaded from YAML files — characters, items, locations, factions, etc.
//
// The loader handles file I/O, YAML deserialization, validation, and
// directory scanning. Consumers define their own structs and implement
// StateID() and Validate(). The loader does the rest.
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// State is the interface that YAML-backed state types must implement.
// StateID returns a unique identifier (used as the map key when loading
// a directory). Validate checks structural invariants after deserialization.
type State interface {
	StateID() string
	Validate() error
}

// Load reads a single YAML file and deserializes it into T. T must be a
// struct type implementing State. Returns an error if the file can't be
// read, parsed, or fails validation.
func Load[T State](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var v T
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if err := v.Validate(); err != nil {
		return nil, fmt.Errorf("validate %s: %w", path, err)
	}

	return &v, nil
}

// LoadDir reads all .yaml/.yml files in a directory and returns the
// deserialized values keyed by StateID(). Returns an error if any file
// fails to load, if duplicate IDs are found, or if the directory has
// no YAML files.
func LoadDir[T State](dir string) (map[string]*T, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	result := make(map[string]*T)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		v, err := Load[T](filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}

		id := (*v).StateID()
		if _, exists := result[id]; exists {
			return nil, fmt.Errorf("duplicate state id %q in %s", id, e.Name())
		}
		result[id] = v
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no YAML files in %s", dir)
	}

	return result, nil
}

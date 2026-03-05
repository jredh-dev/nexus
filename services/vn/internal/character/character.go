// Package character defines the Character type for YAML-backed state.
//
// Character implements state.State, so it can be loaded via the generic
// state.Load and state.LoadDir functions. The struct is intentionally
// minimal; it will grow as the creative model solidifies.
package character

import "fmt"

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

// StateID returns the character's unique identifier.
func (c Character) StateID() string { return c.ID }

// Validate checks that required fields are present.
func (c Character) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("character: id is required")
	}
	if c.Name == "" {
		return fmt.Errorf("character %q: name is required", c.ID)
	}
	return nil
}

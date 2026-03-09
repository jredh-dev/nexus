// Package state manages .wstr.json workstream state at the worktree root.
// It stores enough context to support wstr commit and wstr end without
// any external database or registry server.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const stateFile = ".wstr.json"

// Workstream holds the local state for an active workstream.
type Workstream struct {
	// ID is the full branch/worktree name (e.g. workstream_task-slug-abc12345).
	ID string `json:"id"`
	// Task is the human-readable task description.
	Task string `json:"task"`
	// Repo is "owner/repo" format.
	Repo string `json:"repo"`
	// Branch is the git branch name (same as ID).
	Branch string `json:"branch"`
	// ParentBranch is the base branch (usually "main").
	ParentBranch string `json:"parent_branch"`
	// WorktreePath is the absolute path to this worktree.
	WorktreePath string `json:"worktree_path"`
	// CreatedAt is the creation timestamp (RFC3339).
	CreatedAt string `json:"created_at"`
}

// Save writes the workstream state to .wstr.json in dir.
func Save(dir string, ws *Workstream) error {
	if ws.CreatedAt == "" {
		ws.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	path := filepath.Join(dir, stateFile)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Load reads .wstr.json from dir.
// Returns ErrNotFound if the file does not exist (not in a workstream worktree).
func Load(dir string) (*Workstream, error) {
	path := filepath.Join(dir, stateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var ws Workstream
	if err := json.Unmarshal(data, &ws); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &ws, nil
}

// Remove deletes .wstr.json from dir.
func Remove(dir string) error {
	path := filepath.Join(dir, stateFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// ErrNotFound is returned when .wstr.json does not exist in the directory.
var ErrNotFound = fmt.Errorf("not in a workstream worktree (.wstr.json not found)")

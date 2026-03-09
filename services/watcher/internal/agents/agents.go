// Package agents reads and parses the FORM.md hierarchy in the humanish
// volume. The hierarchy follows these precedence rules:
//
//   - Top-level FORM.md is "scripture" — always ingested first.
//   - Nested FORM.md files are more specific; the deepest one takes precedence.
//   - If questions remain after reading the deepest, walk up toward the root.
//
// This package provides helpers to:
//   - Walk the directory tree and collect all FORM.md paths (root → leaf order).
//   - Read their contents into a merged context string for prompt injection.
//   - Update a specific FORM.md file (used after OpenCode responds).
package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AgentsFile holds the path and content of one FORM.md file.
type AgentsFile struct {
	// Path is the absolute filesystem path.
	Path string
	// Rel is the path relative to the volume root.
	Rel string
	// Content is the raw text content.
	Content string
}

// Collect walks the humanish volume rooted at base and returns all FORM.md
// files ordered from root to deepest (root first = lowest precedence).
func Collect(base string) ([]AgentsFile, error) {
	var files []AgentsFile

	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != "FORM.md" && !strings.HasSuffix(d.Name(), ".FORM.md") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil // non-fatal
		}

		rel, _ := filepath.Rel(base, path)
		files = append(files, AgentsFile{
			Path:    path,
			Rel:     rel,
			Content: string(data),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("agents walk: %w", err)
	}

	// Sort: root first (shortest path first).
	sortByDepth(files)
	return files, nil
}

// MergedContext returns a single string suitable for prepending to an OpenCode
// prompt. It concatenates all FORM.md files from root to deepest, with a
// header indicating path and precedence.
func MergedContext(files []AgentsFile) string {
	if len(files) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("# Agent Instructions (FORM.md hierarchy)\n\n")
	sb.WriteString("Instructions are listed from most general (root) to most specific (deepest).\n")
	sb.WriteString("The deepest file takes precedence when there is a conflict.\n\n")
	sb.WriteString("---\n\n")

	for i, f := range files {
		depth := "root"
		if i > 0 {
			depth = fmt.Sprintf("depth %d", i)
		}
		sb.WriteString(fmt.Sprintf("## %s (%s)\n\n", f.Rel, depth))
		sb.WriteString(f.Content)
		sb.WriteString("\n\n---\n\n")
	}
	return sb.String()
}

// Update writes new content to the AGENTS.md at the given absolute path.
// It creates parent directories if needed.
func Update(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("agents update mkdir: %w", err)
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// RootPath returns the path of the root FORM.md for a given volume base.
func RootPath(base string) string {
	return filepath.Join(base, "FORM.md")
}

// sortByDepth sorts AgentsFile slice by path depth (fewer slashes first).
func sortByDepth(files []AgentsFile) {
	// Simple insertion sort — list is typically very short.
	for i := 1; i < len(files); i++ {
		for j := i; j > 0 && depth(files[j].Rel) < depth(files[j-1].Rel); j-- {
			files[j], files[j-1] = files[j-1], files[j]
		}
	}
}

func depth(rel string) int {
	return strings.Count(filepath.ToSlash(rel), "/")
}

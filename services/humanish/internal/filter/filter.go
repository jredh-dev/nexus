// Package filter determines which files in the humanish volume are "visible"
// to the agent and which should be ignored.
//
// Visibility rules (applied in order, first match wins):
//   - Ignore: any path component starts with '_' or '-'
//   - Ignore: filename ends with '.1'
//   - Ignore: basename is all lowercase, ends in '.md', and the stem is purely numeric
//     (e.g. "042.md", "7.md") — these are private journal/hash files
//   - Ignore: basename starts with a lowercase letter and ends in '.md' when the file
//     is NOT named AGENTS.md — lowercase-named .md files are private notes
//   - Visible: AGENTS.md at any depth (always read, even if other rules would hide it)
//   - Visible: all-uppercase basename (no extension) — e.g. "CONTEXT", "PLAN"
//   - Visible: all-uppercase basename + any extension — e.g. "CONTEXT.md", "PLAN.txt"
//   - Ignore: everything else
package filter

import (
	"path/filepath"
	"strings"
	"unicode"
)

// IsVisible returns true if the file at the given path should be
// considered by the agent. path may be absolute or relative.
func IsVisible(path string) bool {
	// Normalise to forward-slash components.
	path = filepath.ToSlash(path)
	parts := strings.Split(path, "/")

	for _, part := range parts {
		if part == "" {
			continue
		}
		// Any path component starting with '_' or '-' → ignore.
		if strings.HasPrefix(part, "_") || strings.HasPrefix(part, "-") {
			return false
		}
	}

	base := filepath.Base(path)

	// Files ending with '.1' are ignored (e.g. Vim swap backup style).
	if strings.HasSuffix(base, ".1") {
		return false
	}

	// AGENTS.md (and *.AGENTS.md variants) are always visible regardless of other rules.
	if IsAgentsFile(base) {
		return true
	}

	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	// Lowercase .md files where the stem is purely numeric → private journal.
	if strings.ToLower(ext) == ".md" && isNumeric(stem) {
		return false
	}

	// Lowercase .md files (private notes) are invisible.
	if strings.ToLower(ext) == ".md" && len(base) > 0 && unicode.IsLower(rune(base[0])) {
		return false
	}

	// All-uppercase stem → visible (PLAN, CONTEXT, GOALS, etc.)
	if isUppercase(stem) {
		return true
	}

	return false
}

// IsAgentsFile returns true if path is an AGENTS.md file at any depth.
// This includes files named exactly "AGENTS.md" and files whose basename
// ends with ".AGENTS.md" (e.g. "Agents.AGENTS.md") — these are sub-instruction
// files that the user may name with a qualifying prefix.
func IsAgentsFile(path string) bool {
	base := filepath.Base(filepath.ToSlash(path))
	return base == "AGENTS.md" || strings.HasSuffix(base, ".AGENTS.md")
}

// isUppercase returns true if every rune in s is an uppercase ASCII letter
// or an ASCII digit or underscore (i.e. the basename looks like a constant).
func isUppercase(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) && !unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

// isNumeric returns true if s consists entirely of ASCII digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Copyright (C) 2026 jredh-dev. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// This file is part of nexus, licensed under the GNU Affero General Public
// License v3.0 or later. See LICENSE for details.

// Package storyrepo manages a local git repository for YAML story files,
// providing version control for creative narrative content.
//
// Design decisions:
//   - Single branch (main), overwrite semantics. This is content, not code.
//     Force-push / history rewrite is expected.
//   - Uses the git CLI (exec) rather than go-git. Simpler, fewer deps, and
//     the git binary is always available on the host.
//   - Sets GIT_DIR and GIT_WORK_TREE env vars per command to avoid ambient
//     git state leaking in from the host environment.
//   - All commits use a fixed author ("vn-engine <vn@nexus.local>") so that
//     story version history is clearly machine-attributed.
package storyrepo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// gitBinary is the path to the git executable. Extracted as a package-level
// var so tests can override it if needed (though in practice /usr/local/bin/git
// is always present).
var gitBinary = "/usr/local/bin/git"

// commitAuthor is the fixed author identity used for all storyrepo commits.
// Using a fixed identity keeps the version history clearly separated from
// developer commits.
const commitAuthor = "vn-engine <vn@nexus.local>"

// Repo manages a git repository of YAML story files. All operations shell
// out to the git CLI with explicit GIT_DIR and GIT_WORK_TREE to ensure
// isolation from any ambient git context.
type Repo struct {
	// path is the absolute path to the repository working directory.
	path string
}

// CommitInfo holds metadata about a single commit. Parsed from git log
// output using a custom format string.
type CommitInfo struct {
	Hash      string    `json:"hash"`
	Message   string    `json:"message"`
	Author    string    `json:"author"`
	Timestamp time.Time `json:"timestamp"`
}

// Init creates a new git repo at the given path, or opens an existing one.
// If the directory doesn't exist, it creates it and runs git init.
// If the directory exists but is not a git repo, it initializes one.
// If the directory exists and is already a git repo, it just opens it.
func Init(path string) (*Repo, error) {
	// Resolve to absolute path to avoid ambiguity when setting GIT_WORK_TREE.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("storyrepo: resolve absolute path %q: %w", path, err)
	}

	// Create the directory if it doesn't exist.
	if err := os.MkdirAll(absPath, 0o755); err != nil {
		return nil, fmt.Errorf("storyrepo: create directory %q: %w", absPath, err)
	}

	r := &Repo{path: absPath}

	// Check if this is already a git repo by looking for .git.
	gitDir := filepath.Join(absPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		// Not a git repo yet — initialize one with an initial branch named "main".
		if _, err := r.git("init", "-b", "main"); err != nil {
			return nil, fmt.Errorf("storyrepo: git init at %q: %w", absPath, err)
		}

		// Configure the author for this repo so commits don't depend on
		// global git config (which may not exist in CI or agent environments).
		if _, err := r.git("config", "user.name", "vn-engine"); err != nil {
			return nil, fmt.Errorf("storyrepo: set git user.name: %w", err)
		}
		if _, err := r.git("config", "user.email", "vn@nexus.local"); err != nil {
			return nil, fmt.Errorf("storyrepo: set git user.email: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("storyrepo: stat .git at %q: %w", absPath, err)
	}

	return r, nil
}

// Path returns the repository working directory path.
func (r *Repo) Path() string {
	return r.path
}

// Commit stages all YAML files (*.yaml, *.yml) and commits with the given
// message. Returns the short commit hash. If there are no changes to commit,
// returns an error rather than creating an empty commit.
func (r *Repo) Commit(msg string) (string, error) {
	if msg == "" {
		return "", fmt.Errorf("storyrepo: commit message cannot be empty")
	}

	// Stage all YAML files. We use "git add" with glob patterns.
	// First, add any .yaml files. Ignore errors from "nothing to add" —
	// we'll catch "nothing to commit" below.
	_, _ = r.git("add", "--all", "*.yaml")
	_, _ = r.git("add", "--all", "*.yml")

	// Also stage deletions — git add --all handles this, but let's be
	// explicit about tracking removed YAML files too.
	_, _ = r.git("add", "-A")

	// Attempt the commit. If there's nothing staged, git commit will fail
	// with a non-zero exit code, which we propagate.
	out, err := r.git("commit", "-m", msg, "--author", commitAuthor)
	if err != nil {
		// Distinguish "nothing to commit" from real errors.
		if strings.Contains(out, "nothing to commit") ||
			strings.Contains(out, "no changes added") {
			return "", fmt.Errorf("storyrepo: nothing to commit")
		}
		return "", fmt.Errorf("storyrepo: git commit: %w\noutput: %s", err, out)
	}

	// Get the hash of the commit we just created.
	hash, err := r.CurrentHash()
	if err != nil {
		return "", fmt.Errorf("storyrepo: get hash after commit: %w", err)
	}

	return hash, nil
}

// Log returns the commit history (newest first). If limit is 0, all commits
// are returned. The output is parsed from git log with a custom format.
func (r *Repo) Log(limit int) ([]CommitInfo, error) {
	// Custom format: hash, author, unix timestamp, and subject — separated
	// by a delimiter that won't appear in normal commit messages.
	// Format: HASH<|>AUTHOR<|>UNIX_TIMESTAMP<|>MESSAGE
	const sep = "<|>"
	format := strings.Join([]string{"%H", "%an <%ae>", "%at", "%s"}, sep)

	args := []string{"log", "--format=" + format}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}

	out, err := r.git(args...)
	if err != nil {
		// "fatal: bad default revision 'HEAD'" means no commits yet.
		if strings.Contains(out, "bad default revision") ||
			strings.Contains(out, "does not have any commits") {
			return nil, nil // empty repo, no commits — not an error
		}
		return nil, fmt.Errorf("storyrepo: git log: %w\noutput: %s", err, out)
	}

	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}

	lines := strings.Split(out, "\n")
	commits := make([]CommitInfo, 0, len(lines))

	for _, line := range lines {
		parts := strings.SplitN(line, sep, 4)
		if len(parts) != 4 {
			// Skip malformed lines (shouldn't happen with our format).
			continue
		}

		ts, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			// If we can't parse the timestamp, use zero time.
			ts = 0
		}

		commits = append(commits, CommitInfo{
			Hash:      parts[0],
			Author:    parts[1],
			Timestamp: time.Unix(ts, 0),
			Message:   parts[3],
		})
	}

	return commits, nil
}

// Revert resets the repo to a previous commit hash. This is a hard reset —
// it overwrites history. After resetting, it creates a new commit recording
// the revert so the action itself is tracked in the log.
//
// The flow is:
//  1. git checkout <hash> -- .     (restore working tree to that commit's state)
//  2. git add -A                   (stage the restored state)
//  3. git commit                   (record the revert as a new commit)
//
// This approach preserves the linear history on main rather than using
// git reset --hard which would lose commits.
func (r *Repo) Revert(commitHash string) error {
	if commitHash == "" {
		return fmt.Errorf("storyrepo: commit hash cannot be empty")
	}

	// Verify the commit exists before attempting the revert.
	if _, err := r.git("cat-file", "-t", commitHash); err != nil {
		return fmt.Errorf("storyrepo: commit %q not found: %w", commitHash, err)
	}

	// Restore working tree to the target commit's state.
	// "git checkout <hash> -- ." restores all files without moving HEAD.
	if out, err := r.git("checkout", commitHash, "--", "."); err != nil {
		return fmt.Errorf("storyrepo: checkout %q: %w\noutput: %s", commitHash, err, out)
	}

	// Stage everything (including deletions of files that existed in HEAD
	// but not in the target commit).
	if out, err := r.git("add", "-A"); err != nil {
		return fmt.Errorf("storyrepo: stage after revert: %w\noutput: %s", err, out)
	}

	// Commit the revert. Use a descriptive message that includes the target hash.
	msg := fmt.Sprintf("revert to %s", commitHash[:minLen(len(commitHash), 8)])
	out, err := r.git("commit", "-m", msg, "--author", commitAuthor, "--allow-empty")
	if err != nil {
		// If there's nothing to commit, the state was already at that hash.
		if strings.Contains(out, "nothing to commit") {
			return nil // already at that state, not an error
		}
		return fmt.Errorf("storyrepo: commit revert: %w\noutput: %s", err, out)
	}

	return nil
}

// Diff returns the diff between two commits. If toHash is empty, diffs
// the fromHash against HEAD (showing what changed between fromHash and now).
// If both are empty, returns the diff of uncommitted changes.
func (r *Repo) Diff(fromHash, toHash string) (string, error) {
	args := []string{"diff"}

	switch {
	case fromHash != "" && toHash != "":
		args = append(args, fromHash, toHash)
	case fromHash != "" && toHash == "":
		args = append(args, fromHash, "HEAD")
	default:
		// Both empty: show uncommitted changes (working tree vs HEAD).
		args = append(args, "HEAD")
	}

	out, err := r.git(args...)
	if err != nil {
		return "", fmt.Errorf("storyrepo: git diff: %w\noutput: %s", err, out)
	}

	return out, nil
}

// CurrentHash returns the full SHA-1 hash of the HEAD commit.
// Returns an error if the repo has no commits.
func (r *Repo) CurrentHash() (string, error) {
	out, err := r.git("rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("storyrepo: get HEAD hash: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// git executes a git command with the repo's GIT_DIR and GIT_WORK_TREE set.
// Returns the combined stdout+stderr output and any error. Setting these
// env vars per-command ensures complete isolation from ambient git state —
// critical when running inside a worktree or nested repo.
func (r *Repo) git(args ...string) (string, error) {
	cmd := exec.Command(gitBinary, args...)

	// Set env vars to isolate this git invocation from any ambient repo.
	// GIT_DIR points to the .git directory; GIT_WORK_TREE points to the
	// working directory. Together they fully scope git to our story repo.
	cmd.Env = append(os.Environ(),
		"GIT_DIR="+filepath.Join(r.path, ".git"),
		"GIT_WORK_TREE="+r.path,
	)
	cmd.Dir = r.path

	out, err := cmd.CombinedOutput()
	return string(out), err
}

// minLen returns the smaller of a and b. Used for safe substring slicing
// on commit hashes that might be shorter than expected.
func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}

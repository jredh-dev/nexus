// Package workstream manages git worktrees and branches for wstr workstreams.
package workstream

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jredh-dev/nexus/cmd/wstr/internal/state"
)

// Create creates a new git worktree + branch for the given task and repo.
// The worktree is created at $WORK/work/<id>/<repoName>/ where $WORK defaults to the
// parent of the source repo directory (two levels up from the git root).
//
// Returns the created Workstream state and the absolute path to the worktree.
func Create(task, repo, parentBranch string) (*state.Workstream, error) {
	if task == "" {
		return nil, fmt.Errorf("task description is required")
	}
	if repo == "" {
		return nil, fmt.Errorf("repo (owner/repo) is required")
	}
	if parentBranch == "" {
		parentBranch = "main"
	}

	// Derive the repo name from "owner/repo".
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format %q: expected owner/repo", repo)
	}
	repoName := parts[1]

	// Locate the source repo root (must already be a git repo).
	sourceRoot, err := gitRoot(".")
	if err != nil {
		return nil, fmt.Errorf("not inside a git repository: %w", err)
	}

	// Ensure the source repo is on the parent branch and up to date.
	if err := runGit(sourceRoot, "fetch", "origin"); err != nil {
		fmt.Printf("[warn] git fetch failed: %v\n", err)
	}
	if err := runGit(sourceRoot, "checkout", parentBranch); err != nil {
		return nil, fmt.Errorf("checkout %s: %w", parentBranch, err)
	}
	if err := runGit(sourceRoot, "pull", "--ff-only", "origin", parentBranch); err != nil {
		fmt.Printf("[warn] git pull failed: %v\n", err)
	}

	// Generate workstream ID: "workstream_<slug>-<8-char-uuid>"
	id := buildID(task)
	branch := id

	// Compute worktree path: two levels up from source root, then work/<id>/<repoName>/
	workRoot := filepath.Join(filepath.Dir(filepath.Dir(sourceRoot)), "work")
	worktreePath := filepath.Join(workRoot, id, repoName)

	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		return nil, fmt.Errorf("create worktree dir: %w", err)
	}

	// Create the worktree + branch.
	if err := runGit(sourceRoot, "worktree", "add", "-b", branch, worktreePath, parentBranch); err != nil {
		return nil, fmt.Errorf("git worktree add: %w", err)
	}

	ws := &state.Workstream{
		ID:           id,
		Task:         task,
		Repo:         repo,
		Branch:       branch,
		ParentBranch: parentBranch,
		WorktreePath: worktreePath,
	}

	// Write .wstr.json into the new worktree.
	if err := state.Save(worktreePath, ws); err != nil {
		return nil, fmt.Errorf("save state: %w", err)
	}

	return ws, nil
}

// Commit stages all changes in dir and creates a commit with msg.
// If msg is empty, a default message using the workstream task is used.
func Commit(dir, msg string) error {
	ws, err := state.Load(dir)
	if err != nil {
		return err
	}

	if msg == "" {
		msg = "wip: " + ws.Task
	}

	// Stage all changes (including .wstr.json).
	if err := runGit(dir, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// Check if there's anything to commit.
	out, _ := captureGit(dir, "status", "--porcelain")
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("nothing to commit (working tree clean)")
	}

	if err := runGit(dir, "commit", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// End removes the worktree. If push is true, the branch is pushed to origin first.
// The .wstr.json file is removed before cleanup.
func End(dir string, push bool) (*state.Workstream, error) {
	ws, err := state.Load(dir)
	if err != nil {
		return nil, err
	}

	// Push to origin before removing.
	if push {
		if err := runGit(dir, "push", "-u", "origin", ws.Branch); err != nil {
			return nil, fmt.Errorf("git push: %w", err)
		}
	}

	// Remove .wstr.json.
	_ = state.Remove(dir)

	// Locate source repo to prune the worktree from.
	sourceRoot := findSourceRoot(ws.WorktreePath, ws.Repo)
	if sourceRoot != "" {
		_ = runGit(sourceRoot, "worktree", "remove", "--force", ws.WorktreePath)
		_ = runGit(sourceRoot, "branch", "-d", ws.Branch)
	}

	return ws, nil
}

// ---- helpers ----

// buildID generates "workstream_<slug>-<8-char-uuid>" from a task description.
func buildID(task string) string {
	slug := slugify(task)
	if len(slug) > 50 {
		slug = slug[:50]
	}
	// Strip trailing dashes from truncation.
	slug = strings.TrimRight(slug, "-")

	id := uuid.New().String()
	short := strings.ReplaceAll(id, "-", "")[:8]
	return fmt.Sprintf("workstream_%s-%s", slug, short)
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a task description to a URL-safe slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = nonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// gitRoot returns the absolute path of the git root for the given directory.
func gitRoot(dir string) (string, error) {
	out, err := captureGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// runGit runs a git command in dir, printing output on failure.
func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out.String())
	}
	return nil
}

// captureGit runs a git command and returns stdout as a string.
func captureGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// findSourceRoot attempts to locate the source repo root by walking up from the
// worktree path past the workstream dir.
// Heuristic: the worktree is at <work>/<id>/<repoName>/ and the source is at
// <work>/source/<owner>/<repoName>/ (or just the main git repo).
func findSourceRoot(worktreePath, repo string) string {
	// Walk up to the work/ directory.
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	owner, repoName := parts[0], parts[1]

	// worktreePath = .../work/<id>/<repoName>
	// work dir     = .../work
	workDir := filepath.Dir(filepath.Dir(worktreePath))
	candidate := filepath.Join(workDir, "source", owner, repoName)
	if _, err := os.Stat(filepath.Join(candidate, ".git")); err == nil {
		return candidate
	}
	return ""
}

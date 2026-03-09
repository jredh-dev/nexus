// Package shadow manages a shadow git repository overlaid on the humanish
// volume. The shadow repo lives at <dir>/.humanish-git and is separate from
// any user-facing git history. All operations use the git CLI so there are
// no extra library dependencies.
//
// Workflow:
//  1. Init() ensures the repo exists and has a first commit.
//  2. Stage() adds all visible changed files to the shadow index.
//  3. Diff() returns a unified diff of staged vs HEAD.
//  4. Commit(msg) records a snapshot with the given message.
//  5. NoteAppend(ref, note) appends verbose hidden text to a git note on ref.
package shadow

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const shadowDir = ".humanish-git"

// Repo wraps a shadow git repository rooted at <base>/.humanish-git.
type Repo struct {
	base    string // the humanish volume root (bind-mounted host directory)
	gitDir  string // absolute path to .humanish-git
	workDir string // same as base — work-tree is the volume root
}

// New returns a Repo for the given base directory.
func New(base string) *Repo {
	return &Repo{
		base:    base,
		gitDir:  filepath.Join(base, shadowDir),
		workDir: base,
	}
}

// Init initialises the shadow git repo if it does not already exist.
// On first run it creates an initial empty commit so that diffs work.
func (r *Repo) Init() error {
	if _, err := os.Stat(filepath.Join(r.gitDir, "HEAD")); err == nil {
		// Already initialised.
		return nil
	}

	// git init --bare-ish: use separate-git-dir so the work-tree stays clean.
	if err := r.run("git", "init", "--separate-git-dir="+r.gitDir, r.workDir); err != nil {
		return fmt.Errorf("shadow git init: %w", err)
	}

	// Configure identity so commits work without a global gitconfig.
	if err := r.git("config", "user.email", "humanish@local"); err != nil {
		return err
	}
	if err := r.git("config", "user.name", "humanish"); err != nil {
		return err
	}

	// Initial empty commit so HEAD is valid and diffs work.
	if err := r.git("commit", "--allow-empty", "-m", "chore: initial shadow commit"); err != nil {
		return err
	}

	return nil
}

// Stage adds all currently-visible files to the shadow index using the
// provided list of absolute paths. Paths outside base are silently skipped.
func (r *Repo) Stage(paths []string) error {
	// Build relative paths.
	var rel []string
	for _, p := range paths {
		rp, err := filepath.Rel(r.workDir, p)
		if err != nil || strings.HasPrefix(rp, "..") {
			continue
		}
		rel = append(rel, rp)
	}
	if len(rel) == 0 {
		return nil
	}
	args := append([]string{"add", "--"}, rel...)
	return r.git(args...)
}

// StageAll stages all tracked and untracked visible files.
// This is a convenience wrapper used on first sync.
func (r *Repo) StageAll() error {
	return r.git("add", "-A")
}

// Diff returns a unified diff of the staged changes vs HEAD.
// Returns an empty string if there is nothing staged.
func (r *Repo) Diff() (string, error) {
	var out bytes.Buffer
	cmd := r.makeCmd("git", "diff", "--cached", "--stat")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("shadow git diff stat: %w", err)
	}
	if strings.TrimSpace(out.String()) == "" {
		// Nothing staged.
		return "", nil
	}

	out.Reset()
	cmd = r.makeCmd("git", "diff", "--cached")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("shadow git diff: %w", err)
	}
	return out.String(), nil
}

// HasStagedChanges returns true if the index differs from HEAD.
func (r *Repo) HasStagedChanges() (bool, error) {
	var out bytes.Buffer
	cmd := r.makeCmd("git", "diff", "--cached", "--name-only")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return false, err
	}
	return strings.TrimSpace(out.String()) != "", nil
}

// Commit records the staged snapshot with the given message.
func (r *Repo) Commit(msg string) (string, error) {
	if err := r.git("commit", "-m", msg); err != nil {
		return "", fmt.Errorf("shadow commit: %w", err)
	}
	// Return the new HEAD SHA.
	var out bytes.Buffer
	cmd := r.makeCmd("git", "rev-parse", "HEAD")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// NoteAppend appends text to the git note for the given ref (usually HEAD SHA).
// Notes are hidden from normal git log and are ideal for verbose audit data.
func (r *Repo) NoteAppend(ref, text string) error {
	// git notes append takes stdin or -m; using -m keeps it simple.
	return r.git("notes", "append", "-m", text, ref)
}

// HEAD returns the current HEAD SHA.
func (r *Repo) HEAD() (string, error) {
	var out bytes.Buffer
	cmd := r.makeCmd("git", "rev-parse", "HEAD")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// --- internal helpers ---

// git runs a git subcommand with the shadow repo env vars set.
func (r *Repo) git(args ...string) error {
	cmd := r.makeCmd("git", args...)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w — %s", strings.Join(args, " "), err, errBuf.String())
	}
	return nil
}

// makeCmd constructs an exec.Cmd with GIT_DIR and GIT_WORK_TREE overrides.
func (r *Repo) makeCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(),
		"GIT_DIR="+r.gitDir,
		"GIT_WORK_TREE="+r.workDir,
	)
	cmd.Dir = r.workDir
	return cmd
}

// run executes an arbitrary command (used for git init which needs special handling).
func (r *Repo) run(name string, args ...string) error {
	var errBuf bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w — %s", name, strings.Join(args, " "), err, errBuf.String())
	}
	return nil
}

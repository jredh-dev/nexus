// Copyright (C) 2026 jredh-dev. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// This file is part of nexus, licensed under the GNU Affero General Public
// License v3.0 or later. See LICENSE for details.

package storyrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sampleYAML is a minimal valid story YAML used by tests. It's not a
// structurally valid vn story (no nodes etc.) — storyrepo doesn't parse
// YAML content, it just versions files.
const sampleYAML = `version: 1
title: Test Story
description: A test story for unit tests
start_node: ch1.intro
chapters:
  ch1:
    title: Chapter One
    start_node: intro
    nodes:
      intro:
        text: "Welcome to the test"
        is_end: true
`

// updatedYAML is a modified version of sampleYAML for testing diffs.
const updatedYAML = `version: 2
title: Test Story — Updated
description: Modified for diff testing
start_node: ch1.intro
chapters:
  ch1:
    title: Chapter One (Revised)
    start_node: intro
    nodes:
      intro:
        text: "Welcome to the updated test"
        is_end: true
`

// TestInit verifies that Init creates a git repo and can reopen it.
func TestInit(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	repoPath := filepath.Join(dir, "stories")

	// Init should create the directory and initialize a git repo.
	repo, err := Init(repoPath)
	if err != nil {
		t.Fatalf("Init(%q) failed: %v", repoPath, err)
	}

	// Verify the .git directory was created.
	gitDir := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Fatalf(".git directory not created at %q", gitDir)
	}

	// Path() should return the repo path.
	if repo.Path() != repoPath {
		t.Errorf("Path() = %q, want %q", repo.Path(), repoPath)
	}

	// Re-opening the same path should succeed (not re-init).
	repo2, err := Init(repoPath)
	if err != nil {
		t.Fatalf("Init(%q) on existing repo failed: %v", repoPath, err)
	}
	if repo2.Path() != repoPath {
		t.Errorf("reopened Path() = %q, want %q", repo2.Path(), repoPath)
	}
}

// TestCommit verifies that committing YAML files works and returns a hash.
func TestCommit(t *testing.T) {
	repo := initTestRepo(t)

	// Write a YAML file to the repo directory.
	writeFile(t, repo.Path(), "story.yaml", sampleYAML)

	// Commit should succeed and return a non-empty hash.
	hash, err := repo.Commit("initial story")
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if hash == "" {
		t.Fatal("Commit returned empty hash")
	}
	if len(hash) < 7 {
		t.Errorf("Commit hash too short: %q", hash)
	}

	// CurrentHash should match what Commit returned.
	currentHash, err := repo.CurrentHash()
	if err != nil {
		t.Fatalf("CurrentHash failed: %v", err)
	}
	if currentHash != hash {
		t.Errorf("CurrentHash = %q, want %q", currentHash, hash)
	}
}

// TestCommitEmpty verifies that committing with no changes fails gracefully.
func TestCommitEmpty(t *testing.T) {
	repo := initTestRepo(t)

	// Write and commit a file first.
	writeFile(t, repo.Path(), "story.yaml", sampleYAML)
	if _, err := repo.Commit("initial"); err != nil {
		t.Fatalf("initial commit failed: %v", err)
	}

	// Committing again with no changes should error.
	_, err := repo.Commit("no changes")
	if err == nil {
		t.Fatal("expected error committing with no changes, got nil")
	}
	if !strings.Contains(err.Error(), "nothing to commit") {
		t.Errorf("expected 'nothing to commit' error, got: %v", err)
	}
}

// TestCommitEmptyMessage verifies that empty commit messages are rejected.
func TestCommitEmptyMessage(t *testing.T) {
	repo := initTestRepo(t)

	writeFile(t, repo.Path(), "story.yaml", sampleYAML)

	_, err := repo.Commit("")
	if err == nil {
		t.Fatal("expected error for empty commit message, got nil")
	}
}

// TestLog verifies that commit history is returned correctly.
func TestLog(t *testing.T) {
	repo := initTestRepo(t)

	// Create three commits with different content.
	writeFile(t, repo.Path(), "story.yaml", sampleYAML)
	hash1, err := repo.Commit("first commit")
	if err != nil {
		t.Fatalf("first commit failed: %v", err)
	}

	writeFile(t, repo.Path(), "story.yaml", updatedYAML)
	hash2, err := repo.Commit("second commit")
	if err != nil {
		t.Fatalf("second commit failed: %v", err)
	}

	writeFile(t, repo.Path(), "extra.yaml", "extra: true\n")
	hash3, err := repo.Commit("third commit")
	if err != nil {
		t.Fatalf("third commit failed: %v", err)
	}

	// Log with no limit should return all 3 commits, newest first.
	commits, err := repo.Log(0)
	if err != nil {
		t.Fatalf("Log(0) failed: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("Log(0) returned %d commits, want 3", len(commits))
	}

	// Newest first.
	if commits[0].Hash != hash3 {
		t.Errorf("commits[0].Hash = %q, want %q", commits[0].Hash, hash3)
	}
	if commits[1].Hash != hash2 {
		t.Errorf("commits[1].Hash = %q, want %q", commits[1].Hash, hash2)
	}
	if commits[2].Hash != hash1 {
		t.Errorf("commits[2].Hash = %q, want %q", commits[2].Hash, hash1)
	}

	// Verify commit messages.
	if commits[0].Message != "third commit" {
		t.Errorf("commits[0].Message = %q, want %q", commits[0].Message, "third commit")
	}
	if commits[1].Message != "second commit" {
		t.Errorf("commits[1].Message = %q, want %q", commits[1].Message, "second commit")
	}
	if commits[2].Message != "first commit" {
		t.Errorf("commits[2].Message = %q, want %q", commits[2].Message, "first commit")
	}

	// Verify author is the fixed vn-engine identity.
	for i, c := range commits {
		if !strings.Contains(c.Author, "vn-engine") {
			t.Errorf("commits[%d].Author = %q, want to contain 'vn-engine'", i, c.Author)
		}
	}

	// Verify timestamps are reasonable (within the last minute).
	now := time.Now()
	for i, c := range commits {
		if now.Sub(c.Timestamp) > time.Minute {
			t.Errorf("commits[%d].Timestamp %v is too old (now: %v)", i, c.Timestamp, now)
		}
	}
}

// TestLogWithLimit verifies that the limit parameter works.
func TestLogWithLimit(t *testing.T) {
	repo := initTestRepo(t)

	// Create 3 commits.
	for i := 0; i < 3; i++ {
		writeFile(t, repo.Path(), "story.yaml", strings.ReplaceAll(sampleYAML, "Test Story", "Story v"+strings.Repeat("I", i+1)))
		if _, err := repo.Commit("commit " + strings.Repeat("I", i+1)); err != nil {
			t.Fatalf("commit %d failed: %v", i+1, err)
		}
	}

	// Limit to 2 should return only the 2 newest.
	commits, err := repo.Log(2)
	if err != nil {
		t.Fatalf("Log(2) failed: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("Log(2) returned %d commits, want 2", len(commits))
	}
}

// TestLogEmptyRepo verifies that Log on an empty repo returns nil, not an error.
func TestLogEmptyRepo(t *testing.T) {
	repo := initTestRepo(t)

	commits, err := repo.Log(0)
	if err != nil {
		t.Fatalf("Log on empty repo failed: %v", err)
	}
	if commits != nil {
		t.Errorf("Log on empty repo returned %v, want nil", commits)
	}
}

// TestRevert verifies that reverting restores file content to a previous commit.
func TestRevert(t *testing.T) {
	repo := initTestRepo(t)

	// Commit the original story.
	writeFile(t, repo.Path(), "story.yaml", sampleYAML)
	hash1, err := repo.Commit("original story")
	if err != nil {
		t.Fatalf("first commit failed: %v", err)
	}

	// Modify and commit again.
	writeFile(t, repo.Path(), "story.yaml", updatedYAML)
	_, err = repo.Commit("updated story")
	if err != nil {
		t.Fatalf("second commit failed: %v", err)
	}

	// Verify the file currently has updated content.
	content := readFile(t, repo.Path(), "story.yaml")
	if !strings.Contains(content, "Updated") {
		t.Fatal("expected updated content before revert")
	}

	// Revert to the first commit.
	if err := repo.Revert(hash1); err != nil {
		t.Fatalf("Revert(%q) failed: %v", hash1, err)
	}

	// File content should match the original.
	content = readFile(t, repo.Path(), "story.yaml")
	if strings.Contains(content, "Updated") {
		t.Error("file still contains 'Updated' after revert")
	}
	if !strings.Contains(content, "Test Story") {
		t.Error("file missing original 'Test Story' content after revert")
	}

	// A revert commit should have been created, so log should show 3 commits.
	commits, err := repo.Log(0)
	if err != nil {
		t.Fatalf("Log after revert failed: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("expected 3 commits after revert, got %d", len(commits))
	}
	if !strings.Contains(commits[0].Message, "revert to") {
		t.Errorf("revert commit message = %q, want to contain 'revert to'", commits[0].Message)
	}
}

// TestRevertInvalidHash verifies that reverting to a nonexistent hash fails.
func TestRevertInvalidHash(t *testing.T) {
	repo := initTestRepo(t)

	writeFile(t, repo.Path(), "story.yaml", sampleYAML)
	if _, err := repo.Commit("initial"); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	err := repo.Revert("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if err == nil {
		t.Fatal("expected error reverting to invalid hash, got nil")
	}
}

// TestRevertEmptyHash verifies that empty hash is rejected.
func TestRevertEmptyHash(t *testing.T) {
	repo := initTestRepo(t)

	err := repo.Revert("")
	if err == nil {
		t.Fatal("expected error for empty hash, got nil")
	}
}

// TestDiff verifies that diffs between commits show changes.
func TestDiff(t *testing.T) {
	repo := initTestRepo(t)

	// Commit the original.
	writeFile(t, repo.Path(), "story.yaml", sampleYAML)
	hash1, err := repo.Commit("original")
	if err != nil {
		t.Fatalf("first commit failed: %v", err)
	}

	// Commit the update.
	writeFile(t, repo.Path(), "story.yaml", updatedYAML)
	hash2, err := repo.Commit("update")
	if err != nil {
		t.Fatalf("second commit failed: %v", err)
	}

	// Diff between the two commits should show the changes.
	diff, err := repo.Diff(hash1, hash2)
	if err != nil {
		t.Fatalf("Diff(%q, %q) failed: %v", hash1, hash2, err)
	}

	// The diff should mention both the old and new content.
	if !strings.Contains(diff, "Test Story") {
		t.Error("diff missing 'Test Story'")
	}
	if !strings.Contains(diff, "Updated") {
		t.Error("diff missing 'Updated'")
	}

	// Diff from hash1 to HEAD (toHash empty) should produce the same result.
	diffToHead, err := repo.Diff(hash1, "")
	if err != nil {
		t.Fatalf("Diff(%q, \"\") failed: %v", hash1, err)
	}
	if diffToHead != diff {
		t.Error("diff to HEAD differs from diff to explicit hash2")
	}
}

// TestDiffNoChanges verifies that diffing identical commits returns empty.
func TestDiffNoChanges(t *testing.T) {
	repo := initTestRepo(t)

	writeFile(t, repo.Path(), "story.yaml", sampleYAML)
	hash, err := repo.Commit("only commit")
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	diff, err := repo.Diff(hash, hash)
	if err != nil {
		t.Fatalf("Diff same hash failed: %v", err)
	}
	if strings.TrimSpace(diff) != "" {
		t.Errorf("expected empty diff for same hash, got: %q", diff)
	}
}

// TestMultipleFilesCommit verifies that multiple YAML files are tracked.
func TestMultipleFilesCommit(t *testing.T) {
	repo := initTestRepo(t)

	// Write two YAML files.
	writeFile(t, repo.Path(), "chapter1.yaml", sampleYAML)
	writeFile(t, repo.Path(), "chapter2.yml", updatedYAML) // .yml extension

	hash, err := repo.Commit("multi-file commit")
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}
	if hash == "" {
		t.Fatal("commit returned empty hash")
	}

	// Log should show one commit.
	commits, err := repo.Log(0)
	if err != nil {
		t.Fatalf("Log failed: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}

	// Modify one file, commit again. Diff should show only the changed file.
	writeFile(t, repo.Path(), "chapter1.yaml", updatedYAML)
	hash2, err := repo.Commit("update chapter1")
	if err != nil {
		t.Fatalf("second commit failed: %v", err)
	}

	diff, err := repo.Diff(hash, hash2)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if !strings.Contains(diff, "chapter1.yaml") {
		t.Error("diff missing reference to changed file chapter1.yaml")
	}
}

// TestCurrentHashEmptyRepo verifies that CurrentHash fails on empty repos.
func TestCurrentHashEmptyRepo(t *testing.T) {
	repo := initTestRepo(t)

	_, err := repo.CurrentHash()
	if err == nil {
		t.Fatal("expected error for CurrentHash on empty repo, got nil")
	}
}

// TestFileDeletion verifies that deleting a YAML file is tracked by commit.
func TestFileDeletion(t *testing.T) {
	repo := initTestRepo(t)

	// Commit two files.
	writeFile(t, repo.Path(), "a.yaml", sampleYAML)
	writeFile(t, repo.Path(), "b.yaml", updatedYAML)
	hash1, err := repo.Commit("two files")
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	// Delete one file and commit.
	if err := os.Remove(filepath.Join(repo.Path(), "b.yaml")); err != nil {
		t.Fatalf("remove b.yaml: %v", err)
	}
	hash2, err := repo.Commit("remove b.yaml")
	if err != nil {
		t.Fatalf("commit after delete failed: %v", err)
	}

	// Diff should show b.yaml was deleted.
	diff, err := repo.Diff(hash1, hash2)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if !strings.Contains(diff, "b.yaml") {
		t.Error("diff missing reference to deleted file b.yaml")
	}
}

// --- Test helpers ---

// requireGit skips the test if the git binary is not available.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available, skipping")
	}
}

// initTestRepo creates a new storyrepo in a temp directory for testing.
func initTestRepo(t *testing.T) *Repo {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	repoPath := filepath.Join(dir, "stories")

	repo, err := Init(repoPath)
	if err != nil {
		t.Fatalf("Init test repo: %v", err)
	}
	return repo
}

// writeFile writes content to a file in the given directory.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// readFile reads a file from the given directory.
func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(data)
}

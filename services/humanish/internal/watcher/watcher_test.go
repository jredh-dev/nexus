package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAgentFileBypass verifies that writes to AGENTS.md and *.AGENTS.md files
// are emitted immediately without waiting for the quiet-period.
func TestAgentFileBypass(t *testing.T) {
	dir := t.TempDir()

	// Use a very long quiet period so only the bypass path can emit.
	cfg := Config{
		QuietSeconds:    60,
		MaxQuietSeconds: 300,
	}

	w, err := New(dir, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(w.Stop)

	agentsPath := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# instructions\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	select {
	case batch := <-w.Batches:
		if len(batch.Paths) != 1 || batch.Paths[0] != agentsPath {
			t.Errorf("unexpected batch paths: %v", batch.Paths)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for agent-file batch (expected immediate emit)")
	}
}

// TestAgentFilePrefixedBypass verifies that *.AGENTS.md files also bypass the quiet-period.
func TestAgentFilePrefixedBypass(t *testing.T) {
	dir := t.TempDir()

	cfg := Config{
		QuietSeconds:    60,
		MaxQuietSeconds: 300,
	}

	w, err := New(dir, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(w.Stop)

	agentsPath := filepath.Join(dir, "Agents.AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# form\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	select {
	case batch := <-w.Batches:
		if len(batch.Paths) != 1 || batch.Paths[0] != agentsPath {
			t.Errorf("unexpected batch paths: %v", batch.Paths)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Agents.AGENTS.md batch (expected immediate emit)")
	}
}

// TestNonAgentFileRespectQuietPeriod verifies that regular visible files (e.g. PLAN.md)
// are NOT emitted immediately but wait for the quiet-period.
func TestNonAgentFileRespectQuietPeriod(t *testing.T) {
	dir := t.TempDir()

	// Short quiet period so the test doesn't take too long.
	cfg := Config{
		QuietSeconds:    1,
		MaxQuietSeconds: 10,
	}

	w, err := New(dir, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(w.Stop)

	planPath := filepath.Join(dir, "PLAN.md")
	if err := os.WriteFile(planPath, []byte("# plan\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Should NOT be emitted within 400ms (less than 1-second quiet period).
	select {
	case batch := <-w.Batches:
		t.Errorf("expected quiet-period to delay batch but got immediate emit: %v", batch.Paths)
	case <-time.After(400 * time.Millisecond):
		// Good — not emitted immediately.
	}

	// Should be emitted after the quiet period elapses (within 3 seconds total).
	select {
	case batch := <-w.Batches:
		if len(batch.Paths) != 1 || batch.Paths[0] != planPath {
			t.Errorf("unexpected batch paths: %v", batch.Paths)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for PLAN.md batch after quiet period")
	}
}

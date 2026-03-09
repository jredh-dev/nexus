// watcher — file watcher and OpenCode integration service.
//
// Monitors a bind-mounted directory (WATCHER_DIR), computes diffs against the
// real git repo (the host directory is bind-mounted and IS a real git repo),
// and sends those diffs to the OpenCode headless server.
//
// On startup it sends an initialization message so the agent reads FORM.md
// and understands its context before any diffs arrive.
//
// Environment variables:
//
//	WATCHER_DIR             path to the watched volume (required)
//	OPENCODE_URL             OpenCode server URL (default: http://opencode:4096)
//	OPENCODE_PASSWORD        OpenCode server password (required)
//	WATCHER_QUIET_SECONDS   seconds of file silence before committing (default: 5)
//	WATCHER_MAX_QUIET_SEC   hard max wait per file in seconds (default: 300)
//	WATCHER_AUDIT_LOG       path to audit log file (default: <WATCHER_DIR>/work/watcher-audit.log)
//	WATCHER_BRANCH          branch mode: on-demand | on-schedule (default: on-demand)
package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jredh-dev/nexus/services/watcher/internal/agents"
	"github.com/jredh-dev/nexus/services/watcher/internal/filter"
	"github.com/jredh-dev/nexus/services/watcher/internal/opencode"
	"github.com/jredh-dev/nexus/services/watcher/internal/watcher"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	if err := run(cfg); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// config holds all runtime configuration.
type config struct {
	dir          string
	opencodeURL  string
	opencodePass string
	quietSeconds int
	maxQuietSec  int
	auditLog     string
	branch       string
}

func loadConfig() (config, error) {
	c := config{
		dir:          os.Getenv("WATCHER_DIR"),
		opencodeURL:  envOr("OPENCODE_URL", "http://opencode:4096"),
		opencodePass: os.Getenv("OPENCODE_PASSWORD"),
		branch:       envOr("WATCHER_BRANCH", "on-demand"),
	}

	if c.dir == "" {
		return c, fmt.Errorf("WATCHER_DIR is required")
	}
	if c.opencodePass == "" {
		return c, fmt.Errorf("OPENCODE_PASSWORD is required")
	}

	c.quietSeconds = envInt("WATCHER_QUIET_SECONDS", 5)
	c.maxQuietSec = envInt("WATCHER_MAX_QUIET_SEC", 300)

	// Default audit log: <WATCHER_DIR>/work/watcher-audit.log
	c.auditLog = envOr("WATCHER_AUDIT_LOG",
		filepath.Join(c.dir, "work", "watcher-audit.log"))

	return c, nil
}

func run(cfg config) error {
	slog.Info("humanish starting",
		"dir", cfg.dir,
		"opencode_url", cfg.opencodeURL,
		"quiet_seconds", cfg.quietSeconds,
		"max_quiet_sec", cfg.maxQuietSec,
		"branch", cfg.branch,
	)

	// Ensure audit log directory exists.
	if err := os.MkdirAll(filepath.Dir(cfg.auditLog), 0755); err != nil {
		return fmt.Errorf("create audit log dir: %w", err)
	}

	// Verify cfg.dir is a real git repo — fail fast if not.
	if err := gitCheck(cfg.dir); err != nil {
		return fmt.Errorf("WATCHER_DIR is not a git repo: %w", err)
	}
	slog.Info("git repo verified", "dir", cfg.dir)

	// Build OpenCode client.
	oc := opencode.New(cfg.opencodeURL, cfg.opencodePass)

	// Wait for OpenCode server to be healthy (retry up to 60s).
	slog.Info("waiting for OpenCode server", "url", cfg.opencodeURL)
	if err := waitForHealth(oc, 60*time.Second); err != nil {
		return fmt.Errorf("opencode server not reachable: %w", err)
	}
	slog.Info("opencode server healthy")

	// --- Send initialization message ---
	// Read FORM.md hierarchy so the agent knows its context before any diffs.
	if err := sendInit(cfg, oc); err != nil {
		// Non-fatal: log and continue. Diffs will still be sent.
		slog.Warn("init message failed", "err", err)
	}

	// Start file watcher.
	w, err := watcher.New(cfg.dir, watcher.Config{
		QuietSeconds:    cfg.quietSeconds,
		MaxQuietSeconds: cfg.maxQuietSec,
	})
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	if err := w.Start(); err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}
	defer w.Stop()

	slog.Info("watching for changes", "dir", cfg.dir)

	// Signal handling.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-sigCh:
			slog.Info("shutdown signal received")
			return nil

		case batch, ok := <-w.Batches:
			if !ok {
				return nil
			}
			if err := processBatch(cfg, oc, batch.Paths); err != nil {
				slog.Error("batch processing error", "err", err)
				// Continue — don't crash on a single bad batch.
			}
		}
	}
}

// sendInit reads the FORM.md hierarchy and sends a startup message to
// OpenCode so it ingests instructions before any diffs arrive.
func sendInit(cfg config, oc *opencode.Client) error {
	formFiles, err := agents.Collect(cfg.dir)
	if err != nil {
		return err
	}

	ctx := agents.MergedContext(formFiles)

	initMsg := fmt.Sprintf(`You are the watcher agent. Your role is defined in the FORM.md files below.

Please read all FORM.md instructions carefully. You are now running in "%s" mode.

Your working directory is: %s

After reading the instructions, confirm you understand your role in one short paragraph.
If any instructions are unclear or missing, note what you need. Do NOT make changes yet.
Wait for the first diff to arrive.`, cfg.branch, cfg.dir)

	slog.Info("sending initialization message to OpenCode")
	reply, err := oc.Send(ctx, initMsg)
	if err != nil {
		return fmt.Errorf("init send: %w", err)
	}

	slog.Info("opencode init response received", "length", len(reply))

	// Append to audit log.
	appendAudit(cfg.auditLog, "INIT", map[string]string{
		"branch": cfg.branch,
		"reply":  truncate(reply, 500),
	})

	return nil
}

// processBatch diffs the given paths against HEAD using the real git repo,
// sends the diff to OpenCode, and logs the result.
func processBatch(cfg config, oc *opencode.Client, paths []string) error {
	// Filter to only visible files that still exist.
	var visible []string
	for _, p := range paths {
		rel, _ := filepath.Rel(cfg.dir, p)
		if filter.IsVisible(rel) {
			visible = append(visible, p)
		}
	}
	if len(visible) == 0 {
		return nil
	}

	// Check if any of the changed files are FORM.md — flag for the prompt.
	var formUpdated bool
	for _, p := range visible {
		if filter.IsFormFile(p) {
			formUpdated = true
			break
		}
	}

	// Get a real git diff for just these files.
	diff, err := gitDiff(cfg.dir, visible)
	if err != nil {
		return fmt.Errorf("git diff: %w", err)
	}
	if diff == "" {
		slog.Debug("no diff for batch, skipping")
		return nil
	}

	// Read current FORM.md hierarchy (always fresh — may have just changed).
	formFiles, err := agents.Collect(cfg.dir)
	if err != nil {
		slog.Warn("could not collect FORM.md", "err", err)
	}
	formCtx := agents.MergedContext(formFiles)

	// Build the message body.
	var sb strings.Builder
	if formUpdated {
		sb.WriteString("Note: FORM.md was updated in this batch. Please re-read your instructions above.\n\n")
	}
	sb.WriteString(fmt.Sprintf("New changes detected in the watched volume (%d files):\n\n", len(visible)))
	sb.WriteString("```diff\n")
	sb.WriteString(diff)
	sb.WriteString("\n```\n\n")
	sb.WriteString("Please:\n")
	sb.WriteString("1. Read the FORM.md hierarchy above (instructions take precedence deepest-first).\n")
	sb.WriteString("2. Update the most relevant FORM.md immediately if the changes affect your instructions.\n")
	sb.WriteString("3. Respond with a brief assessment of what changed and any actions taken.\n")
	sb.WriteString("4. If you detect a conflict or ambiguity that requires human input, say so clearly.\n")

	slog.Info("sending diff to OpenCode", "files", len(visible))
	reply, err := oc.Send(formCtx, sb.String())
	if err != nil {
		return fmt.Errorf("opencode send: %w", err)
	}

	slog.Info("opencode response received", "length", len(reply))

	// Append to audit log.
	appendAudit(cfg.auditLog, "BATCH", map[string]string{
		"files": strings.Join(visible, ", "),
		"reply": truncate(reply, 500),
	})

	return nil
}

// gitCheck verifies that dir is inside a real git repository.
func gitCheck(dir string) error {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	return cmd.Run()
}

// gitDiff returns a unified diff of the given paths against HEAD.
// Uses `git diff HEAD -- <paths>` so it shows both staged and unstaged changes.
// Falls back to `git diff -- <paths>` if HEAD doesn't exist (empty repo).
func gitDiff(dir string, paths []string) (string, error) {
	// Build relative paths — git diff wants paths relative to the repo root.
	var relPaths []string
	for _, p := range paths {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			continue
		}
		relPaths = append(relPaths, rel)
	}
	if len(relPaths) == 0 {
		return "", nil
	}

	args := append([]string{"diff", "HEAD", "--"}, relPaths...)
	var out, errBuf bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		// HEAD may not exist on an empty repo; fall back to index diff.
		args2 := append([]string{"diff", "--"}, relPaths...)
		cmd2 := exec.Command("git", args2...)
		cmd2.Dir = dir
		out.Reset()
		errBuf.Reset()
		cmd2.Stdout = &out
		cmd2.Stderr = &errBuf
		if err2 := cmd2.Run(); err2 != nil {
			return "", fmt.Errorf("git diff: %w — %s", err2, errBuf.String())
		}
	}

	return strings.TrimSpace(out.String()), nil
}

// waitForHealth retries Health() until it succeeds or timeout expires.
func waitForHealth(oc *opencode.Client, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := oc.Health(); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out after %s", timeout)
}

// appendAudit appends a structured entry to the audit log.
func appendAudit(path, event string, fields map[string]string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		slog.Warn("audit log open failed", "err", err)
		return
	}
	defer f.Close()

	ts := time.Now().Format(time.RFC3339)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] EVENT=%s", ts, event))
	for k, v := range fields {
		sb.WriteString(fmt.Sprintf(" %s=%q", k, truncate(v, 200)))
	}
	sb.WriteString("\n")
	_, _ = f.WriteString(sb.String())
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

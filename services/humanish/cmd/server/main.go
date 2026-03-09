// humanish — file watcher and OpenCode integration service.
//
// Monitors a bind-mounted directory (HUMANISH_DIR), maintains a shadow git
// repo of visible changes, and sends diffs to the OpenCode headless server.
// On startup it sends an initialization message so the agent reads AGENTS.md
// and understands its context before any diffs arrive.
//
// Environment variables:
//
//	HUMANISH_DIR             path to the humanish volume (required)
//	OPENCODE_URL             OpenCode server URL (default: http://opencode:4096)
//	OPENCODE_PASSWORD        OpenCode server password (required)
//	OPENCODE_PROVIDER_ID     OpenCode provider ID (default: github-copilot)
//	OPENCODE_MODEL_ID        OpenCode model ID (default: claude-sonnet-4.6)
//	HUMANISH_QUIET_SECONDS   seconds of file silence before committing (default: 5)
//	HUMANISH_MAX_QUIET_SEC   hard max wait per file in seconds (default: 300)
//	HUMANISH_AUDIT_LOG       path to audit log file (default: <HUMANISH_DIR>/work/humanish-audit.log)
//	HUMANISH_BRANCH          branch mode: on-demand | on-schedule (default: on-demand)
package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jredh-dev/nexus/services/humanish/internal/agents"
	"github.com/jredh-dev/nexus/services/humanish/internal/filter"
	"github.com/jredh-dev/nexus/services/humanish/internal/opencode"
	"github.com/jredh-dev/nexus/services/humanish/internal/shadow"
	"github.com/jredh-dev/nexus/services/humanish/internal/watcher"
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
	dir              string
	opencodeURL      string
	opencodePass     string
	opencodeProvider string
	opencodeModel    string
	quietSeconds     int
	maxQuietSec      int
	auditLog         string
	branch           string
}

func loadConfig() (config, error) {
	c := config{
		dir:              os.Getenv("HUMANISH_DIR"),
		opencodeURL:      envOr("OPENCODE_URL", "http://opencode:4096"),
		opencodePass:     os.Getenv("OPENCODE_PASSWORD"),
		opencodeProvider: envOr("OPENCODE_PROVIDER_ID", "github-copilot"),
		opencodeModel:    envOr("OPENCODE_MODEL_ID", "claude-sonnet-4.6"),
		branch:           envOr("HUMANISH_BRANCH", "on-demand"),
	}

	if c.dir == "" {
		return c, fmt.Errorf("HUMANISH_DIR is required")
	}
	if c.opencodePass == "" {
		return c, fmt.Errorf("OPENCODE_PASSWORD is required")
	}

	c.quietSeconds = envInt("HUMANISH_QUIET_SECONDS", 5)
	c.maxQuietSec = envInt("HUMANISH_MAX_QUIET_SEC", 300)

	// Default audit log: <HUMANISH_DIR>/work/humanish-audit.log
	c.auditLog = envOr("HUMANISH_AUDIT_LOG",
		filepath.Join(c.dir, "work", "humanish-audit.log"))

	return c, nil
}

func run(cfg config) error {
	slog.Info("humanish starting",
		"dir", cfg.dir,
		"opencode_url", cfg.opencodeURL,
		"provider", cfg.opencodeProvider,
		"model", cfg.opencodeModel,
		"quiet_seconds", cfg.quietSeconds,
		"max_quiet_sec", cfg.maxQuietSec,
		"branch", cfg.branch,
	)

	// Ensure audit log directory exists.
	if err := os.MkdirAll(filepath.Dir(cfg.auditLog), 0755); err != nil {
		return fmt.Errorf("create audit log dir: %w", err)
	}

	// Initialise shadow git repo.
	repo := shadow.New(cfg.dir)
	if err := repo.Init(); err != nil {
		return fmt.Errorf("shadow git init: %w", err)
	}

	// Do an initial stage-all so the shadow repo has a baseline.
	if err := initialStage(cfg.dir, repo); err != nil {
		slog.Warn("initial stage error (non-fatal)", "err", err)
	}

	// Build OpenCode client.
	oc := opencode.New(cfg.opencodeURL, cfg.opencodePass, cfg.opencodeProvider, cfg.opencodeModel)

	// Wait for OpenCode server to be healthy (retry up to 60s).
	slog.Info("waiting for OpenCode server", "url", cfg.opencodeURL)
	if err := waitForHealth(oc, 60*time.Second); err != nil {
		return fmt.Errorf("opencode server not reachable: %w", err)
	}
	slog.Info("opencode server healthy")

	// --- Send initialization message ---
	// Read AGENTS.md hierarchy so the agent knows its context before any diffs.
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
			if err := processBatch(cfg, repo, oc, batch.Paths); err != nil {
				slog.Error("batch processing error", "err", err)
				// Continue — don't crash on a single bad batch.
			}
		}
	}
}

// sendInit sends a lean startup message to OpenCode.
//
// We do NOT embed the full AGENTS.md hierarchy in the message body — the
// OpenCode server already injects the project AGENTS.md automatically, and
// adding another large context blob risks exceeding the provider's context
// window (which triggers a runaway compaction loop).  Instead we tell
// OpenCode where the humanish files live and ask it to read them directly.
func sendInit(cfg config, oc *opencode.Client) error {
	initMsg := fmt.Sprintf(`humanish watcher started in "%s" mode.

Humanish directory: %s

Please read %s/AGENTS.md and any *.AGENTS.md files in that directory.
Confirm you understand your role in one short paragraph.
Do NOT make any changes yet — wait for the first file-change diff.`,
		cfg.branch, cfg.dir, cfg.dir)

	slog.Info("sending initialization message to OpenCode")
	reply, err := oc.Send("", initMsg)
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

// processBatch stages changed files, diffs them, sends to OpenCode, and commits.
func processBatch(cfg config, repo *shadow.Repo, oc *opencode.Client, paths []string) error {
	// Collect only currently-visible files from the batch.
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

	// Check if any of the changed files are AGENTS.md — read them first.
	var agentsUpdated bool
	for _, p := range visible {
		if filter.IsAgentsFile(p) {
			agentsUpdated = true
			break
		}
	}

	// Stage the changed files.
	if err := repo.Stage(visible); err != nil {
		return fmt.Errorf("stage: %w", err)
	}

	// Get the diff.
	diff, err := repo.Diff()
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}
	if diff == "" {
		slog.Debug("no staged changes in diff, skipping")
		return nil
	}

	// Read current AGENTS.md hierarchy (always fresh — may have just changed).
	agentFiles, err := agents.Collect(cfg.dir)
	if err != nil {
		slog.Warn("could not collect AGENTS.md", "err", err)
	}
	agentsCtx := agents.MergedContext(agentFiles)

	// Build the message body.
	var sb strings.Builder
	if agentsUpdated {
		sb.WriteString("Note: AGENTS.md was updated in this batch. Please re-read your instructions above.\n\n")
	}
	sb.WriteString(fmt.Sprintf("New changes detected in the humanish volume (%d files):\n\n", len(visible)))
	sb.WriteString("```diff\n")
	sb.WriteString(diff)
	sb.WriteString("\n```\n\n")
	sb.WriteString("Please:\n")
	sb.WriteString("1. Read the AGENTS.md hierarchy above (instructions take precedence deepest-first).\n")
	sb.WriteString("2. Update the most relevant AGENTS.md immediately if the changes affect your instructions.\n")
	sb.WriteString("3. Respond with a brief assessment of what changed and any actions taken.\n")
	sb.WriteString("4. If you detect a conflict or ambiguity that requires human input, say so clearly.\n")

	slog.Info("sending diff to OpenCode", "files", len(visible))
	reply, err := oc.Send(agentsCtx, sb.String())
	if err != nil {
		return fmt.Errorf("opencode send: %w", err)
	}

	slog.Info("opencode response received", "length", len(reply))

	// Commit the batch to the shadow repo.
	msg := fmt.Sprintf("snapshot: %d file(s) — %s", len(visible), time.Now().Format(time.RFC3339))
	sha, err := repo.Commit(msg)
	if err != nil {
		return fmt.Errorf("shadow commit: %w", err)
	}

	// Append verbose hidden note to the shadow commit.
	note := fmt.Sprintf("opencode_response:\n%s\n\nbatch_files:\n%s",
		reply, strings.Join(visible, "\n"))
	if noteErr := repo.NoteAppend(sha, note); noteErr != nil {
		slog.Warn("shadow note append failed (non-fatal)", "err", noteErr)
	}

	// Append to audit log.
	appendAudit(cfg.auditLog, "BATCH", map[string]string{
		"sha":   sha,
		"files": strings.Join(visible, ", "),
		"reply": truncate(reply, 500),
	})

	return nil
}

// initialStage performs a one-time stage of all existing visible files.
func initialStage(base string, repo *shadow.Repo) error {
	hasChanges, err := repo.HasStagedChanges()
	if err != nil {
		return err
	}
	if hasChanges {
		return nil // already staged
	}

	var visiblePaths []string
	err = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".humanish-git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(base, path)
		if filter.IsVisible(rel) {
			visiblePaths = append(visiblePaths, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(visiblePaths) == 0 {
		return nil
	}
	return repo.Stage(visiblePaths)
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

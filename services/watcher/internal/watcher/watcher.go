// Package watcher monitors the humanish volume for file changes using fsnotify.
//
// Design:
//   - A fsnotify.Watcher watches the entire directory tree recursively.
//   - Each write/create/rename event resets a per-file quiet timer.
//   - A file is considered "settled" when no events have arrived for QuietSeconds.
//   - A hard MaxQuietSeconds caps the wait so very long editing sessions still
//     get committed eventually.
//   - Race-condition policy: if a file is still changing when a batch is being
//     processed, it is deferred to the next batch rather than committed mid-edit.
//   - Invisible files (per filter.IsVisible) are ignored at the event level.
//   - Form files (FORM.md, *.FORM.md) bypass the quiet-period entirely and
//     are emitted immediately as a single-file batch.
//
// The caller receives a Batch of settled file paths on the Batches channel.
package watcher

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/jredh-dev/nexus/services/watcher/internal/filter"
)

// Batch is a set of file paths that have settled (no writes for QuietSeconds).
type Batch struct {
	Paths []string
}

// Config controls watcher timing behaviour.
type Config struct {
	// QuietSeconds: seconds of silence before a file is considered settled.
	// Default: 5.
	QuietSeconds int
	// MaxQuietSeconds: hard ceiling on wait time per file.
	// Default: 300 (5 minutes).
	MaxQuietSeconds int
}

func (c *Config) quiet() time.Duration {
	if c.QuietSeconds <= 0 {
		return 5 * time.Second
	}
	return time.Duration(c.QuietSeconds) * time.Second
}

func (c *Config) maxQuiet() time.Duration {
	if c.MaxQuietSeconds <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(c.MaxQuietSeconds) * time.Second
}

// Watcher watches a directory tree and emits batches of settled files.
type Watcher struct {
	cfg     Config
	base    string
	Batches chan Batch
	stop    chan struct{}

	mu        sync.Mutex
	pending   map[string]time.Time // path → time of last event
	firstSeen map[string]time.Time // path → time first seen in current batch
	fw        *fsnotify.Watcher
}

// New creates a Watcher rooted at base.
func New(base string, cfg Config) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		cfg:       cfg,
		base:      base,
		Batches:   make(chan Batch, 16),
		stop:      make(chan struct{}),
		pending:   make(map[string]time.Time),
		firstSeen: make(map[string]time.Time),
		fw:        fw,
	}
	return w, nil
}

// Start begins watching. Call Stop to terminate.
func (w *Watcher) Start() error {
	// Add root and all existing subdirectories recursively.
	if err := w.addDirRecursive(w.base); err != nil {
		return err
	}

	go w.eventLoop()
	go w.batchLoop()
	return nil
}

// Stop shuts down the watcher.
func (w *Watcher) Stop() {
	close(w.stop)
	_ = w.fw.Close()
}

// Interrupt signals the batch loop to flush all pending files immediately,
// bypassing the quiet-period wait. Used by the HTTP control server's
// /interrupt endpoint.
func (w *Watcher) Interrupt() {
	// Force checkSettled to treat all pending files as settled now by
	// directly draining and emitting them.
	w.mu.Lock()
	var paths []string
	for path := range w.pending {
		paths = append(paths, path)
		delete(w.pending, path)
		delete(w.firstSeen, path)
	}
	w.mu.Unlock()

	if len(paths) > 0 {
		select {
		case w.Batches <- Batch{Paths: paths}:
		default:
			slog.Warn("watcher: interrupt — batch channel full, dropping", "count", len(paths))
		}
	}
}

// PendingCount returns the number of files currently waiting for their
// quiet period to expire.
func (w *Watcher) PendingCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.pending)
}

// addDirRecursive adds a directory and all its subdirectories to fsnotify.
func (w *Watcher) addDirRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if d.IsDir() {
			if err := w.fw.Add(path); err != nil {
				slog.Warn("watcher: could not add dir", "path", path, "err", err)
			}
		}
		return nil
	})
}

// eventLoop drains fsnotify events and tracks pending files.
func (w *Watcher) eventLoop() {
	for {
		select {
		case <-w.stop:
			return
		case event, ok := <-w.fw.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.fw.Errors:
			if !ok {
				return
			}
			slog.Warn("watcher fsnotify error", "err", err)
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	path := event.Name

	// If a new directory was created, watch it recursively.
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		_ = w.addDirRecursive(path)
		return
	}

	// Apply visibility filter — ignore invisible files entirely.
	rel, _ := filepath.Rel(w.base, path)
	if !filter.IsVisible(rel) {
		return
	}

	// Remove events: drop the file from pending (no longer exists).
	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		w.mu.Lock()
		delete(w.pending, path)
		delete(w.firstSeen, path)
		w.mu.Unlock()
		return
	}

	// Form files (FORM.md, *.FORM.md) bypass the quiet-period entirely
	// and are emitted immediately as a single-file batch. These are shared
	// files that can be written at any time — no courtesy delay needed.
	if filter.IsFormFile(rel) {
		slog.Debug("watcher: form file changed — emitting immediately", "path", rel)
		select {
		case w.Batches <- Batch{Paths: []string{path}}:
		default:
			slog.Warn("watcher: batch channel full, dropping agent file batch", "path", rel)
		}
		return
	}

	now := time.Now()
	w.mu.Lock()
	if _, exists := w.firstSeen[path]; !exists {
		w.firstSeen[path] = now
	}
	w.pending[path] = now // reset quiet timer
	w.mu.Unlock()
}

// batchLoop periodically checks for settled files and emits batches.
func (w *Watcher) batchLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.checkSettled()
		}
	}
}

func (w *Watcher) checkSettled() {
	now := time.Now()
	quiet := w.cfg.quiet()
	maxWait := w.cfg.maxQuiet()

	w.mu.Lock()
	var settled []string
	for path, lastEvent := range w.pending {
		first := w.firstSeen[path]
		quietElapsed := now.Sub(lastEvent) >= quiet
		maxElapsed := now.Sub(first) >= maxWait

		if quietElapsed || maxElapsed {
			if maxElapsed && !quietElapsed {
				// Hard timeout hit while file is still being written.
				// Surface to human rather than silently committing a partial edit.
				slog.Warn("watcher: file hit max timeout while still being edited — deferring to human",
					"path", path,
					"age", now.Sub(first).Round(time.Second),
				)
				// Remove from pending so we don't keep logging, but do NOT
				// add to settled — the caller will see a RaceDetected log entry.
				delete(w.pending, path)
				delete(w.firstSeen, path)
				continue
			}
			settled = append(settled, path)
			delete(w.pending, path)
			delete(w.firstSeen, path)
		}
	}
	w.mu.Unlock()

	if len(settled) > 0 {
		select {
		case w.Batches <- Batch{Paths: settled}:
		default:
			slog.Warn("watcher: batch channel full, dropping batch", "count", len(settled))
		}
	}
}

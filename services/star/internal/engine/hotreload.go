package engine

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
)

// HotLoader watches a story directory for YAML changes and atomically
// swaps the loaded story. It uses atomic.Value for lock-free reads and
// fsnotify for filesystem events.
type HotLoader struct {
	storyDir string
	story    atomic.Value // holds *Story
	watcher  *fsnotify.Watcher
	onReload func(*Story) // callback after successful reload
	done     chan struct{}
}

// NewHotLoader creates a HotLoader that watches the given directory.
// The onReload callback is called after each successful reload with the
// new story. It may be nil.
func NewHotLoader(storyDir string, onReload func(*Story)) (*HotLoader, error) {
	// Initial load.
	story, err := LoadStoryDir(storyDir)
	if err != nil {
		return nil, fmt.Errorf("initial story load: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}

	if err := watcher.Add(storyDir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("watch %s: %w", storyDir, err)
	}

	hl := &HotLoader{
		storyDir: storyDir,
		watcher:  watcher,
		onReload: onReload,
		done:     make(chan struct{}),
	}
	hl.story.Store(story)

	go hl.watch()

	return hl, nil
}

// Story returns the current story. This is a lock-free read via atomic.Value.
func (hl *HotLoader) Story() *Story {
	return hl.story.Load().(*Story)
}

// Close stops watching and releases resources.
func (hl *HotLoader) Close() error {
	close(hl.done)
	return hl.watcher.Close()
}

// watch is the event loop that processes filesystem events.
func (hl *HotLoader) watch() {
	for {
		select {
		case <-hl.done:
			return

		case event, ok := <-hl.watcher.Events:
			if !ok {
				return
			}

			// Only react to YAML file changes.
			if !isYAML(event.Name) {
				continue
			}

			// Only reload on write/create/rename (not chmod).
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Rename) {
				continue
			}

			log.Printf("[star/engine] detected change: %s %s", event.Op, filepath.Base(event.Name))

			story, err := LoadStoryDir(hl.storyDir)
			if err != nil {
				log.Printf("[star/engine] reload failed (keeping old story): %v", err)
				continue
			}

			hl.story.Store(story)
			log.Printf("[star/engine] story reloaded: %q (%d chapters)",
				story.Title, len(story.Chapters))

			if hl.onReload != nil {
				hl.onReload(story)
			}

		case err, ok := <-hl.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[star/engine] watcher error: %v", err)
		}
	}
}

func isYAML(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}

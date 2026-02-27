// Package wall implements a rotating lies wall.
//
// A background worker periodically scans the store for lies and pre-builds
// pages of up to PageSize lie values. Each HTTP request gets a different page
// via an atomic round-robin counter, so consecutive visitors see different
// content. When there are fewer lies than PageSize, every request returns
// the same (only) page.
package wall

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jredh-dev/nexus/services/secrets/internal/store"
)

const (
	// PageSize is the maximum number of lies per page.
	PageSize = 1000

	// RefreshInterval is how often the worker rebuilds pages.
	RefreshInterval = 5 * time.Second
)

// Wall serves pre-built pages of lies in round-robin order.
type Wall struct {
	store   *store.Store
	counter atomic.Uint64

	mu    sync.RWMutex
	pages []string // each entry is a text blob of up to PageSize lies
	total int      // total number of lies across all pages
	stop  chan struct{}
}

// New creates a new Wall and starts the background worker.
func New(s *store.Store) *Wall {
	w := &Wall{
		store: s,
		stop:  make(chan struct{}),
	}
	w.rebuild()
	go w.run()
	return w
}

// Page returns the next page of lies for the current request.
// Returns the text blob and metadata (page index, total pages, total lies).
func (w *Wall) Page() (text string, pageIdx int, totalPages int, totalLies int) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if len(w.pages) == 0 {
		return "", 0, 0, 0
	}

	idx := int(w.counter.Add(1)-1) % len(w.pages)
	return w.pages[idx], idx, len(w.pages), w.total
}

// Stop shuts down the background worker.
func (w *Wall) Stop() {
	close(w.stop)
}

func (w *Wall) run() {
	ticker := time.NewTicker(RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.rebuild()
		case <-w.stop:
			return
		}
	}
}

func (w *Wall) rebuild() {
	all := w.store.List()

	// Filter to lies only
	lies := make([]string, 0, len(all))
	for _, s := range all {
		if s.State == store.Lie {
			lies = append(lies, s.Value)
		}
	}

	// Build pages
	var pages []string
	for i := 0; i < len(lies); i += PageSize {
		end := i + PageSize
		if end > len(lies) {
			end = len(lies)
		}
		pages = append(pages, strings.Join(lies[i:end], "\n"))
	}

	w.mu.Lock()
	w.pages = pages
	w.total = len(lies)
	w.mu.Unlock()
}

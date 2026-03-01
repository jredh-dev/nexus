// Package wall implements a rotating display of exposed (non-secret) entries.
//
// A background worker periodically scans the store for entries with count > 1
// and pre-builds pages. Each HTTP request gets a different page via atomic
// round-robin, so consecutive visitors see different content.
package wall

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jredh-dev/nexus/services/secrets/internal/store"
)

const (
	PageSize        = 1000
	RefreshInterval = 5 * time.Second
)

// Wall serves pre-built pages of non-secrets in round-robin order.
type Wall struct {
	store   *store.Store
	counter atomic.Uint64

	mu    sync.RWMutex
	pages []string
	total int
	stop  chan struct{}
}

// New creates a Wall and starts the background worker.
func New(s *store.Store) *Wall {
	w := &Wall{
		store: s,
		stop:  make(chan struct{}),
	}
	w.rebuild()
	go w.run()
	return w
}

// Page returns the next page for the current request.
func (w *Wall) Page() (text string, pageIdx int, totalPages int, totalExposed int) {
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

	// Filter to non-secrets (count > 1).
	exposed := make([]string, 0, len(all))
	for _, s := range all {
		if !s.IsSecret() {
			exposed = append(exposed, s.Value)
		}
	}

	var pages []string
	for i := 0; i < len(exposed); i += PageSize {
		end := i + PageSize
		if end > len(exposed) {
			end = len(exposed)
		}
		pages = append(pages, strings.Join(exposed[i:end], "\n"))
	}

	w.mu.Lock()
	w.pages = pages
	w.total = len(exposed)
	w.mu.Unlock()
}

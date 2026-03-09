// Package events provides a rolling in-memory event buffer backed by a
// Postgres LISTEN/NOTIFY subscriber (via pgbus), and an SSE HTTP handler
// that streams the buffer to the matrix dashboard.
//
// Architecture:
//   - Buffer: a thread-safe ring buffer of the last N portal events.
//   - Subscribe goroutine: calls pgbus.Subscribe in a retry loop, appending
//     each incoming notification to the buffer.
//   - SSEHandler: accepts SSE client connections on GET /events/stream,
//     writes buffered events on connect, then streams new ones as they arrive.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jredh-dev/nexus/internal/pgbus"
)

const (
	// bufferSize is the maximum number of events held in memory.
	bufferSize = 50
	// retryDelay is how long to wait before reconnecting a dropped subscriber.
	retryDelay = 5 * time.Second
)

// Entry is one event in the buffer, decoded from a pgbus JSON notification.
type Entry struct {
	Event   string         `json:"event"`
	Service string         `json:"service"`
	Ts      string         `json:"ts"`
	Data    map[string]any `json:"data,omitempty"`
}

// Buffer is a thread-safe ring buffer of portal events.
type Buffer struct {
	mu      sync.RWMutex
	entries []Entry
	// subs is a set of subscriber channels — one per active SSE client.
	// Each channel receives newly appended entries as they arrive.
	subs map[chan Entry]struct{}
}

// NewBuffer creates an empty event buffer.
func NewBuffer() *Buffer {
	return &Buffer{
		subs: make(map[chan Entry]struct{}),
	}
}

// Append adds an entry to the tail of the ring buffer and fans it out to
// all active SSE subscribers. If the buffer exceeds bufferSize, the oldest
// entry is dropped from the head.
func (b *Buffer) Append(e Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.entries = append(b.entries, e)
	if len(b.entries) > bufferSize {
		// Drop the oldest entry.
		b.entries = b.entries[1:]
	}

	// Fan out to live SSE clients. Non-blocking send so a slow client
	// cannot stall event ingestion.
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
			// Client is too slow; skip this event for them.
		}
	}
}

// Snapshot returns a copy of the current buffer contents, oldest first.
func (b *Buffer) Snapshot() []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Entry, len(b.entries))
	copy(out, b.entries)
	return out
}

// subscribe registers a channel to receive new events.
// Returns a cancel func to deregister.
func (b *Buffer) subscribe(ch chan Entry) func() {
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}

// StartSubscriber launches a goroutine that calls pgbus.Subscribe in a retry
// loop, parsing each notification and appending it to buf.
//
// connStr is a libpq DSN for the portal database (the same DB that portal
// publishes to). It must have CONNECT + SELECT on the portal db (read-only is fine).
//
// The goroutine runs until ctx is cancelled.
func StartSubscriber(ctx context.Context, connStr string, buf *Buffer) {
	go func() {
		for {
			log.Printf("matrix/events: connecting subscriber to portal.events")
			err := pgbus.Subscribe(ctx, connStr, "portal.events", func(payload string) {
				var e Entry
				if err := json.Unmarshal([]byte(payload), &e); err != nil {
					log.Printf("matrix/events: malformed payload: %v", err)
					return
				}
				buf.Append(e)
				log.Printf("matrix/events: received %s from %s", e.Event, e.Service)
			})
			if ctx.Err() != nil {
				// Normal shutdown.
				return
			}
			// Unexpected disconnect — retry after delay.
			log.Printf("matrix/events: subscriber error: %v — retrying in %s", err, retryDelay)
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryDelay):
			}
		}
	}()
}

// SSEHandler returns an HTTP handler that streams portal events to SSE clients.
//
// On connect, all buffered events are sent immediately as JSON blobs, then new
// events are streamed as they arrive. The connection stays open until the client
// disconnects or the server shuts down.
func SSEHandler(buf *Buffer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// SSE requires flusher support.
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "SSE not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Send buffered events immediately on connect.
		snapshot := buf.Snapshot()
		for _, e := range snapshot {
			writeSSEEvent(w, e)
		}
		flusher.Flush()

		// Register for live events.
		ch := make(chan Entry, 32)
		cancel := buf.subscribe(ch)
		defer cancel()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-ch:
				writeSSEEvent(w, e)
				flusher.Flush()
			}
		}
	}
}

// writeSSEEvent writes a single SSE data line for entry e.
// Format: "data: <json>\n\n"
func writeSSEEvent(w http.ResponseWriter, e Entry) {
	b, err := json.Marshal(e)
	if err != nil {
		log.Printf("matrix/events: marshal event: %v", err)
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}

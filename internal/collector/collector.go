// SPDX-License-Identifier: AGPL-3.0-or-later

// Package collector provides a generic, concurrency-safe rolling TTL window
// for accumulating items over time. Items older than the configured TTL are
// lazily expired on Add and Snapshot, keeping memory bounded to the active
// window without requiring a background goroutine.
//
// Typical usage:
//
//	c := collector.New[MyEvent](15 * time.Minute)
//	c.Add(event)
//	items := c.Snapshot()
//	avg := collector.Avg(func(e MyEvent) float64 { return e.Value })(items)
package collector

import (
	"sync"
	"time"
)

// timestamped wraps an item with the wall-clock time it was added.
type timestamped[T any] struct {
	t    time.Time
	item T
}

// Collector holds a rolling TTL window of items.
// Safe for concurrent use.
// Items older than TTL are expired on Add and Snapshot.
type Collector[T any] struct {
	mu    sync.RWMutex
	items []timestamped[T]
	ttl   time.Duration
}

// New creates a Collector with the given TTL window (e.g. 15*time.Minute).
// Items added more than ttl ago are considered stale and are not returned
// by Snapshot or counted by Len.
func New[T any](ttl time.Duration) *Collector[T] {
	return &Collector[T]{ttl: ttl}
}

// expire removes items older than TTL from the front of the slice.
// Must be called with mu held for writing.
func (c *Collector[T]) expire(now time.Time) {
	cutoff := now.Add(-c.ttl)
	// Items are appended in chronological order, so the oldest are at index 0.
	i := 0
	for i < len(c.items) && c.items[i].t.Before(cutoff) {
		i++
	}
	if i > 0 {
		// Slice off the expired prefix; copy remaining to avoid memory leak.
		c.items = append([]timestamped[T]{}, c.items[i:]...)
	}
}

// Add appends item to the window, expiring stale items first.
func (c *Collector[T]) Add(item T) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expire(now)
	c.items = append(c.items, timestamped[T]{t: now, item: item})
}

// Snapshot returns all non-expired items currently in the window.
// The returned slice is a copy; callers may modify it freely.
func (c *Collector[T]) Snapshot() []T {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expire(now)
	out := make([]T, len(c.items))
	for i, ts := range c.items {
		out[i] = ts.item
	}
	return out
}

// Len returns the number of non-expired items currently in the window.
func (c *Collector[T]) Len() int {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expire(now)
	return len(c.items)
}

// Clear empties the window, discarding all items regardless of age.
func (c *Collector[T]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = c.items[:0]
}

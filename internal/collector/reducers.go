// SPDX-License-Identifier: AGPL-3.0-or-later

// Package collector — reducer functions.
// Reducers are plain functions that accept a []T and return an aggregate.
// They compose naturally with Collector.Snapshot():
//
//	snap := c.Snapshot()
//	avg  := collector.Avg(func(e Event) float64 { return e.Latency })(snap)
//	rate := collector.Rate[Event](15 * time.Minute)(snap)
package collector

import (
	"sort"
	"time"
)

// Avg returns a reducer that computes the arithmetic mean of field(item)
// across all items. Returns 0 for an empty slice.
func Avg[T any](field func(T) float64) func([]T) float64 {
	return func(items []T) float64 {
		if len(items) == 0 {
			return 0
		}
		sum := 0.0
		for _, item := range items {
			sum += field(item)
		}
		return sum / float64(len(items))
	}
}

// Median returns a reducer that computes the median of field(item) across
// all items. For an even-length slice the lower middle value is returned
// (no interpolation). Returns 0 for an empty slice.
func Median[T any](field func(T) float64) func([]T) float64 {
	return func(items []T) float64 {
		if len(items) == 0 {
			return 0
		}
		// Copy before sorting so we don't mutate the caller's slice.
		vals := make([]float64, len(items))
		for i, item := range items {
			vals[i] = field(item)
		}
		sort.Float64s(vals)
		return vals[len(vals)/2]
	}
}

// Count returns a reducer that reports the number of items in the slice.
func Count[T any]() func([]T) int {
	return func(items []T) int {
		return len(items)
	}
}

// Rate returns a reducer that computes items-per-minute over the given
// window duration. window should match the Collector TTL so that the rate
// reflects the full observation period even when the window is partially
// empty.
//
//	rate := collector.Rate[Event](15 * time.Minute)(c.Snapshot())
func Rate[T any](window time.Duration) func([]T) float64 {
	return func(items []T) float64 {
		minutes := window.Minutes()
		if minutes == 0 {
			return 0
		}
		return float64(len(items)) / minutes
	}
}

// Last returns a reducer that returns a pointer to the last item in the
// slice, or nil if the slice is empty. The pointer refers to a copy of
// the item, not the original slice element.
func Last[T any]() func([]T) *T {
	return func(items []T) *T {
		if len(items) == 0 {
			return nil
		}
		v := items[len(items)-1]
		return &v
	}
}

// Filter returns a reducer that produces a new slice containing only the
// items for which pred returns true. The result is always a non-nil slice.
func Filter[T any](pred func(T) bool) func([]T) []T {
	return func(items []T) []T {
		out := make([]T, 0, len(items))
		for _, item := range items {
			if pred(item) {
				out = append(out, item)
			}
		}
		return out
	}
}

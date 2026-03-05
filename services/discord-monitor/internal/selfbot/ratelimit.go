package selfbot

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimiter tracks per-route rate limits using Discord's response headers.
// Discord returns X-RateLimit-Remaining and X-RateLimit-Reset on every
// response. We use these to proactively wait before sending requests
// that would be rejected with 429 Too Many Requests.
//
// Routes are keyed by a string like "GET /guilds" — the caller decides
// the granularity. Discord's actual rate limit buckets are more nuanced
// (they use X-RateLimit-Bucket), but route-level tracking is sufficient
// for our low-frequency polling use case.
type RateLimiter struct {
	buckets sync.Map // map[string]*bucket
}

// bucket tracks the rate limit state for a single route.
type bucket struct {
	remaining int       // requests remaining in the current window
	resetAt   time.Time // when the rate limit window resets
	mu        sync.Mutex
}

// NewRateLimiter creates a RateLimiter with no initial state.
// Buckets are created lazily when Update is called with response headers.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{}
}

// Wait blocks until the rate limit for the given route allows a request.
// If no rate limit data exists for the route (first request), it returns
// immediately. Returns ctx.Err() if the context is cancelled while waiting.
func (r *RateLimiter) Wait(ctx context.Context, route string) error {
	val, ok := r.buckets.Load(route)
	if !ok {
		// No rate limit data yet — this is the first request to this route.
		return nil
	}

	b := val.(*bucket)
	b.mu.Lock()
	remaining := b.remaining
	resetAt := b.resetAt
	b.mu.Unlock()

	// If we have remaining requests, proceed immediately.
	if remaining > 0 {
		return nil
	}

	// If the reset time has already passed, the window has rolled over.
	now := time.Now()
	if now.After(resetAt) {
		return nil
	}

	// We're rate-limited. Wait until the reset time or context cancellation.
	delay := time.Until(resetAt)
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Update reads Discord rate limit headers from a response and updates
// the bucket for the given route. Called after every successful API request.
//
// Discord headers used:
//   - X-RateLimit-Remaining: requests left in the current window
//   - X-RateLimit-Reset: Unix timestamp (float seconds) when the window resets
//   - X-RateLimit-Reset-After: seconds until the window resets (alternative)
func (r *RateLimiter) Update(route string, resp *http.Response) {
	remaining := -1
	if v := resp.Header.Get("X-RateLimit-Remaining"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			remaining = n
		}
	}

	var resetAt time.Time

	// Prefer X-RateLimit-Reset (absolute Unix timestamp) over Reset-After
	// (relative seconds). The absolute timestamp avoids clock skew between
	// when the response was sent and when we process it.
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if ts, err := strconv.ParseFloat(v, 64); err == nil {
			sec := int64(ts)
			nsec := int64((ts - float64(sec)) * 1e9)
			resetAt = time.Unix(sec, nsec)
		}
	} else if v := resp.Header.Get("X-RateLimit-Reset-After"); v != "" {
		if secs, err := strconv.ParseFloat(v, 64); err == nil {
			resetAt = time.Now().Add(time.Duration(secs * float64(time.Second)))
		}
	}

	// Only update if we got usable data from the headers.
	if remaining < 0 && resetAt.IsZero() {
		return
	}

	val, _ := r.buckets.LoadOrStore(route, &bucket{})
	b := val.(*bucket)
	b.mu.Lock()
	if remaining >= 0 {
		b.remaining = remaining
	}
	if !resetAt.IsZero() {
		b.resetAt = resetAt
	}
	b.mu.Unlock()
}

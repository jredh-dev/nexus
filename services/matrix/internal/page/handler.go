// Package page provides the HTTP handler for the matrix dev hub dashboard.
// It serves a single static HTML page embedded in the binary via go:embed.
//
// The index.html file lives in this package directory so that go:embed can
// reference it with a simple relative path (no path traversal needed).
package page

import (
	_ "embed"
	"net/http"
)

// html holds the embedded dashboard page.
//
//go:embed index.html
var html []byte

// Handler serves the embedded index.html at the root route.
// The page is fully static — no templating, no JS, no CDN dependencies.
func Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Allow browsers to cache for 60 seconds — links and badges don't change often.
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.WriteHeader(http.StatusOK)
	w.Write(html) //nolint:errcheck
}

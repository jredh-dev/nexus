// Package templates provides embedded HTML templates for the portal web UI.
package templates

import "embed"

//go:embed *.html partials/*.html
var FS embed.FS

// Package templates provides embedded HTML templates for the portal web UI.
// Only giveaway templates remain; all other pages are served by the Astro frontend.
package templates

import "embed"

//go:embed *.html
var FS embed.FS

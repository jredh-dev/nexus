// Package selfbot provides an HTTP client for Discord's user-facing REST API.
//
// This package mimics browser requests (specifically Safari on macOS) to
// access Discord as a regular user account. It includes browser-accurate
// headers, per-route rate limiting using Discord's response headers, and
// read-only endpoint wrappers.
//
// WARNING: Using a user token to automate Discord actions violates Discord's
// Terms of Service. This is intended for personal monitoring of servers you
// own or have permission to monitor. Use at your own risk.
package selfbot

import "net/http"

// HeaderProfile contains HTTP headers that mimic a real browser session.
// Discord uses these headers (especially User-Agent and Sec-Fetch-*)
// to distinguish between browsers and bots. Sending accurate headers
// reduces the chance of the user token being flagged.
type HeaderProfile struct {
	UserAgent      string
	AcceptLanguage string
	Accept         string
	Origin         string
	Referer        string
	SecFetchDest   string
	SecFetchMode   string
	SecFetchSite   string
	DNT            string
}

// DefaultProfile returns a HeaderProfile mimicking Safari on macOS.
// These values are based on a recent Safari release and should be
// updated periodically to match current browser versions.
func DefaultProfile() HeaderProfile {
	return HeaderProfile{
		UserAgent:      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.6 Safari/605.1.15",
		AcceptLanguage: "en-US,en;q=0.9",
		Accept:         "*/*",
		Origin:         "https://discord.com",
		Referer:        "https://discord.com/channels/@me",
		SecFetchDest:   "empty",
		SecFetchMode:   "cors",
		SecFetchSite:   "same-origin",
		DNT:            "1",
	}
}

// Apply sets all header profile values on the given HTTP request.
// This should be called on every outgoing request to Discord's API
// to maintain consistent browser fingerprinting.
func (p HeaderProfile) Apply(req *http.Request) {
	req.Header.Set("User-Agent", p.UserAgent)
	req.Header.Set("Accept-Language", p.AcceptLanguage)
	req.Header.Set("Accept", p.Accept)
	req.Header.Set("Origin", p.Origin)
	req.Header.Set("Referer", p.Referer)
	req.Header.Set("Sec-Fetch-Dest", p.SecFetchDest)
	req.Header.Set("Sec-Fetch-Mode", p.SecFetchMode)
	req.Header.Set("Sec-Fetch-Site", p.SecFetchSite)
	req.Header.Set("DNT", p.DNT)
}

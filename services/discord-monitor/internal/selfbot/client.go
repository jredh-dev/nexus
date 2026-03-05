package selfbot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// defaultBaseURL is the Discord user API endpoint.
	// v9 is the latest stable version used by the web client.
	defaultBaseURL = "https://discord.com/api/v9"

	// defaultTimeout is the HTTP client timeout for individual requests.
	// Discord's API is generally fast, but large guild responses can take
	// a few seconds.
	defaultTimeout = 30 * time.Second
)

// Client is an HTTP client for Discord's user-facing REST API.
// It applies browser headers, manages authentication via a user token,
// and respects per-route rate limits parsed from Discord response headers.
type Client struct {
	http    *http.Client
	token   string
	headers HeaderProfile
	limiter *RateLimiter
	baseURL string
}

// Option configures a Client during construction.
type Option func(*Client)

// WithHTTPClient overrides the default http.Client.
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) { cl.http = c }
}

// WithHeaders overrides the default browser header profile.
func WithHeaders(h HeaderProfile) Option {
	return func(cl *Client) { cl.headers = h }
}

// WithBaseURL overrides the Discord API base URL (for testing).
func WithBaseURL(url string) Option {
	return func(cl *Client) { cl.baseURL = url }
}

// New creates a selfbot Client with the given user token.
// The token is sent as-is in the Authorization header (no "Bot " prefix,
// which is the key difference from bot tokens).
func New(token string, opts ...Option) *Client {
	c := &Client{
		http: &http.Client{
			Timeout: defaultTimeout,
		},
		token:   token,
		headers: DefaultProfile(),
		limiter: NewRateLimiter(),
		baseURL: defaultBaseURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// do executes an HTTP request against the Discord API.
// It handles: rate limit waiting, header application, auth, and rate limit
// updates from the response. The caller is responsible for closing the
// response body.
//
// The route parameter for rate limiting is derived from method + path,
// e.g. "GET /users/@me". The caller provides the path relative to baseURL
// (e.g. "/users/@me").
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	route := method + " " + path

	// Wait for rate limit clearance before sending the request.
	if err := c.limiter.Wait(ctx, route); err != nil {
		return nil, fmt.Errorf("rate limit wait for %s: %w", route, err)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request %s %s: %w", method, path, err)
	}

	// Apply browser headers to look like a real Discord web client.
	c.headers.Apply(req)

	// User token auth — no "Bot " prefix. This is what Discord's web client
	// sends when you're logged in.
	req.Header.Set("Authorization", c.token)

	// Set content type for requests with a body (POST/PATCH/PUT).
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request %s %s: %w", method, path, err)
	}

	// Update rate limit state from Discord's response headers.
	// This must happen before we check the status code — even 429 responses
	// include rate limit headers telling us when to retry.
	c.limiter.Update(route, resp)

	return resp, nil
}

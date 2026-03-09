// Package opencode is an HTTP client for the OpenCode headless server.
//
// It uses the OpenCode REST API to create sessions and send messages.
// Each call to Send creates a fresh session, sends the message (with context
// prepended), waits for the synchronous response, and returns the text.
//
// Auth: HTTP basic auth, username "opencode", password from OPENCODE_PASSWORD.
// URL:  OPENCODE_URL (default http://opencode:4096).
package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to the OpenCode headless server.
type Client struct {
	url      string
	username string
	password string
	http     *http.Client
}

// New creates a Client. url should be e.g. "http://opencode:4096".
// password is OPENCODE_SERVER_PASSWORD on the server.
func New(url, password string) *Client {
	return &Client{
		url:      strings.TrimRight(url, "/"),
		username: "opencode",
		password: password,
		http: &http.Client{
			Timeout: 10 * time.Minute, // model responses can be slow
		},
	}
}

// sessionResp is the minimal shape returned by POST /session.
type sessionResp struct {
	ID string `json:"id"`
}

// messageReq is the body for POST /session/:id/message.
type messageReq struct {
	Parts []messagePart `json:"parts"`
}

// messagePart represents a text content part.
type messagePart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// messageResp is the minimal shape returned by POST /session/:id/message.
type messageResp struct {
	Parts []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"parts"`
}

// Send creates a new session, sends text as a user message, and returns the
// assistant's response text. context is prepended to the message (AGENTS.md
// hierarchy). An empty response is valid (model may reply with tool calls only).
func (c *Client) Send(context, message string) (string, error) {
	// 1. Create session.
	sessionID, err := c.createSession()
	if err != nil {
		return "", fmt.Errorf("opencode create session: %w", err)
	}

	// 2. Build prompt: context (AGENTS.md hierarchy) + message body.
	prompt := message
	if context != "" {
		prompt = context + "\n\n---\n\n" + message
	}

	// 3. Send message (synchronous — waits for full response).
	reply, err := c.sendMessage(sessionID, prompt)
	if err != nil {
		return "", fmt.Errorf("opencode send message: %w", err)
	}

	return reply, nil
}

// Health checks whether the server is reachable.
func (c *Client) Health() error {
	req, err := http.NewRequest("GET", c.url+"/global/health", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.password)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("opencode health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("opencode health: status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) createSession() (string, error) {
	req, err := http.NewRequest("POST", c.url+"/session", bytes.NewBufferString("{}"))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create session status %d: %s", resp.StatusCode, string(body))
	}

	var s sessionResp
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return "", fmt.Errorf("decode session: %w", err)
	}
	if s.ID == "" {
		return "", fmt.Errorf("session ID empty in response")
	}
	return s.ID, nil
}

func (c *Client) sendMessage(sessionID, text string) (string, error) {
	body := messageReq{
		Parts: []messagePart{
			{Type: "text", Text: text},
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/session/%s/message", c.url, sessionID)
	req, err := http.NewRequest("POST", url, bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("send message status %d: %s", resp.StatusCode, string(b))
	}

	var mr messageResp
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return "", fmt.Errorf("decode message response: %w", err)
	}

	// Collect all text parts into a single response.
	var sb strings.Builder
	for _, p := range mr.Parts {
		if p.Type == "text" && p.Text != "" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String(), nil
}

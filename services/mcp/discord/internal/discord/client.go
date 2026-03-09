// Package discord provides a Discord webhook client.
// It sends structured event notifications as embeds.
// Auth: reads DISCORD_WEBHOOK_URL from the environment.
package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Event types.
const (
	EventDeploy  = "deploy"
	EventTest    = "test"
	EventInstall = "install"
	EventBuild   = "build"
)

// Status values.
const (
	StatusSuccess = "success"
	StatusFailure = "failure"
	StatusPending = "pending"
)

// Notification holds the data for a single event notification.
type Notification struct {
	Event   string // deploy, test, install, build
	Service string // service name (e.g. hermit, secrets, github-mcp)
	Version string // git SHA, tag, or version string (optional)
	Env     string // dev, prod, local (optional)
	Status  string // success, failure, pending
	Message string // optional freeform message override
}

// embed color by status.
func embedColor(status string) int {
	switch status {
	case StatusSuccess:
		return 0x2ECC71 // green
	case StatusFailure:
		return 0xE74C3C // red
	case StatusPending:
		return 0xF39C12 // yellow
	default:
		return 0x95A5A6 // grey
	}
}

// statusLabel returns a readable status label.
func statusLabel(status string) string {
	switch status {
	case StatusSuccess:
		return "OK"
	case StatusFailure:
		return "FAILED"
	case StatusPending:
		return "PENDING"
	default:
		return status
	}
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordEmbed struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Color       int            `json:"color"`
	Fields      []discordField `json:"fields,omitempty"`
	Timestamp   string         `json:"timestamp"`
}

type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

// Send posts a notification to webhookURL.
func Send(webhookURL string, n Notification) error {
	if webhookURL == "" {
		return fmt.Errorf("discord webhook URL is empty")
	}

	var fields []discordField
	if n.Env != "" {
		fields = append(fields, discordField{Name: "Environment", Value: fmt.Sprintf("`%s`", n.Env), Inline: true})
	}
	if n.Version != "" {
		fields = append(fields, discordField{Name: "Version", Value: fmt.Sprintf("`%s`", n.Version), Inline: true})
	}
	fields = append(fields, discordField{Name: "Status", Value: statusLabel(n.Status), Inline: true})

	// Build title and description.
	eventLabel := strings.ToUpper(n.Event[:1]) + n.Event[1:]
	title := fmt.Sprintf("%s: %s", eventLabel, n.Service)

	desc := fmt.Sprintf("[%s] %s", statusLabel(n.Status), n.Event)
	if n.Message != "" {
		desc = n.Message
	}

	payload := discordPayload{
		Embeds: []discordEmbed{
			{
				Title:       title,
				Description: desc,
				Color:       embedColor(n.Status),
				Fields:      fields,
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body)) //nolint:noctx
	if err != nil {
		return fmt.Errorf("post to discord: %w", err)
	}
	defer resp.Body.Close()

	// Discord returns 204 No Content on success.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord returned status %d", resp.StatusCode)
	}
	return nil
}

// SendFromEnv reads DISCORD_WEBHOOK_URL and DISCORD_ENABLED from the environment
// and calls Send. Returns nil without sending if DISCORD_ENABLED != "true".
func SendFromEnv(n Notification) error {
	if os.Getenv("DISCORD_ENABLED") != "true" {
		return nil
	}
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		return fmt.Errorf("DISCORD_WEBHOOK_URL not set")
	}
	return Send(webhookURL, n)
}

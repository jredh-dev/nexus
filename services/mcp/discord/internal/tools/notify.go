// Package tools defines MCP tool handlers for the Discord MCP service.
package tools

import (
	"encoding/json"
	"fmt"

	"github.com/jredh-dev/nexus/services/mcp/discord/internal/discord"
	"github.com/jredh-dev/nexus/services/mcp/discord/internal/mcp"
)

// RegisterAll registers all Discord MCP tools with the server.
func RegisterAll(s *mcp.Server) {
	s.RegisterTool(mcp.Tool{
		Name:        "notify_discord",
		Description: "Send a deploy/CI event notification to Discord via webhook.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"event":   {Type: "string", Description: "Event type", Enum: []string{"deploy", "test", "install", "build"}},
				"service": {Type: "string", Description: "Service name (e.g. hermit, secrets, github-mcp)"},
				"status":  {Type: "string", Description: "Outcome", Enum: []string{"success", "failure", "pending"}, Default: "success"},
				"version": {Type: "string", Description: "Version/SHA (optional)"},
				"env":     {Type: "string", Description: "Environment, e.g. dev, prod, local (optional)"},
				"message": {Type: "string", Description: "Freeform message override (optional)"},
			},
			Required: []string{"event", "service"},
		},
	}, handleNotify)
}

func handleNotify(args json.RawMessage) (*mcp.ToolCallResult, error) {
	var p struct {
		Event   string `json:"event"`
		Service string `json:"service"`
		Status  string `json:"status"`
		Version string `json:"version"`
		Env     string `json:"env"`
		Message string `json:"message"`
	}
	if err := parseArgs(args, &p); err != nil {
		return nil, err
	}
	if p.Event == "" {
		return nil, errMissing("event")
	}
	if p.Service == "" {
		return nil, errMissing("service")
	}
	if p.Status == "" {
		p.Status = discord.StatusSuccess
	}

	n := discord.Notification{
		Event:   p.Event,
		Service: p.Service,
		Version: p.Version,
		Env:     p.Env,
		Status:  p.Status,
		Message: p.Message,
	}

	if err := discord.SendFromEnv(n); err != nil {
		return nil, err
	}
	return textResult("Notification sent"), nil
}

// jsonResult marshals v as indented JSON and wraps it in a ToolCallResult.
func jsonResult(v any) (*mcp.ToolCallResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return &mcp.ToolCallResult{
		Content: []mcp.ContentBlock{mcp.TextContent(string(data))},
	}, nil
}

// textResult wraps a plain text string in a ToolCallResult.
func textResult(text string) *mcp.ToolCallResult {
	return &mcp.ToolCallResult{
		Content: []mcp.ContentBlock{mcp.TextContent(text)},
	}
}

// parseArgs unmarshals raw JSON arguments into dst.
func parseArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// errMissing returns a standard missing-parameter error.
func errMissing(param string) error {
	return fmt.Errorf("missing required parameter: %s", param)
}

// Package tools defines MCP tool handlers for the Discord MCP service.
package tools

import (
	"encoding/json"

	"github.com/jredh-dev/nexus/internal/mcp"
	"github.com/jredh-dev/nexus/services/mcp/discord/internal/discord"
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
	if err := mcp.ParseArgs(args, &p); err != nil {
		return nil, err
	}
	if p.Event == "" {
		return nil, mcp.ErrMissing("event")
	}
	if p.Service == "" {
		return nil, mcp.ErrMissing("service")
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
	return mcp.TextResult("Notification sent"), nil
}

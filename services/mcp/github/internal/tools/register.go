// Package tools defines MCP tool handlers for the GitHub MCP service.
package tools

import (
	"encoding/json"
	"fmt"

	"github.com/jredh-dev/nexus/services/mcp/github/internal/mcp"
)

// RegisterAll registers all GitHub MCP tools with the server.
func RegisterAll(s *mcp.Server) {
	registerPRTools(s)
	registerCheckTools(s)
	registerIssueTools(s)
	registerRCTools(s)
}

// jsonResult marshals v as indented JSON and wraps it in a successful ToolCallResult.
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

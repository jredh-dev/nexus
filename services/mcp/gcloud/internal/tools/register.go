// Package tools defines MCP tool handlers for the gcloud MCP service.
package tools

import (
	"encoding/json"

	"github.com/jredh-dev/nexus/internal/mcp"
)

// RegisterAll registers all gcloud MCP tools with the server.
func RegisterAll(s *mcp.Server) {
	registerCloudRunTools(s)
	registerLoggingTools(s)
	registerTerraformTools(s)
}

// jsonResult delegates to the shared mcp helper.
func jsonResult(v any) (*mcp.ToolCallResult, error) { return mcp.JSONResult(v) }

// textResult delegates to the shared mcp helper.
func textResult(text string) *mcp.ToolCallResult { return mcp.TextResult(text) }

// parseArgs delegates to the shared mcp helper.
func parseArgs(raw json.RawMessage, dst any) error { return mcp.ParseArgs(raw, dst) }

// errMissing delegates to the shared mcp helper.
func errMissing(param string) error { return mcp.ErrMissing(param) }

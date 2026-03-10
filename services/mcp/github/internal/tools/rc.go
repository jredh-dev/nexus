package tools

import (
	"encoding/json"

	"github.com/jredh-dev/nexus/internal/mcp"
	gh "github.com/jredh-dev/nexus/services/mcp/github/internal/github"
)

func registerRCTools(s *mcp.Server) {
	s.RegisterTool(mcp.Tool{
		Name:        "rc_list",
		Description: "List all rc/* branches for a repository with their PR status.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"repo": {Type: "string", Description: "Repository in owner/repo format"},
			},
			Required: []string{"repo"},
		},
	}, handleRCList)

	s.RegisterTool(mcp.Tool{
		Name:        "rc_current",
		Description: "Get the current (latest) rc/* branch name for a repository.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"repo": {Type: "string", Description: "Repository in owner/repo format"},
			},
			Required: []string{"repo"},
		},
	}, handleRCCurrent)

	s.RegisterTool(mcp.Tool{
		Name:        "rc_create",
		Description: "Create the next rc/vX.Y.Z branch on GitHub. Auto-increments the minor version if no version is specified.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"repo":    {Type: "string", Description: "Repository in owner/repo format"},
				"version": {Type: "string", Description: "Explicit version string, e.g. v1.3.0 (optional — auto-increments if omitted)"},
			},
			Required: []string{"repo"},
		},
	}, handleRCCreate)
}

func handleRCList(args json.RawMessage) (*mcp.ToolCallResult, error) {
	var p struct {
		Repo string `json:"repo"`
	}
	if err := parseArgs(args, &p); err != nil {
		return nil, err
	}
	if p.Repo == "" {
		return nil, errMissing("repo")
	}
	client, err := gh.NewClientFromEnv()
	if err != nil {
		return nil, err
	}
	result, err := client.ListRCBranches(p.Repo)
	if err != nil {
		return nil, err
	}
	return jsonResult(result)
}

func handleRCCurrent(args json.RawMessage) (*mcp.ToolCallResult, error) {
	var p struct {
		Repo string `json:"repo"`
	}
	if err := parseArgs(args, &p); err != nil {
		return nil, err
	}
	if p.Repo == "" {
		return nil, errMissing("repo")
	}
	client, err := gh.NewClientFromEnv()
	if err != nil {
		return nil, err
	}
	result, err := client.ListRCBranches(p.Repo)
	if err != nil {
		return nil, err
	}
	return textResult(result.Current), nil
}

func handleRCCreate(args json.RawMessage) (*mcp.ToolCallResult, error) {
	var p struct {
		Repo    string `json:"repo"`
		Version string `json:"version"`
	}
	if err := parseArgs(args, &p); err != nil {
		return nil, err
	}
	if p.Repo == "" {
		return nil, errMissing("repo")
	}
	client, err := gh.NewClientFromEnv()
	if err != nil {
		return nil, err
	}
	result, err := client.CreateRCBranch(p.Repo, p.Version)
	if err != nil {
		return nil, err
	}
	return jsonResult(result)
}

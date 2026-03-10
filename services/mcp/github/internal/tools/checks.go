package tools

import (
	"encoding/json"
	"strconv"

	"github.com/jredh-dev/nexus/internal/mcp"
	gh "github.com/jredh-dev/nexus/services/mcp/github/internal/github"
)

func registerCheckTools(s *mcp.Server) {
	s.RegisterTool(mcp.Tool{
		Name:        "pr_checks",
		Description: "Get CI check status for a PR or a branch ref.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"repo":   {Type: "string", Description: "Repository in owner/repo format"},
				"number": {Type: "string", Description: "PR number (use this OR ref, not both)"},
				"ref":    {Type: "string", Description: "Branch name (use this OR number, not both)"},
			},
			Required: []string{"repo"},
		},
	}, handlePRChecks)
}

func handlePRChecks(args json.RawMessage) (*mcp.ToolCallResult, error) {
	var p struct {
		Repo   string `json:"repo"`
		Number string `json:"number"`
		Ref    string `json:"ref"`
	}
	if err := parseArgs(args, &p); err != nil {
		return nil, err
	}
	if p.Repo == "" {
		return nil, errMissing("repo")
	}
	if p.Number == "" && p.Ref == "" {
		return nil, errMissing("number or ref")
	}

	client, err := gh.NewClientFromEnv()
	if err != nil {
		return nil, err
	}

	var result *gh.ChecksResult
	if p.Number != "" {
		num, err := strconv.Atoi(p.Number)
		if err != nil {
			return nil, err
		}
		result, err = client.GetChecksForPR(p.Repo, num)
		if err != nil {
			return nil, err
		}
	} else {
		result, err = client.GetChecksForRef(p.Repo, p.Ref)
		if err != nil {
			return nil, err
		}
	}
	return jsonResult(result)
}

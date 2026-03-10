package tools

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jredh-dev/nexus/internal/mcp"
	gh "github.com/jredh-dev/nexus/services/mcp/github/internal/github"
)

func registerIssueTools(s *mcp.Server) {
	s.RegisterTool(mcp.Tool{
		Name:        "issue_list",
		Description: "List open issues for a GitHub repository.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"repo": {Type: "string", Description: "Repository in owner/repo format"},
			},
			Required: []string{"repo"},
		},
	}, handleIssueList)

	s.RegisterTool(mcp.Tool{
		Name:        "issue_create",
		Description: "Create a new GitHub issue.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"repo":   {Type: "string", Description: "Repository in owner/repo format"},
				"title":  {Type: "string", Description: "Issue title"},
				"body":   {Type: "string", Description: "Issue body (markdown)"},
				"labels": {Type: "string", Description: "Comma-separated label names (optional)"},
			},
			Required: []string{"repo", "title"},
		},
	}, handleIssueCreate)

	s.RegisterTool(mcp.Tool{
		Name:        "issue_label",
		Description: "Add or remove labels on a GitHub issue.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"repo":   {Type: "string", Description: "Repository in owner/repo format"},
				"number": {Type: "string", Description: "Issue number"},
				"add":    {Type: "string", Description: "Comma-separated labels to add"},
				"remove": {Type: "string", Description: "Comma-separated labels to remove"},
			},
			Required: []string{"repo", "number"},
		},
	}, handleIssueLabel)
}

func handleIssueList(args json.RawMessage) (*mcp.ToolCallResult, error) {
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
	result, err := client.ListIssues(p.Repo)
	if err != nil {
		return nil, err
	}
	return jsonResult(result)
}

func handleIssueCreate(args json.RawMessage) (*mcp.ToolCallResult, error) {
	var p struct {
		Repo   string `json:"repo"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Labels string `json:"labels"`
	}
	if err := parseArgs(args, &p); err != nil {
		return nil, err
	}
	if p.Repo == "" {
		return nil, errMissing("repo")
	}
	if p.Title == "" {
		return nil, errMissing("title")
	}

	var labels []string
	if p.Labels != "" {
		for _, l := range strings.Split(p.Labels, ",") {
			if l = strings.TrimSpace(l); l != "" {
				labels = append(labels, l)
			}
		}
	}

	client, err := gh.NewClientFromEnv()
	if err != nil {
		return nil, err
	}
	result, err := client.CreateIssue(p.Repo, p.Title, p.Body, labels)
	if err != nil {
		return nil, err
	}
	return jsonResult(result)
}

func handleIssueLabel(args json.RawMessage) (*mcp.ToolCallResult, error) {
	var p struct {
		Repo   string `json:"repo"`
		Number string `json:"number"`
		Add    string `json:"add"`
		Remove string `json:"remove"`
	}
	if err := parseArgs(args, &p); err != nil {
		return nil, err
	}
	if p.Repo == "" {
		return nil, errMissing("repo")
	}
	if p.Number == "" {
		return nil, errMissing("number")
	}
	num, err := strconv.Atoi(p.Number)
	if err != nil {
		return nil, err
	}

	client, err := gh.NewClientFromEnv()
	if err != nil {
		return nil, err
	}

	// Add labels first.
	if p.Add != "" {
		var labels []string
		for _, l := range strings.Split(p.Add, ",") {
			if l = strings.TrimSpace(l); l != "" {
				labels = append(labels, l)
			}
		}
		if err := client.AddIssueLabels(p.Repo, num, labels); err != nil {
			return nil, err
		}
	}

	// Then remove labels.
	if p.Remove != "" {
		for _, l := range strings.Split(p.Remove, ",") {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			if err := client.RemoveIssueLabel(p.Repo, num, l); err != nil {
				return nil, err
			}
		}
	}

	return textResult("Labels updated"), nil
}

package tools

import (
	"encoding/json"
	"strconv"

	"github.com/jredh-dev/nexus/internal/mcp"
	gh "github.com/jredh-dev/nexus/services/mcp/github/internal/github"
)

func registerPRTools(s *mcp.Server) {
	s.RegisterTool(mcp.Tool{
		Name:        "pr_list",
		Description: "List open pull requests for a GitHub repository.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"repo": {Type: "string", Description: "Repository in owner/repo format (e.g. jredh-dev/nexus)"},
			},
			Required: []string{"repo"},
		},
	}, handlePRList)

	s.RegisterTool(mcp.Tool{
		Name:        "pr_get",
		Description: "Get PR details: title, body, diff, CI check status, review status, merge conflicts.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"repo":   {Type: "string", Description: "Repository in owner/repo format"},
				"number": {Type: "string", Description: "PR number"},
			},
			Required: []string{"repo", "number"},
		},
	}, handlePRGet)

	s.RegisterTool(mcp.Tool{
		Name:        "pr_create",
		Description: "Open a new pull request on GitHub.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"repo":  {Type: "string", Description: "Repository in owner/repo format"},
				"title": {Type: "string", Description: "PR title"},
				"body":  {Type: "string", Description: "PR description (markdown)"},
				"head":  {Type: "string", Description: "Head branch name"},
				"base":  {Type: "string", Description: "Base branch name (default: main)", Default: "main"},
			},
			Required: []string{"repo", "title", "head"},
		},
	}, handlePRCreate)

	s.RegisterTool(mcp.Tool{
		Name:        "pr_merge",
		Description: "Merge a pull request. Defaults to squash merge.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"repo":   {Type: "string", Description: "Repository in owner/repo format"},
				"number": {Type: "string", Description: "PR number"},
				"method": {Type: "string", Description: "Merge method", Enum: []string{"squash", "merge", "rebase"}, Default: "squash"},
			},
			Required: []string{"repo", "number"},
		},
	}, handlePRMerge)
}

func handlePRList(args json.RawMessage) (*mcp.ToolCallResult, error) {
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
	result, err := client.ListPRs(p.Repo)
	if err != nil {
		return nil, err
	}
	return jsonResult(result)
}

func handlePRGet(args json.RawMessage) (*mcp.ToolCallResult, error) {
	var p struct {
		Repo   string `json:"repo"`
		Number string `json:"number"`
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
	result, err := client.GetPR(p.Repo, num)
	if err != nil {
		return nil, err
	}
	return jsonResult(result)
}

func handlePRCreate(args json.RawMessage) (*mcp.ToolCallResult, error) {
	var p struct {
		Repo  string `json:"repo"`
		Title string `json:"title"`
		Body  string `json:"body"`
		Head  string `json:"head"`
		Base  string `json:"base"`
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
	if p.Head == "" {
		return nil, errMissing("head")
	}
	if p.Base == "" {
		p.Base = "main"
	}
	client, err := gh.NewClientFromEnv()
	if err != nil {
		return nil, err
	}
	result, err := client.CreatePR(p.Repo, p.Title, p.Body, p.Head, p.Base)
	if err != nil {
		return nil, err
	}
	return jsonResult(result)
}

func handlePRMerge(args json.RawMessage) (*mcp.ToolCallResult, error) {
	var p struct {
		Repo   string `json:"repo"`
		Number string `json:"number"`
		Method string `json:"method"`
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
	result, err := client.MergePR(p.Repo, num, p.Method)
	if err != nil {
		return nil, err
	}
	return jsonResult(result)
}

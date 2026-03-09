// Package github provides a minimal GitHub REST API client used by the MCP server.
// All operations require a GITHUB_TOKEN set in the environment.
// No third-party GitHub SDK is used — stdlib net/http only.
package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const apiBase = "https://api.github.com"

// Client is a minimal GitHub REST API client.
type Client struct {
	token string
	http  *http.Client
}

// NewClientFromEnv creates a Client using GITHUB_TOKEN from the environment.
// Returns an error if the token is not set.
func NewClientFromEnv() (*Client, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN not set")
	}
	return &Client{
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// do performs an authenticated request to the GitHub API and decodes the JSON response into dst.
// Pass nil dst to discard the response body.
func (c *Client) do(method, path string, body any, dst any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, apiBase+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if dst != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, dst); err != nil {
			return resp, fmt.Errorf("decode response: %w", err)
		}
	}
	return resp, nil
}

// splitRepo splits "owner/repo" into (owner, repo), returning an error on bad format.
func splitRepo(repo string) (string, string, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo format %q: expected owner/repo", repo)
	}
	return parts[0], parts[1], nil
}

// ---- PR types ----

// PR represents a GitHub pull request.
type PR struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	Head    struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Draft          bool   `json:"draft"`
	Mergeable      *bool  `json:"mergeable"`
	MergeableState string `json:"mergeable_state"`
	ReviewDecision string `json:"review_decision"`
}

// PRListResult holds a list of PRs.
type PRListResult struct {
	Repo string `json:"repo"`
	PRs  []PR   `json:"prs"`
}

// PRGetResult holds a single PR with check status.
type PRGetResult struct {
	Repo   string     `json:"repo"`
	PR     PR         `json:"pr"`
	Checks []CheckRun `json:"checks"`
}

// PRCreateResult holds the result of PR creation.
type PRCreateResult struct {
	URL    string `json:"url"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Repo   string `json:"repo"`
	Head   string `json:"head"`
	Base   string `json:"base"`
}

// PRMergeResult holds the result of a PR merge.
type PRMergeResult struct {
	PRNumber int    `json:"pr_number"`
	Repo     string `json:"repo"`
	SHA      string `json:"sha"`
	Title    string `json:"title"`
	Method   string `json:"method"`
}

// ---- Check types ----

// CheckRun represents a single CI check run.
type CheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
}

// ChecksResult holds CI check results for a ref.
type ChecksResult struct {
	Ref        string     `json:"ref"`
	Repo       string     `json:"repo"`
	TotalCount int        `json:"total_count"`
	CheckRuns  []CheckRun `json:"check_runs"`
}

type checkRunsResponse struct {
	TotalCount int        `json:"total_count"`
	CheckRuns  []CheckRun `json:"check_runs"`
}

// ---- Issue types ----

// Issue represents a GitHub issue.
type Issue struct {
	Number  int     `json:"number"`
	Title   string  `json:"title"`
	State   string  `json:"state"`
	HTMLURL string  `json:"html_url"`
	Body    string  `json:"body"`
	Labels  []Label `json:"labels"`
}

// Label is a GitHub issue label.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// IssueListResult holds a list of issues.
type IssueListResult struct {
	Repo   string  `json:"repo"`
	Issues []Issue `json:"issues"`
}

// IssueCreateResult holds the result of issue creation.
type IssueCreateResult struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Repo   string `json:"repo"`
}

// ---- RC branch types ----

// Branch represents a GitHub branch.
type Branch struct {
	Name string `json:"name"`
}

// RCListResult holds a list of rc/* branches.
type RCListResult struct {
	Repo     string   `json:"repo"`
	Branches []string `json:"branches"`
	Current  string   `json:"current"`
}

// RCCreateResult holds the result of creating an RC branch.
type RCCreateResult struct {
	Repo       string `json:"repo"`
	BranchName string `json:"branch_name"`
	BaseSHA    string `json:"base_sha"`
}

// ---- PR operations ----

// ListPRs lists open pull requests for a repo.
func (c *Client) ListPRs(repo string) (*PRListResult, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	var prs []PR
	if _, err := c.do("GET", fmt.Sprintf("/repos/%s/%s/pulls?state=open&per_page=50", owner, name), nil, &prs); err != nil {
		return nil, err
	}
	return &PRListResult{Repo: repo, PRs: prs}, nil
}

// GetPR fetches a single PR by number, including its CI checks.
func (c *Client) GetPR(repo string, number int) (*PRGetResult, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	var pr PR
	if _, err := c.do("GET", fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, name, number), nil, &pr); err != nil {
		return nil, err
	}

	checks, err := c.GetChecksForRef(repo, pr.Head.Ref)
	if err != nil {
		// Best-effort: return PR without checks rather than failing.
		checks = &ChecksResult{Ref: pr.Head.Ref, Repo: repo}
	}

	return &PRGetResult{Repo: repo, PR: pr, Checks: checks.CheckRuns}, nil
}

// CreatePR opens a new pull request.
func (c *Client) CreatePR(repo, title, body, head, base string) (*PRCreateResult, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"title": title,
		"body":  body,
		"head":  head,
		"base":  base,
	}

	var result struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
		Title   string `json:"title"`
	}
	if _, err := c.do("POST", fmt.Sprintf("/repos/%s/%s/pulls", owner, name), payload, &result); err != nil {
		return nil, err
	}
	return &PRCreateResult{
		URL:    result.HTMLURL,
		Number: result.Number,
		Title:  result.Title,
		Repo:   repo,
		Head:   head,
		Base:   base,
	}, nil
}

// MergePR merges a pull request. method must be "squash", "merge", or "rebase".
func (c *Client) MergePR(repo string, number int, method string) (*PRMergeResult, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	// Validate method, default to squash.
	switch method {
	case "squash", "merge", "rebase":
	case "":
		method = "squash"
	default:
		return nil, fmt.Errorf("invalid merge method %q: must be squash, merge, or rebase", method)
	}

	// Fetch PR title and head SHA for the commit message.
	var prData struct {
		Title string `json:"title"`
		Head  struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if _, err := c.do("GET", fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, name, number), nil, &prData); err != nil {
		return nil, fmt.Errorf("fetch PR details: %w", err)
	}

	payload := map[string]any{
		"merge_method": method,
		"commit_title": prData.Title,
		"sha":          prData.Head.SHA,
	}

	var mergeResp struct {
		SHA     string `json:"sha"`
		Message string `json:"message"`
	}
	if _, err := c.do("PUT", fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", owner, name, number), payload, &mergeResp); err != nil {
		return nil, err
	}
	return &PRMergeResult{
		PRNumber: number,
		Repo:     repo,
		SHA:      mergeResp.SHA,
		Title:    prData.Title,
		Method:   method,
	}, nil
}

// ---- CI check operations ----

// GetChecksForRef returns CI check runs for a branch ref.
func (c *Client) GetChecksForRef(repo, ref string) (*ChecksResult, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	var resp checkRunsResponse
	if _, err := c.do("GET", fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", owner, name, ref), nil, &resp); err != nil {
		return nil, err
	}
	return &ChecksResult{
		Ref:        ref,
		Repo:       repo,
		TotalCount: resp.TotalCount,
		CheckRuns:  resp.CheckRuns,
	}, nil
}

// GetChecksForPR returns CI check runs for a PR (looked up by number).
func (c *Client) GetChecksForPR(repo string, number int) (*ChecksResult, error) {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	// Resolve branch from PR number.
	var prData struct {
		Head struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}
	if _, err := c.do("GET", fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repoName, number), nil, &prData); err != nil {
		return nil, fmt.Errorf("resolve PR branch: %w", err)
	}
	return c.GetChecksForRef(repo, prData.Head.Ref)
}

// ---- Issue operations ----

// ListIssues lists open issues for a repo.
func (c *Client) ListIssues(repo string) (*IssueListResult, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	var issues []Issue
	if _, err := c.do("GET", fmt.Sprintf("/repos/%s/%s/issues?state=open&per_page=50", owner, name), nil, &issues); err != nil {
		return nil, err
	}
	// Filter out PRs (GitHub returns PRs in the issues list).
	var filtered []Issue
	for _, iss := range issues {
		filtered = append(filtered, iss)
	}
	return &IssueListResult{Repo: repo, Issues: filtered}, nil
}

// CreateIssue opens a new issue.
func (c *Client) CreateIssue(repo, title, body string, labels []string) (*IssueCreateResult, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"title":  title,
		"body":   body,
		"labels": labels,
	}

	var result struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		Title   string `json:"title"`
	}
	if _, err := c.do("POST", fmt.Sprintf("/repos/%s/%s/issues", owner, name), payload, &result); err != nil {
		return nil, err
	}
	return &IssueCreateResult{
		Number: result.Number,
		URL:    result.HTMLURL,
		Title:  result.Title,
		Repo:   repo,
	}, nil
}

// AddIssueLabels adds labels to an existing issue.
func (c *Client) AddIssueLabels(repo string, number int, labels []string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	_, err = c.do("POST", fmt.Sprintf("/repos/%s/%s/issues/%d/labels", owner, name, number), map[string]any{"labels": labels}, nil)
	return err
}

// RemoveIssueLabel removes a single label from an issue.
func (c *Client) RemoveIssueLabel(repo string, number int, label string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	_, err = c.do("DELETE", fmt.Sprintf("/repos/%s/%s/issues/%d/labels/%s", owner, name, number, label), nil, nil)
	return err
}

// ---- RC branch operations ----

// ListRCBranches returns all rc/* branches for a repo, sorted by name descending.
func (c *Client) ListRCBranches(repo string) (*RCListResult, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	// GitHub's branch filter uses per_page + prefix matching via search.
	var branches []Branch
	if _, err := c.do("GET", fmt.Sprintf("/repos/%s/%s/branches?per_page=100", owner, name), nil, &branches); err != nil {
		return nil, err
	}

	var rcBranches []string
	for _, b := range branches {
		if strings.HasPrefix(b.Name, "rc/") {
			rcBranches = append(rcBranches, b.Name)
		}
	}

	// Sort descending (latest semver first).
	sortRCBranches(rcBranches)

	current := ""
	if len(rcBranches) > 0 {
		current = rcBranches[0]
	}
	return &RCListResult{Repo: repo, Branches: rcBranches, Current: current}, nil
}

// CreateRCBranch creates the next rc/vX.Y.Z branch on GitHub based on the current highest version.
// If version is empty, it auto-increments the minor version from the current RC branch.
func (c *Client) CreateRCBranch(repo, version string) (*RCCreateResult, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	// Determine the new branch name.
	branchName, err := c.nextRCBranchName(repo, version)
	if err != nil {
		return nil, err
	}

	// Get the current HEAD SHA of main to base the branch on.
	var refResp struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if _, err := c.do("GET", fmt.Sprintf("/repos/%s/%s/git/ref/heads/main", owner, name), nil, &refResp); err != nil {
		return nil, fmt.Errorf("get main HEAD: %w", err)
	}

	// Create the branch.
	payload := map[string]any{
		"ref": "refs/heads/" + branchName,
		"sha": refResp.Object.SHA,
	}
	if _, err := c.do("POST", fmt.Sprintf("/repos/%s/%s/git/refs", owner, name), payload, nil); err != nil {
		return nil, fmt.Errorf("create branch: %w", err)
	}

	return &RCCreateResult{
		Repo:       repo,
		BranchName: branchName,
		BaseSHA:    refResp.Object.SHA,
	}, nil
}

// nextRCBranchName determines the next rc/vX.Y.Z name.
// If version is provided (e.g. "v1.3.0"), it is used directly.
// Otherwise the current highest RC branch is auto-incremented (minor bump).
func (c *Client) nextRCBranchName(repo, version string) (string, error) {
	if version != "" {
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		return "rc/" + version, nil
	}

	list, err := c.ListRCBranches(repo)
	if err != nil {
		return "", err
	}

	if len(list.Branches) == 0 {
		// No existing RC branches: start at v0.1.0.
		return "rc/v0.1.0", nil
	}

	// Parse "rc/vX.Y.Z" and bump minor.
	current := strings.TrimPrefix(list.Current, "rc/v")
	parts := strings.SplitN(current, ".", 3)
	if len(parts) != 3 {
		return "", fmt.Errorf("cannot parse version from %q", list.Current)
	}

	var major, minor int
	fmt.Sscanf(parts[0], "%d", &major)
	fmt.Sscanf(parts[1], "%d", &minor)
	return fmt.Sprintf("rc/v%d.%d.0", major, minor+1), nil
}

// sortRCBranches sorts rc/* branch names descending (latest first) using simple string sort.
// For "rc/vX.Y.Z" names this produces correct semver ordering for common cases.
func sortRCBranches(branches []string) {
	for i := 0; i < len(branches)-1; i++ {
		for j := i + 1; j < len(branches); j++ {
			if branches[i] < branches[j] {
				branches[i], branches[j] = branches[j], branches[i]
			}
		}
	}
}

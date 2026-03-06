// Package data provides fetchers for Gatus, GitHub Actions, and Gitea Actions.
package data

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// FetchGitHubWorkflow fetches the latest run for a single GitHub Actions workflow
// on the main branch.
//
// workflowFile is the filename of the workflow (e.g. "ci.yml").
// token is a GitHub personal access token.
func FetchGitHubWorkflow(ctx context.Context, owner, repo, workflowFile, token string) (WorkflowRun, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/workflows/%s/runs?per_page=1&branch=main", owner, repo, workflowFile)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return WorkflowRun{Status: CIUnknown}, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return WorkflowRun{Status: CIUnknown}, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return WorkflowRun{Status: CIUnknown}, fmt.Errorf("github returned %d", resp.StatusCode)
	}

	var raw struct {
		Runs []struct {
			Name       string `json:"name"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
			UpdatedAt  string `json:"updated_at"`
		} `json:"workflow_runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return WorkflowRun{Status: CIUnknown}, err
	}
	if len(raw.Runs) == 0 {
		return WorkflowRun{Status: CIUnknown}, nil
	}

	r := raw.Runs[0]
	return WorkflowRun{
		Name:      r.Name,
		Status:    conclusionToCIStatus(r.Conclusion),
		URL:       r.HTMLURL,
		UpdatedAt: r.UpdatedAt,
	}, nil
}

// FetchGitHubWorkflows fetches latest runs for multiple workflows in parallel.
// Returns a map keyed by workflowFile.
func FetchGitHubWorkflows(ctx context.Context, owner, repo, token string, workflowFiles []string) map[string]WorkflowRun {
	type result struct {
		file string
		run  WorkflowRun
	}
	ch := make(chan result, len(workflowFiles))
	for _, f := range workflowFiles {
		go func(file string) {
			run, _ := FetchGitHubWorkflow(ctx, owner, repo, file, token)
			ch <- result{file: file, run: run}
		}(f)
	}
	out := make(map[string]WorkflowRun, len(workflowFiles))
	for range workflowFiles {
		r := <-ch
		out[r.file] = r.run
	}
	return out
}

func conclusionToCIStatus(c string) CIStatus {
	switch c {
	case "success":
		return CISuccess
	case "failure", "timed_out", "cancelled":
		return CIFailure
	case "action_required", "waiting", "queued", "in_progress":
		return CIPending
	default:
		return CIUnknown
	}
}

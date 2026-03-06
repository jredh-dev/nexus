// Package data provides fetchers for Gatus, GitHub Actions, and Gitea Actions.
package data

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// FetchGiteaWorkflows fetches the latest run for each workflow in workflowFiles
// from the local Gitea instance.
//
// Returns a map keyed by workflow filename (e.g. "docker-deploy.yml").
func FetchGiteaWorkflows(ctx context.Context, baseURL, owner, repo, token string, workflowFiles []string) map[string]WorkflowRun {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Fetch recent runs — grab enough to cover all workflow files
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/actions/runs?limit=50", baseURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return unknownRuns(workflowFiles)
	}
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return unknownRuns(workflowFiles)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return unknownRuns(workflowFiles)
	}

	var raw struct {
		Runs []struct {
			Path       string `json:"path"` // e.g. "docker-deploy.yml@refs/heads/main"
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
			UpdatedAt  string `json:"completed_at"`
		} `json:"workflow_runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return unknownRuns(workflowFiles)
	}

	// Index: workflow filename → latest run (first match wins since runs are newest-first)
	out := make(map[string]WorkflowRun, len(workflowFiles))
	for _, run := range raw.Runs {
		// path looks like "docker-deploy.yml@refs/heads/main"
		file := strings.SplitN(run.Path, "@", 2)[0]
		if _, seen := out[file]; seen {
			continue // already have latest for this file
		}
		out[file] = WorkflowRun{
			Name:      file,
			Status:    giteaRunStatus(run.Status, run.Conclusion),
			URL:       run.HTMLURL,
			UpdatedAt: run.UpdatedAt,
		}
	}

	// Fill missing with unknown
	for _, f := range workflowFiles {
		if _, ok := out[f]; !ok {
			out[f] = WorkflowRun{Status: CIUnknown}
		}
	}
	return out
}

func unknownRuns(files []string) map[string]WorkflowRun {
	out := make(map[string]WorkflowRun, len(files))
	for _, f := range files {
		out[f] = WorkflowRun{Status: CIUnknown}
	}
	return out
}

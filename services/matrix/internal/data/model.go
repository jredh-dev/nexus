// Package data provides types shared across all data fetchers.
package data

// Status represents a simple up/down/unknown state.
type Status string

const (
	StatusUp      Status = "up"
	StatusDown    Status = "down"
	StatusUnknown Status = "unknown"
)

// CIStatus represents the conclusion of a CI/CD workflow run.
type CIStatus string

const (
	CISuccess CIStatus = "success"
	CIFailure CIStatus = "failure"
	CIPending CIStatus = "pending"
	CIUnknown CIStatus = "unknown"
)

// GatusResult is the health status for a single Gatus endpoint.
type GatusResult struct {
	Name   string
	Group  string
	Key    string
	Status Status
}

// WorkflowRun is the latest run result for a single CI/CD workflow.
type WorkflowRun struct {
	Name      string
	Status    CIStatus
	URL       string
	UpdatedAt string // RFC3339 timestamp of last update/completion
	RunNumber int    // provider run number (e.g. GitHub run_number, Gitea id)
}

// PageData is the full dataset rendered into the dashboard template.
type PageData struct {
	// Gatus results keyed by Gatus endpoint key (e.g. "local_portal-(local)")
	Gatus map[string]GatusResult

	// GitHub workflow runs keyed by workflow filename (e.g. "ci.yml")
	GitHub map[string]WorkflowRun

	// Gitea workflow runs keyed by workflow filename (e.g. "docker-deploy.yml")
	Gitea map[string]WorkflowRun

	// Error messages for each data source (non-fatal, displayed in footer)
	Errors []string
}

// Package page provides the HTTP handler and HTML template for the matrix dashboard.
package page

import (
	_ "embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jredh-dev/nexus/services/matrix/internal/data"
)

//go:embed template.html
var templateSource string

var tmpl = template.Must(template.New("matrix").Funcs(template.FuncMap{
	// statusClass returns a CSS class name for a health status dot.
	"statusClass": func(s data.Status) string {
		switch s {
		case data.StatusUp:
			return "dot-up"
		case data.StatusDown:
			return "dot-down"
		default:
			return "dot-unknown"
		}
	},
	// statusText returns a short label for a health status.
	"statusText": func(s data.Status) string {
		switch s {
		case data.StatusUp:
			return "up"
		case data.StatusDown:
			return "down"
		default:
			return "?"
		}
	},
	// ciClass returns a CSS class name for a CI run conclusion.
	"ciClass": func(s data.CIStatus) string {
		switch s {
		case data.CISuccess:
			return "ci-success"
		case data.CIFailure:
			return "ci-failure"
		case data.CIPending:
			return "ci-pending"
		default:
			return "ci-unknown"
		}
	},
	// ciText returns a short label for a CI run conclusion.
	"ciText": func(s data.CIStatus) string {
		switch s {
		case data.CISuccess:
			return "✓"
		case data.CIFailure:
			return "✗"
		case data.CIPending:
			return "…"
		default:
			return "?"
		}
	},
}).Parse(templateSource))

// Config holds runtime configuration for the page handler.
type Config struct {
	GatusURL    string // e.g. "http://host.docker.internal:8084"
	GiteaURL    string // e.g. "http://host.docker.internal:3000"
	GiteaToken  string
	GitHubToken string
	GitHubOwner string
	GitHubRepo  string
	GiteaOwner  string
	GiteaRepo   string
}

// ConfigFromEnv reads handler config from environment variables with defaults.
func ConfigFromEnv() Config {
	return Config{
		GatusURL:    envOr("GATUS_URL", "http://host.docker.internal:8084"),
		GiteaURL:    envOr("GITEA_URL", "http://host.docker.internal:3000"),
		GiteaToken:  os.Getenv("GITEA_TOKEN"),
		GitHubToken: os.Getenv("GITHUB_TOKEN"),
		GitHubOwner: envOr("GITHUB_OWNER", "jredh-dev"),
		GitHubRepo:  envOr("GITHUB_REPO", "nexus"),
		GiteaOwner:  envOr("GITEA_OWNER", "jredhbot"),
		GiteaRepo:   envOr("GITEA_REPO", "nexus"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Handler returns an HTTP handler that renders the matrix dashboard.
// Data is fetched live on each request (with a short timeout).
// Slow or unavailable sources show "?" rather than blocking the page.
func Handler(cfg Config) http.HandlerFunc {
	// All GitHub Actions workflows to fetch for nexus.
	ghWorkflows := []string{
		"ci.yml",
		"deploy-hermit-dev.yml",
		"deploy-go-http-dev.yml",
		"deploy-portal-dev.yml",
		"deploy-web-dev.yml",
		"deploy-cal-dev.yml",
		"deploy-vn-dev.yml",
		"integration-tests-dev.yml",
	}
	// ctl repo workflows.
	ctlWorkflows := []string{"ci.yml"}

	// Gitea workflows to track (nexus repo only).
	giteaWorkflows := []string{"docker-deploy.yml", "install.yml"}

	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		start := time.Now()

		var (
			mu     sync.Mutex
			errors []string
		)
		addErr := func(msg string) {
			mu.Lock()
			errors = append(errors, msg)
			mu.Unlock()
		}

		// Fetch all data sources in parallel.
		var (
			gatusData map[string]data.GatusResult
			ghNexus   map[string]data.WorkflowRun
			ghCtl     map[string]data.WorkflowRun
			giteaData map[string]data.WorkflowRun
		)

		var wg sync.WaitGroup
		wg.Add(4)

		go func() {
			defer wg.Done()
			d, err := data.FetchGatus(ctx, cfg.GatusURL)
			if err != nil {
				addErr("gatus: " + err.Error())
				d = map[string]data.GatusResult{}
			}
			gatusData = d
		}()

		go func() {
			defer wg.Done()
			ghNexus = data.FetchGitHubWorkflows(ctx, cfg.GitHubOwner, cfg.GitHubRepo, cfg.GitHubToken, ghWorkflows)
		}()

		go func() {
			defer wg.Done()
			ghCtl = data.FetchGitHubWorkflows(ctx, cfg.GitHubOwner, "ctl", cfg.GitHubToken, ctlWorkflows)
		}()

		go func() {
			defer wg.Done()
			giteaData = data.FetchGiteaWorkflows(ctx, cfg.GiteaURL, cfg.GiteaOwner, cfg.GiteaRepo, cfg.GiteaToken, giteaWorkflows)
		}()

		wg.Wait()
		log.Printf("matrix: data fetched in %s", time.Since(start).Round(time.Millisecond))

		pd := buildPageData(gatusData, ghNexus, ghCtl, giteaData, errors)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Short cache — stale for 10s is fine, data changes slowly.
		w.Header().Set("Cache-Control", "public, max-age=10")
		if err := tmpl.Execute(w, pd); err != nil {
			log.Printf("matrix: template error: %v", err)
		}
	}
}

// TemplateData is the full view model passed to template.html.
type TemplateData struct {
	Services  []ServiceGroup
	Repos     []RepoGroup
	Errors    []string
	FetchedAt string
}

// ServiceGroup is one named service (hermit, secrets, portal, etc.)
// with its local and cloud environments.
type ServiceGroup struct {
	Name        string
	Description string
	Local       *ServiceEnv // nil if no local deployment
	Cloud       *ServiceEnv // nil if no cloud deployment
}

// ServiceEnv holds the runtime info for one environment (local or cloud).
type ServiceEnv struct {
	URL         string
	Port        string
	GatusKey    string
	GatusStatus data.Status
	// Pipelines is the ordered list of CI/deploy workflows for this env.
	// Each entry is displayed as an individual named pill.
	Pipelines []WorkflowRow
}

// WorkflowRow holds a single CI/deploy pipeline status + link.
type WorkflowRow struct {
	Label  string // display name (workflow filename without .yml)
	Status data.CIStatus
	URL    string
}

// RepoGroup is a repo with its Gitea/GitHub links and all associated pipelines.
type RepoGroup struct {
	Name      string
	GitHubURL string
	GiteaURL  string
	Pipelines []WorkflowRow // all named pipelines for this repo
}

// trimYML strips the ".yml" suffix for display labels.
func trimYML(s string) string {
	return strings.TrimSuffix(s, ".yml")
}

func buildPageData(
	gatus map[string]data.GatusResult,
	ghNexus map[string]data.WorkflowRun,
	ghCtl map[string]data.WorkflowRun,
	gitea map[string]data.WorkflowRun,
	errors []string,
) TemplateData {
	// gs looks up a Gatus health status by endpoint key.
	gs := func(key string) data.Status {
		if r, ok := gatus[key]; ok {
			return r.Status
		}
		return data.StatusUnknown
	}

	// ghRow builds a WorkflowRow from a GitHub Actions map.
	ghRow := func(m map[string]data.WorkflowRun, file string) WorkflowRow {
		r, ok := m[file]
		if !ok {
			return WorkflowRow{Label: trimYML(file), Status: data.CIUnknown}
		}
		return WorkflowRow{Label: trimYML(file), Status: r.Status, URL: r.URL}
	}

	// giteaRow builds a WorkflowRow from the Gitea runs map.
	giteaRow := func(file string) WorkflowRow {
		r, ok := gitea[file]
		if !ok {
			return WorkflowRow{Label: trimYML(file), Status: data.CIUnknown}
		}
		return WorkflowRow{Label: trimYML(file), Status: r.Status, URL: r.URL}
	}

	// ghRows builds multiple WorkflowRows from a GitHub Actions map.
	ghRows := func(m map[string]data.WorkflowRun, files ...string) []WorkflowRow {
		rows := make([]WorkflowRow, 0, len(files))
		for _, f := range files {
			rows = append(rows, ghRow(m, f))
		}
		return rows
	}

	// giteaRows builds multiple WorkflowRows from the Gitea runs map.
	giteaRows := func(files ...string) []WorkflowRow {
		rows := make([]WorkflowRow, 0, len(files))
		for _, f := range files {
			rows = append(rows, giteaRow(f))
		}
		return rows
	}

	services := []ServiceGroup{
		{
			Name:        "hermit",
			Description: "rust grpc server",
			Local: &ServiceEnv{
				URL:         "http://localhost:9090",
				Port:        ":9090",
				GatusKey:    "local_hermit-(local)",
				GatusStatus: gs("local_hermit-(local)"),
				// docker-deploy rebuilds all containers; install keeps tui binary fresh
				Pipelines: giteaRows("docker-deploy.yml", "install.yml"),
			},
			Cloud: &ServiceEnv{
				URL:         "https://nexus-hermit-dev-2tvic4xjjq-uc.a.run.app",
				Port:        "cloud run",
				GatusKey:    "cloud-run_hermit-(cloud)",
				GatusStatus: gs("cloud-run_hermit-(cloud)"),
				Pipelines:   ghRows(ghNexus, "deploy-hermit-dev.yml"),
			},
		},
		{
			Name:        "secrets",
			Description: "confessions api",
			Local: &ServiceEnv{
				URL:         "http://localhost:8081",
				Port:        ":8081",
				GatusKey:    "local_secrets-(local)",
				GatusStatus: gs("local_secrets-(local)"),
				Pipelines:   giteaRows("docker-deploy.yml"),
			},
			Cloud: &ServiceEnv{
				URL:         "https://nexus-secrets-dev-2tvic4xjjq-uc.a.run.app/health",
				Port:        "cloud run",
				GatusKey:    "cloud-run_secrets-(cloud)",
				GatusStatus: gs("cloud-run_secrets-(cloud)"),
				// secrets is deployed via deploy-go-http-dev.yml (shared go-http workflow)
				Pipelines: ghRows(ghNexus, "deploy-go-http-dev.yml"),
			},
		},
		{
			Name:        "portal",
			Description: "web portal / admin",
			Local: &ServiceEnv{
				URL:         "http://localhost:8090/login",
				Port:        ":8090",
				GatusKey:    "local_portal-(local)",
				GatusStatus: gs("local_portal-(local)"),
				Pipelines:   giteaRows("docker-deploy.yml"),
			},
			Cloud: &ServiceEnv{
				URL:         "https://nexus-portal-dev-2tvic4xjjq-uc.a.run.app",
				Port:        "cloud run",
				GatusKey:    "cloud-run_portal-(cloud)",
				GatusStatus: gs("cloud-run_portal-(cloud)"),
				Pipelines:   ghRows(ghNexus, "deploy-portal-dev.yml"),
			},
		},
		{
			Name:        "web",
			Description: "astro frontend",
			Local: &ServiceEnv{
				URL:         "http://localhost:8083",
				Port:        ":8083",
				GatusKey:    "local_web-(local)",
				GatusStatus: gs("local_web-(local)"),
				Pipelines:   giteaRows("docker-deploy.yml"),
			},
			Cloud: &ServiceEnv{
				URL:         "https://nexus-web-dev-2tvic4xjjq-uc.a.run.app",
				Port:        "cloud run",
				GatusKey:    "cloud-run_web-(cloud)",
				GatusStatus: gs("cloud-run_web-(cloud)"),
				Pipelines:   ghRows(ghNexus, "deploy-web-dev.yml"),
			},
		},
		{
			Name:        "vn",
			Description: "visual novel engine",
			Local: &ServiceEnv{
				URL:         "http://localhost:8082/health",
				Port:        ":8082",
				GatusKey:    "local_vn-(local)",
				GatusStatus: gs("local_vn-(local)"),
				Pipelines:   giteaRows("docker-deploy.yml"),
			},
			Cloud: &ServiceEnv{
				URL:         "https://nexus-vn-dev-2tvic4xjjq-uc.a.run.app/health",
				Port:        "cloud run",
				GatusKey:    "cloud-run_vn-(cloud)",
				GatusStatus: gs("cloud-run_vn-(cloud)"),
				Pipelines:   ghRows(ghNexus, "deploy-vn-dev.yml"),
			},
		},
		{
			Name:        "cal",
			Description: "calendar / ical service",
			Cloud: &ServiceEnv{
				URL:         "https://nexus-cal-dev-2tvic4xjjq-uc.a.run.app/health",
				Port:        "cloud run",
				GatusKey:    "cloud-run_cal-(cloud)",
				GatusStatus: gs("cloud-run_cal-(cloud)"),
				Pipelines:   ghRows(ghNexus, "deploy-cal-dev.yml"),
			},
		},
		{
			Name:        "matrix",
			Description: "this page",
			Local: &ServiceEnv{
				URL:       "http://localhost:8085",
				Port:      ":8085",
				Pipelines: giteaRows("docker-deploy.yml"),
			},
		},
		{
			Name:        "gatus",
			Description: "health monitor",
			Local: &ServiceEnv{
				URL:  "http://localhost:8084",
				Port: ":8084",
			},
		},
		{
			Name:        "gitea",
			Description: "local git + ci",
			Local: &ServiceEnv{
				URL:         "http://localhost:3000",
				Port:        ":3000",
				GatusKey:    "local_gitea",
				GatusStatus: gs("local_gitea"),
			},
		},
	}

	// Repos section: each repo shows all its named pipelines.
	repos := []RepoGroup{
		{
			Name:      "nexus",
			GitHubURL: "https://github.com/jredh-dev/nexus",
			GiteaURL:  "http://localhost:3000/jredhbot/nexus",
			Pipelines: append(
				ghRows(ghNexus,
					"ci.yml",
					"integration-tests-dev.yml",
				),
				giteaRows("docker-deploy.yml", "install.yml")...,
			),
		},
		{
			Name:      "ctl",
			GitHubURL: "https://github.com/jredh-dev/ctl",
			GiteaURL:  "http://localhost:3000/jredhbot/ctl",
			Pipelines: ghRows(ghCtl, "ci.yml"),
		},
	}

	return TemplateData{
		Services:  services,
		Repos:     repos,
		Errors:    errors,
		FetchedAt: time.Now().Format("15:04:05"),
	}
}

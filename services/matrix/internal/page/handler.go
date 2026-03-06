// Package page provides the HTTP handler and HTML template for the matrix dashboard.
package page

import (
	_ "embed"
	"html/template"
	"log"
	"net/http"
	"os"
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
	// GitHub workflows to track (filename → display label)
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
	// ctl repo workflows
	ctlWorkflows := []string{"ci.yml"}

	// Gitea workflows to track
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

		// Fetch all data sources in parallel
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
		// Short cache — stale for 10s is fine, data changes slowly
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
// with its local status, cloud status, and associated CI/deploy workflows.
type ServiceGroup struct {
	Name        string
	Description string
	Local       *ServiceEnv  // nil if no local deployment
	Cloud       *ServiceEnv  // nil if no cloud deployment
	CIWorkflow  *WorkflowRow // GitHub CI (only on nexus/ctl top level)
}

// ServiceEnv holds the runtime info for one environment (local or cloud).
type ServiceEnv struct {
	URL         string
	Port        string
	GatusKey    string
	GatusStatus data.Status
	Deploy      *WorkflowRow // deploy workflow for this env
}

// WorkflowRow holds a CI/deploy workflow status + link.
type WorkflowRow struct {
	Label  string
	Status data.CIStatus
	URL    string
}

// RepoGroup is a repo and its Gitea/GitHub links.
type RepoGroup struct {
	Name      string
	GitHubURL string
	GiteaURL  string
	CIURL     string
	CIStatus  data.CIStatus
}

func buildPageData(
	gatus map[string]data.GatusResult,
	ghNexus map[string]data.WorkflowRun,
	ghCtl map[string]data.WorkflowRun,
	gitea map[string]data.WorkflowRun,
	errors []string,
) TemplateData {
	gs := func(key string) data.Status {
		if r, ok := gatus[key]; ok {
			return r.Status
		}
		return data.StatusUnknown
	}
	ghRun := func(m map[string]data.WorkflowRun, file string) *WorkflowRow {
		r, ok := m[file]
		if !ok {
			return &WorkflowRow{Label: file, Status: data.CIUnknown}
		}
		return &WorkflowRow{Label: file, Status: r.Status, URL: r.URL}
	}
	giteaRun := func(file string) *WorkflowRow {
		r, ok := gitea[file]
		if !ok {
			return &WorkflowRow{Label: file, Status: data.CIUnknown}
		}
		return &WorkflowRow{Label: file, Status: r.Status, URL: r.URL}
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
				Deploy:      giteaRun("docker-deploy.yml"),
			},
			Cloud: &ServiceEnv{
				URL:         "https://nexus-hermit-dev-2tvic4xjjq-uc.a.run.app",
				Port:        "cloud run",
				GatusKey:    "cloud-run_hermit-(cloud)",
				GatusStatus: gs("cloud-run_hermit-(cloud)"),
				Deploy:      ghRun(ghNexus, "deploy-hermit-dev.yml"),
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
				Deploy:      giteaRun("docker-deploy.yml"),
			},
			Cloud: &ServiceEnv{
				URL:         "https://nexus-secrets-dev-2tvic4xjjq-uc.a.run.app/health",
				Port:        "cloud run",
				GatusKey:    "cloud-run_secrets-(cloud)",
				GatusStatus: gs("cloud-run_secrets-(cloud)"),
				Deploy:      ghRun(ghNexus, "deploy-go-http-dev.yml"),
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
				Deploy:      giteaRun("docker-deploy.yml"),
			},
			Cloud: &ServiceEnv{
				URL:         "https://nexus-portal-dev-2tvic4xjjq-uc.a.run.app",
				Port:        "cloud run",
				GatusKey:    "cloud-run_portal-(cloud)",
				GatusStatus: gs("cloud-run_portal-(cloud)"),
				Deploy:      ghRun(ghNexus, "deploy-portal-dev.yml"),
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
				Deploy:      giteaRun("docker-deploy.yml"),
			},
			Cloud: &ServiceEnv{
				URL:         "https://nexus-web-dev-2tvic4xjjq-uc.a.run.app",
				Port:        "cloud run",
				GatusKey:    "cloud-run_web-(cloud)",
				GatusStatus: gs("cloud-run_web-(cloud)"),
				Deploy:      ghRun(ghNexus, "deploy-web-dev.yml"),
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
				Deploy:      giteaRun("docker-deploy.yml"),
			},
			Cloud: &ServiceEnv{
				URL:         "https://nexus-vn-dev-2tvic4xjjq-uc.a.run.app/health",
				Port:        "cloud run",
				GatusKey:    "cloud-run_vn-(cloud)",
				GatusStatus: gs("cloud-run_vn-(cloud)"),
				Deploy:      ghRun(ghNexus, "deploy-vn-dev.yml"),
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
				Deploy:      ghRun(ghNexus, "deploy-cal-dev.yml"),
			},
		},
		{
			Name:        "matrix",
			Description: "this page",
			Local: &ServiceEnv{
				URL:    "http://localhost:8085",
				Port:   ":8085",
				Deploy: giteaRun("docker-deploy.yml"),
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

	// nexus CI row (top-level, not per-service)
	nexusCIRun := ghRun(ghNexus, "ci.yml")
	nexusIntRun := ghRun(ghNexus, "integration-tests-dev.yml")
	ctlCIRun := ghRun(ghCtl, "ci.yml")

	repos := []RepoGroup{
		{
			Name:      "nexus",
			GitHubURL: "https://github.com/jredh-dev/nexus",
			GiteaURL:  "http://localhost:3000/jredhbot/nexus",
			CIURL:     nexusCIRun.URL,
			CIStatus:  nexusCIRun.Status,
		},
		{
			Name:      "ctl",
			GitHubURL: "https://github.com/jredh-dev/ctl",
			GiteaURL:  "http://localhost:3000/jredhbot/ctl",
			CIURL:     ctlCIRun.URL,
			CIStatus:  ctlCIRun.Status,
		},
	}

	_ = nexusIntRun // available for template if needed

	return TemplateData{
		Services:  services,
		Repos:     repos,
		Errors:    errors,
		FetchedAt: time.Now().Format("15:04:05"),
	}
}

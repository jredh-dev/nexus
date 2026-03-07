// Package page provides the HTTP handler and HTML template for the matrix dashboard.
package page

import (
	_ "embed"
	"fmt"
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
	// timeAgo formats an RFC3339 timestamp as a human-friendly "Xm ago" string.
	// Returns an empty string if the timestamp is zero or unparseable.
	"timeAgo": func(ts string) string {
		if ts == "" {
			return ""
		}
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			// Try without sub-seconds
			t, err = time.Parse("2006-01-02T15:04:05Z", ts)
			if err != nil {
				return ""
			}
		}
		d := time.Since(t).Round(time.Minute)
		switch {
		case d < time.Minute:
			return "just now"
		case d < time.Hour:
			return fmt.Sprintf("%dm ago", int(d.Minutes()))
		case d < 24*time.Hour:
			h := int(d.Hours())
			m := int(d.Minutes()) % 60
			if m == 0 {
				return fmt.Sprintf("%dh ago", h)
			}
			return fmt.Sprintf("%dh%dm ago", h, m)
		default:
			return fmt.Sprintf("%dd ago", int(d.Hours()/24))
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
	// ctl repo GitHub workflows.
	ctlGHWorkflows := []string{"ci.yml"}

	// Gitea workflows to track (nexus repo).
	nexusGiteaWorkflows := []string{"docker-deploy.yml", "install.yml"}

	// Gitea workflows to track (ctl repo).
	ctlGiteaWorkflows := []string{"install.yml"}

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
			gatusData  map[string]data.GatusResult
			ghNexus    map[string]data.WorkflowRun
			ghCtl      map[string]data.WorkflowRun
			giteaNexus map[string]data.WorkflowRun
			giteaCtl   map[string]data.WorkflowRun
		)

		var wg sync.WaitGroup
		wg.Add(5)

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
			ghCtl = data.FetchGitHubWorkflows(ctx, cfg.GitHubOwner, "ctl", cfg.GitHubToken, ctlGHWorkflows)
		}()

		go func() {
			defer wg.Done()
			giteaNexus = data.FetchGiteaWorkflows(ctx, cfg.GiteaURL, cfg.GiteaOwner, cfg.GiteaRepo, cfg.GiteaToken, nexusGiteaWorkflows)
		}()

		go func() {
			defer wg.Done()
			// ctl lives in the jredhbot Gitea org under repo "ctl"
			giteaCtl = data.FetchGiteaWorkflows(ctx, cfg.GiteaURL, cfg.GiteaOwner, "ctl", cfg.GiteaToken, ctlGiteaWorkflows)
		}()

		wg.Wait()
		log.Printf("matrix: data fetched in %s", time.Since(start).Round(time.Millisecond))

		pd := buildPageData(gatusData, ghNexus, ghCtl, giteaNexus, giteaCtl, errors)

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

// ServiceURL is a labelled link to a running instance of a service.
// Multiple URLs per env allow showing both the cloud run URL and any
// custom domain mappings side by side.
type ServiceURL struct {
	Label string // short display text, e.g. ":8081", "cloud run", "secrets.jredh.com"
	Href  string // full URL
}

// ServiceEnv holds the runtime info for one environment (local or cloud).
type ServiceEnv struct {
	URLs        []ServiceURL // ordered list of links: primary first, custom domains after
	GatusKey    string
	GatusStatus data.Status
	// Pipelines is the ordered list of CI/deploy workflows for this env.
	// Each entry is displayed as an individual named pill.
	Pipelines []WorkflowRow
}

// WorkflowRow holds a single CI/deploy pipeline status + link.
type WorkflowRow struct {
	Label     string // display name (workflow filename without .yml)
	Status    data.CIStatus
	URL       string
	UpdatedAt string // RFC3339 timestamp, formatted by the timeAgo template func
	RunNumber int    // provider run number; 0 means unknown
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

// u is a convenience constructor for ServiceURL.
func u(label, href string) ServiceURL { return ServiceURL{Label: label, Href: href} }

func buildPageData(
	gatus map[string]data.GatusResult,
	ghNexus map[string]data.WorkflowRun,
	ghCtl map[string]data.WorkflowRun,
	giteaNexus map[string]data.WorkflowRun,
	giteaCtl map[string]data.WorkflowRun,
	errors []string,
) TemplateData {
	// gs looks up a Gatus health status by endpoint key.
	gs := func(key string) data.Status {
		if r, ok := gatus[key]; ok {
			return r.Status
		}
		return data.StatusUnknown
	}

	// toRow converts a WorkflowRun to a WorkflowRow with a given display label.
	toRow := func(file string, r data.WorkflowRun) WorkflowRow {
		return WorkflowRow{
			Label:     trimYML(file),
			Status:    r.Status,
			URL:       r.URL,
			UpdatedAt: r.UpdatedAt,
			RunNumber: r.RunNumber,
		}
	}

	// ghRow builds a WorkflowRow from a GitHub Actions map.
	ghRow := func(m map[string]data.WorkflowRun, file string) WorkflowRow {
		r, ok := m[file]
		if !ok {
			return WorkflowRow{Label: trimYML(file), Status: data.CIUnknown}
		}
		return toRow(file, r)
	}

	// giteaRow builds a WorkflowRow from a Gitea runs map.
	giteaRow := func(m map[string]data.WorkflowRun, file string) WorkflowRow {
		r, ok := m[file]
		if !ok {
			return WorkflowRow{Label: trimYML(file), Status: data.CIUnknown}
		}
		return toRow(file, r)
	}

	// ghRows builds multiple WorkflowRows from a GitHub Actions map.
	ghRows := func(m map[string]data.WorkflowRun, files ...string) []WorkflowRow {
		rows := make([]WorkflowRow, 0, len(files))
		for _, f := range files {
			rows = append(rows, ghRow(m, f))
		}
		return rows
	}

	// giteaRows builds multiple WorkflowRows from a Gitea runs map.
	giteaRows := func(m map[string]data.WorkflowRun, files ...string) []WorkflowRow {
		rows := make([]WorkflowRow, 0, len(files))
		for _, f := range files {
			rows = append(rows, giteaRow(m, f))
		}
		return rows
	}

	services := []ServiceGroup{
		{
			Name:        "hermit",
			Description: "rust grpc server",
			Local: &ServiceEnv{
				URLs:        []ServiceURL{u(":9090", "http://localhost:9090")},
				GatusKey:    "local_hermit-(local)",
				GatusStatus: gs("local_hermit-(local)"),
				// docker-deploy rebuilds all containers; install keeps tui binary fresh
				Pipelines: giteaRows(giteaNexus, "docker-deploy.yml", "install.yml"),
			},
			Cloud: &ServiceEnv{
				URLs: []ServiceURL{
					u("cloud run", "https://nexus-hermit-dev-2tvic4xjjq-uc.a.run.app"),
				},
				GatusKey:    "cloud-run_hermit-(cloud)",
				GatusStatus: gs("cloud-run_hermit-(cloud)"),
				Pipelines:   ghRows(ghNexus, "deploy-hermit-dev.yml"),
			},
		},
		{
			Name:        "secrets",
			Description: "confessions api",
			Local: &ServiceEnv{
				URLs:        []ServiceURL{u(":8081", "http://localhost:8081")},
				GatusKey:    "local_secrets-(local)",
				GatusStatus: gs("local_secrets-(local)"),
				Pipelines:   giteaRows(giteaNexus, "docker-deploy.yml"),
			},
			Cloud: &ServiceEnv{
				// secrets.jredh.com has a failed cert mapping — show it but note it
				URLs: []ServiceURL{
					u("cloud run", "https://nexus-secrets-dev-2tvic4xjjq-uc.a.run.app/health"),
					u("secrets.jredh.com", "https://secrets.jredh.com"),
				},
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
				URLs:        []ServiceURL{u(":8090", "http://localhost:8090/login")},
				GatusKey:    "local_portal-(local)",
				GatusStatus: gs("local_portal-(local)"),
				Pipelines:   giteaRows(giteaNexus, "docker-deploy.yml"),
			},
			Cloud: &ServiceEnv{
				URLs: []ServiceURL{
					u("cloud run", "https://nexus-portal-dev-2tvic4xjjq-uc.a.run.app"),
				},
				GatusKey:    "cloud-run_portal-(cloud)",
				GatusStatus: gs("cloud-run_portal-(cloud)"),
				Pipelines:   ghRows(ghNexus, "deploy-portal-dev.yml"),
			},
		},
		{
			Name:        "web",
			Description: "astro frontend",
			Local: &ServiceEnv{
				URLs:        []ServiceURL{u(":8083", "http://localhost:8083")},
				GatusKey:    "local_web-(local)",
				GatusStatus: gs("local_web-(local)"),
				Pipelines:   giteaRows(giteaNexus, "docker-deploy.yml"),
			},
			Cloud: &ServiceEnv{
				// portal.jredh.com is mapped to nexus-web-dev (the Astro frontend)
				URLs: []ServiceURL{
					u("cloud run", "https://nexus-web-dev-2tvic4xjjq-uc.a.run.app"),
					u("portal.jredh.com", "https://portal.jredh.com"),
				},
				GatusKey:    "cloud-run_web-(cloud)",
				GatusStatus: gs("cloud-run_web-(cloud)"),
				Pipelines:   ghRows(ghNexus, "deploy-web-dev.yml"),
			},
		},
		{
			Name:        "vn",
			Description: "visual novel engine",
			Local: &ServiceEnv{
				URLs:        []ServiceURL{u(":8082", "http://localhost:8082/health")},
				GatusKey:    "local_vn-(local)",
				GatusStatus: gs("local_vn-(local)"),
				Pipelines:   giteaRows(giteaNexus, "docker-deploy.yml"),
			},
			Cloud: &ServiceEnv{
				URLs: []ServiceURL{
					u("cloud run", "https://nexus-vn-dev-2tvic4xjjq-uc.a.run.app/health"),
				},
				GatusKey:    "cloud-run_vn-(cloud)",
				GatusStatus: gs("cloud-run_vn-(cloud)"),
				Pipelines:   ghRows(ghNexus, "deploy-vn-dev.yml"),
			},
		},
		{
			Name:        "cal",
			Description: "calendar / ical service",
			Cloud: &ServiceEnv{
				URLs: []ServiceURL{
					u("cloud run", "https://nexus-cal-dev-2tvic4xjjq-uc.a.run.app/health"),
					u("cal.jredh.com", "https://cal.jredh.com"),
				},
				GatusKey:    "cloud-run_cal-(cloud)",
				GatusStatus: gs("cloud-run_cal-(cloud)"),
				Pipelines:   ghRows(ghNexus, "deploy-cal-dev.yml"),
			},
		},
		{
			Name:        "matrix",
			Description: "this page",
			Local: &ServiceEnv{
				URLs:      []ServiceURL{u(":8085", "http://localhost:8085")},
				Pipelines: giteaRows(giteaNexus, "docker-deploy.yml"),
			},
		},
		{
			Name:        "gatus",
			Description: "health monitor",
			Local: &ServiceEnv{
				URLs: []ServiceURL{u(":8084", "http://localhost:8084")},
			},
		},
		{
			Name:        "gitea",
			Description: "local git + ci",
			Local: &ServiceEnv{
				URLs:        []ServiceURL{u(":3000", "http://localhost:3000")},
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
				giteaRows(giteaNexus, "docker-deploy.yml", "install.yml")...,
			),
		},
		{
			Name:      "ctl",
			GitHubURL: "https://github.com/jredh-dev/ctl",
			GiteaURL:  "http://localhost:3000/jredhbot/ctl",
			// GitHub ci.yml + Gitea install.yml (auto-installs binary on merge)
			Pipelines: append(
				ghRows(ghCtl, "ci.yml"),
				giteaRows(giteaCtl, "install.yml")...,
			),
		},
	}

	// Display the fetch time in Pacific time (PST/PDT) — where this server runs.
	// time.LoadLocation uses the tzdata embedded in the Go binary (or the host's
	// zoneinfo); "America/Los_Angeles" covers both PST (UTC-8) and PDT (UTC-7).
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		// Fallback: UTC with an explicit suffix so it's never silently wrong.
		loc = time.UTC
	}
	return TemplateData{
		Services:  services,
		Repos:     repos,
		Errors:    errors,
		FetchedAt: time.Now().In(loc).Format("15:04:05 MST"),
	}
}

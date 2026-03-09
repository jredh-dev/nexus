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
	"github.com/jredh-dev/nexus/services/matrix/internal/discover"
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
	"timeAgo": func(ts string) string {
		if ts == "" {
			return ""
		}
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
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
	GatusURL              string // e.g. "http://host.docker.internal:8084"
	GiteaURL              string // e.g. "http://host.docker.internal:3000"
	GiteaToken            string
	GitHubToken           string
	GitHubOwner           string
	GitHubRepo            string
	GiteaOwner            string
	GiteaRepo             string
	OpenObserveURL        string // internal API URL e.g. "http://host.docker.internal:5080"
	OpenObserveBrowserURL string // browser-facing nav link e.g. "http://localhost:5080"
	OpenObserveUser       string // e.g. "admin@local.dev"
	OpenObservePass       string // e.g. "changeme"
	OpenCodeURL           string // e.g. "http://localhost:4096" — browser-facing nav link
	DockerHost            string // Docker socket path (default: /var/run/docker.sock); set DOCKER_HOST env
}

// ConfigFromEnv reads handler config from environment variables with defaults.
func ConfigFromEnv() Config {
	return Config{
		GatusURL:              envOr("GATUS_URL", "http://host.docker.internal:8084"),
		GiteaURL:              envOr("GITEA_URL", "http://host.docker.internal:3000"),
		GiteaToken:            os.Getenv("GITEA_TOKEN"),
		GitHubToken:           os.Getenv("GITHUB_TOKEN"),
		GitHubOwner:           envOr("GITHUB_OWNER", "jredh-dev"),
		GitHubRepo:            envOr("GITHUB_REPO", "nexus"),
		GiteaOwner:            envOr("GITEA_OWNER", "jredh-dev"),
		GiteaRepo:             envOr("GITEA_REPO", "nexus"),
		OpenObserveURL:        envOr("OPENOBSERVE_URL", "http://host.docker.internal:5080"),
		OpenObserveBrowserURL: envOr("OPENOBSERVE_BROWSER_URL", "http://localhost:5080"),
		OpenObserveUser:       envOr("OPENOBSERVE_USER", "admin@local.dev"),
		OpenObservePass:       envOr("OPENOBSERVE_PASS", "changeme"),
		OpenCodeURL:           envOr("OPENCODE_URL", "http://localhost:4096"),
		DockerHost:            os.Getenv("DOCKER_HOST"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Handler returns an HTTP handler that renders the matrix dashboard.
func Handler(cfg Config) http.HandlerFunc {
	ghWorkflows := []string{
		"ci.yml",
		"deploy-hermit-dev.yml",
		"deploy-go-http-dev.yml",
		"deploy-portal-dev.yml",
		"deploy-web-dev.yml",
		"deploy-cal-dev.yml",
		"deploy-vn-dev.yml",
		"deploy-deadman-dev.yml",
		"integration-tests-dev.yml",
	}
	ctlGHWorkflows := []string{"ci.yml"}
	nexusGiteaWorkflows := []string{"ci.yml", "deploy-vn.yml", "docker-deploy.yml", "install.yml"}
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

		var (
			gatusData      map[string]data.GatusResult
			ghNexus        map[string]data.WorkflowRun
			ghCtl          map[string]data.WorkflowRun
			giteaNexus     map[string]data.WorkflowRun
			giteaCtl       map[string]data.WorkflowRun
			logCounts      map[string]int
			discoveredSvcs []discover.ServiceDef
		)

		var wg sync.WaitGroup
		wg.Add(7)

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
			giteaCtl = data.FetchGiteaWorkflows(ctx, cfg.GiteaURL, cfg.GiteaOwner, "ctl", cfg.GiteaToken, ctlGiteaWorkflows)
		}()
		go func() {
			defer wg.Done()
			logCounts = data.FetchLogCounts(ctx, cfg.OpenObserveURL, cfg.OpenObserveUser, cfg.OpenObservePass)
		}()
		go func() {
			defer wg.Done()
			// Docker discovery is best-effort: if the socket is unavailable (e.g. no
			// DOCKER_HOST set, or not in Docker), just log and continue.
			svcs, err := discover.Services(ctx)
			if err != nil {
				log.Printf("matrix: docker discovery unavailable: %v", err)
				svcs = nil
			}
			discoveredSvcs = svcs
		}()

		wg.Wait()
		log.Printf("matrix: data fetched in %s", time.Since(start).Round(time.Millisecond))

		pd := buildPageData(cfg, gatusData, ghNexus, ghCtl, giteaNexus, giteaCtl, logCounts, discoveredSvcs, errors)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=10")
		if err := tmpl.Execute(w, pd); err != nil {
			log.Printf("matrix: template error: %v", err)
		}
	}
}

// TemplateData is the full view model passed to template.html.
type TemplateData struct {
	GatusURL       string // browser-facing nav link
	OpenObserveURL string // browser-facing nav link
	OpenCodeURL    string // browser-facing nav link

	AppServices   []ServiceCard
	CloudServices []ServiceCard // cloud-only services (cal)
	InfraServices []ServiceCard

	// GatusGroups is the Gatus health monitor data, grouped and sorted for the
	// inline health-monitor section. Groups are ordered: local, cloud-run,
	// domains, then anything else alphabetically.
	GatusGroups []GatusGroup

	Repos     []RepoGroup
	Errors    []string
	FetchedAt string
}

// GatusGroup is one named group of Gatus endpoint rows.
type GatusGroup struct {
	Name    string
	Entries []GatusEntry
}

// GatusEntry is one Gatus endpoint row in the health-monitor section.
type GatusEntry struct {
	Name   string
	Key    string
	Status data.Status
	// URL is the Gatus endpoint detail page link (e.g. /endpoints/<key>).
	URL string
}

// ServiceCard is one service tile. Endpoints is a flat ordered list:
// localhost first (if any), then cloud run, then custom domains.
// Each endpoint gets its own health dot.
type ServiceCard struct {
	Name         string
	Description  string
	Endpoints    []Endpoint
	Pipelines    []WorkflowRow // all pipelines for this service across envs
	LogCount     int
	LogSearchURL string
}

// Endpoint is one reachable instance of a service.
// Label is the short display text shown next to the dot.
// URL is the clickable href (may be empty for socket-only endpoints like postgres).
// SubURL is an optional faded line beneath — used to show the raw Cloud Run URL
// under a custom domain entry.
// IsLocal marks the localhost entry so the template can render "localhost" prefix.
type Endpoint struct {
	Label       string
	URL         string // primary clickable href
	SubURL      string // faded line beneath (raw Cloud Run URL for domain entries)
	SubLabel    string // label for SubURL (e.g. "nexus-cal-dev-....run.app")
	GatusKey    string
	GatusStatus data.Status
	IsLocal     bool // true for the localhost endpoint
}

// WorkflowRow holds a single CI/deploy pipeline status + link.
type WorkflowRow struct {
	Label     string
	Status    data.CIStatus
	URL       string
	UpdatedAt string
	RunNumber int
}

// RepoGroup is a repo with its Gitea/GitHub links and all associated pipelines.
type RepoGroup struct {
	Name      string
	GitHubURL string
	GiteaURL  string
	Pipelines []WorkflowRow
}

func trimYML(s string) string { return strings.TrimSuffix(s, ".yml") }

// ooLogsURL returns the OpenObserve logs page URL.
func ooLogsURL(baseURL, _ string) string {
	return fmt.Sprintf("%s/web/logs?org_identifier=default", baseURL)
}

func buildPageData(
	cfg Config,
	gatus map[string]data.GatusResult,
	ghNexus map[string]data.WorkflowRun,
	ghCtl map[string]data.WorkflowRun,
	giteaNexus map[string]data.WorkflowRun,
	giteaCtl map[string]data.WorkflowRun,
	logCounts map[string]int,
	discovered []discover.ServiceDef,
	errors []string,
) TemplateData {
	gs := func(key string) data.Status {
		if r, ok := gatus[key]; ok {
			return r.Status
		}
		return data.StatusUnknown
	}

	toRow := func(file string, r data.WorkflowRun) WorkflowRow {
		return WorkflowRow{
			Label:     trimYML(file),
			Status:    r.Status,
			URL:       r.URL,
			UpdatedAt: r.UpdatedAt,
			RunNumber: r.RunNumber,
		}
	}
	ghRow := func(m map[string]data.WorkflowRun, file string) WorkflowRow {
		r, ok := m[file]
		if !ok {
			return WorkflowRow{Label: trimYML(file), Status: data.CIUnknown}
		}
		return toRow(file, r)
	}
	giteaRow := func(m map[string]data.WorkflowRun, file string) WorkflowRow {
		r, ok := m[file]
		if !ok {
			return WorkflowRow{Label: trimYML(file), Status: data.CIUnknown}
		}
		return toRow(file, r)
	}
	ghRows := func(m map[string]data.WorkflowRun, files ...string) []WorkflowRow {
		rows := make([]WorkflowRow, 0, len(files))
		for _, f := range files {
			rows = append(rows, ghRow(m, f))
		}
		return rows
	}
	giteaRows := func(m map[string]data.WorkflowRun, files ...string) []WorkflowRow {
		rows := make([]WorkflowRow, 0, len(files))
		for _, f := range files {
			rows = append(rows, giteaRow(m, f))
		}
		return rows
	}

	// localEp builds the localhost endpoint for a service.
	localEp := func(label, href, gatusKey string) Endpoint {
		return Endpoint{
			Label:       label,
			URL:         href,
			GatusKey:    gatusKey,
			GatusStatus: gs(gatusKey),
			IsLocal:     true,
		}
	}

	// cloudEp builds a cloud run endpoint (no custom domain).
	cloudEp := func(label, href, gatusKey string) Endpoint {
		return Endpoint{
			Label:       label,
			URL:         href,
			GatusKey:    gatusKey,
			GatusStatus: gs(gatusKey),
		}
	}

	// domainEp builds a custom domain endpoint with the raw Cloud Run URL shown faded beneath.
	domainEp := func(domain, domainURL, cloudRunURL, gatusKey string) Endpoint {
		// Extract just the hostname from cloudRunURL for the sub-label.
		subLabel := strings.TrimPrefix(cloudRunURL, "https://")
		subLabel = strings.TrimPrefix(subLabel, "http://")
		subLabel = strings.SplitN(subLabel, "/", 2)[0]
		return Endpoint{
			Label:       domain,
			URL:         domainURL,
			SubURL:      cloudRunURL,
			SubLabel:    subLabel,
			GatusKey:    gatusKey,
			GatusStatus: gs(gatusKey),
		}
	}

	// card builds a ServiceCard.
	card := func(name, desc, svcName string, eps []Endpoint, pipelines []WorkflowRow) ServiceCard {
		return ServiceCard{
			Name:         name,
			Description:  desc,
			Endpoints:    eps,
			Pipelines:    pipelines,
			LogCount:     logCounts[svcName],
			LogSearchURL: ooLogsURL(cfg.OpenObserveBrowserURL, svcName),
		}
	}

	// Convenience: combined pipeline slices.
	dockerDeploy := giteaRows(giteaNexus, "docker-deploy.yml")
	dockerInstall := giteaRows(giteaNexus, "docker-deploy.yml", "install.yml")
	// vnDeploy includes the Gitea deploy-vn workflow in addition to docker-deploy.
	vnDeploy := giteaRows(giteaNexus, "deploy-vn.yml", "docker-deploy.yml")

	// ---- App services ----
	appServices := []ServiceCard{
		card("hermit", "rust grpc server", "hermit",
			[]Endpoint{
				localEp(":9090", "http://localhost:9090", "local_hermit"),
				cloudEp("nexus-hermit-dev", "https://nexus-hermit-dev-2tvic4xjjq-uc.a.run.app", "cloud-run_hermit (cloud)"),
			},
			append(dockerInstall, ghRows(ghNexus, "deploy-hermit-dev.yml")...),
		),
		card("secrets", "confessions api", "secrets",
			[]Endpoint{
				localEp(":8081", "http://localhost:8081", "local_secrets"),
				cloudEp("nexus-secrets-dev", "https://nexus-secrets-dev-2tvic4xjjq-uc.a.run.app/health", "cloud-run_secrets (cloud)"),
				domainEp("secrets.jredh.com", "https://secrets.jredh.com", "https://nexus-secrets-dev-2tvic4xjjq-uc.a.run.app", "domains_secrets.jredh.com"),
			},
			append(dockerDeploy, ghRows(ghNexus, "deploy-go-http-dev.yml")...),
		),
		card("portal", "web portal / admin", "portal",
			[]Endpoint{
				localEp(":8090", "http://localhost:8090/login", "local_portal"),
				cloudEp("nexus-portal-dev", "https://nexus-portal-dev-2tvic4xjjq-uc.a.run.app", "cloud-run_portal (cloud)"),
			},
			append(dockerDeploy, ghRows(ghNexus, "deploy-portal-dev.yml")...),
		),
		card("web", "astro frontend", "web",
			[]Endpoint{
				localEp(":8083", "http://localhost:8083", "local_web"),
				cloudEp("nexus-web-dev", "https://nexus-web-dev-2tvic4xjjq-uc.a.run.app", "cloud-run_web (cloud)"),
				domainEp("portal.jredh.com", "https://portal.jredh.com", "https://nexus-web-dev-2tvic4xjjq-uc.a.run.app", "domains_portal.jredh.com"),
			},
			append(dockerDeploy, ghRows(ghNexus, "deploy-web-dev.yml")...),
		),
		card("vn", "visual novel engine", "vn",
			[]Endpoint{
				localEp(":8082", "http://localhost:8082/health", "local_vn"),
				cloudEp("nexus-vn-dev", "https://nexus-vn-dev-2tvic4xjjq-uc.a.run.app/health", "cloud-run_vn (cloud)"),
			},
			append(vnDeploy, ghRows(ghNexus, "deploy-vn-dev.yml")...),
		),
		card("deadman", "sms deadman switch", "deadman",
			[]Endpoint{
				localEp(":8095", "http://localhost:8095/health", "local_deadman"),
				cloudEp("nexus-deadman-dev", "https://nexus-deadman-dev-2tvic4xjjq-uc.a.run.app", "cloud-run_deadman (cloud)"),
				domainEp("deadman.jredh.com", "https://deadman.jredh.com", "https://nexus-deadman-dev-2tvic4xjjq-uc.a.run.app", "domains_deadman.jredh.com"),
			},
			append(dockerDeploy, ghRows(ghNexus, "deploy-deadman-dev.yml")...),
		),
		card("ref", "reference / docs", "ref",
			[]Endpoint{localEp(":8086", "http://localhost:8086", "local_ref")},
			dockerDeploy,
		),
		card("matrix", "this page", "matrix",
			[]Endpoint{localEp(":8085", "http://localhost:8085", "local_matrix")},
			dockerDeploy,
		),
		card("opencode", "ai coding agent", "opencode",
			[]Endpoint{localEp(":4096", cfg.OpenCodeURL, "local_opencode")},
			nil,
		),
	}

	// ---- Cloud-only services ----
	cloudServices := []ServiceCard{
		card("cal", "calendar / ical", "cal",
			[]Endpoint{
				cloudEp("nexus-cal-dev", "https://nexus-cal-dev-2tvic4xjjq-uc.a.run.app/health", "cloud-run_cal (cloud)"),
				domainEp("cal.jredh.com", "https://cal.jredh.com", "https://nexus-cal-dev-2tvic4xjjq-uc.a.run.app", "domains_cal.jredh.com"),
			},
			ghRows(ghNexus, "deploy-cal-dev.yml"),
		),
	}

	// ---- Infra (local only) ----
	infraServices := []ServiceCard{
		card("postgres", "postgresql 16", "postgres",
			[]Endpoint{localEp("/tmp/ctl-pg", "", "local_postgres")},
			nil,
		),
		card("vault", "secret management", "vault",
			[]Endpoint{localEp(":8200", "http://localhost:8200", "local_vault")},
			nil,
		),
		card("gitea", "local git + ci", "gitea",
			[]Endpoint{localEp(":3000", "http://localhost:3000", "local_gitea")},
			nil,
		),
		card("openobserve", "log aggregation", "openobserve",
			[]Endpoint{localEp(":5080", cfg.OpenObserveBrowserURL, "local_openobserve")},
			nil,
		),
		card("vector", "log shipper", "vector",
			[]Endpoint{localEp("docker", "", "local_vector")},
			nil,
		),
		card("act_runner", "gitea ci runner", "act_runner",
			[]Endpoint{localEp("docker", "", "local_act_runner")},
			nil,
		),
		card("gatus", "health monitor", "gatus",
			[]Endpoint{localEp(":8084", "http://localhost:8084", "local_gatus")},
			nil,
		),
	}

	// ---- Auto-discovered services (from Docker labels) ----
	// Build a set of service names already represented in the hardcoded lists so
	// we don't duplicate them. Discovered services not in this set get simple cards
	// appended to the appropriate group.
	known := make(map[string]bool)
	for _, s := range appServices {
		known[s.Name] = true
	}
	for _, s := range cloudServices {
		known[s.Name] = true
	}
	for _, s := range infraServices {
		known[s.Name] = true
	}

	for _, svc := range discovered {
		if known[svc.Name] {
			continue
		}
		// Build a minimal endpoint from nexus.port (if set) or nexus.health_url.
		var eps []Endpoint
		gatusKey := "local_" + svc.Name
		if svc.Port != "" {
			label := ":" + svc.Port
			href := ""
			if svc.HealthURL != "" {
				href = svc.HealthURL
			} else if svc.HealthType != "none" {
				href = "http://localhost:" + svc.Port
			}
			eps = append(eps, localEp(label, href, gatusKey))
		} else if svc.HealthURL != "" && svc.HealthType != "none" {
			eps = append(eps, localEp(svc.HealthURL, svc.HealthURL, gatusKey))
		} else {
			// No HTTP endpoint — show as presence-only (no clickable link).
			eps = append(eps, localEp("docker", "", gatusKey))
		}

		// Build workflow rows from nexus.workflows label.
		var pipelines []WorkflowRow
		for _, wf := range svc.Workflows {
			pipelines = append(pipelines, giteaRow(giteaNexus, wf))
		}

		desc := svc.Description
		autoCard := card(svc.Name, desc, svc.Name, eps, pipelines)

		switch svc.Group {
		case "cloud":
			cloudServices = append(cloudServices, autoCard)
		case "infra":
			infraServices = append(infraServices, autoCard)
		default: // "app" or anything else
			appServices = append(appServices, autoCard)
		}
		known[svc.Name] = true
	}

	repos := []RepoGroup{
		{
			Name:      "nexus",
			GitHubURL: "https://github.com/jredh-dev/nexus",
			GiteaURL:  "http://localhost:3000/jredh-dev/nexus",
			Pipelines: append(
				ghRows(ghNexus, "ci.yml", "integration-tests-dev.yml"),
				giteaRows(giteaNexus, "ci.yml", "deploy-vn.yml", "docker-deploy.yml", "install.yml")...,
			),
		},
		{
			Name:      "ctl",
			GitHubURL: "https://github.com/jredh-dev/ctl",
			GiteaURL:  "http://localhost:3000/jredh-dev/ctl",
			Pipelines: append(
				ghRows(ghCtl, "ci.yml"),
				giteaRows(giteaCtl, "install.yml")...,
			),
		},
	}

	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		loc = time.UTC
	}
	return TemplateData{
		GatusURL:       cfg.GatusURL,
		OpenObserveURL: cfg.OpenObserveBrowserURL,
		OpenCodeURL:    cfg.OpenCodeURL,
		AppServices:    appServices,
		CloudServices:  cloudServices,
		InfraServices:  infraServices,
		GatusGroups:    buildGatusGroups(cfg.GatusURL, gatus),
		Repos:          repos,
		Errors:         errors,
		FetchedAt:      time.Now().In(loc).Format("15:04:05 MST"),
	}
}

// buildGatusGroups converts the raw Gatus result map into ordered GatusGroup
// slices for the health-monitor section. Group order: local, cloud-run,
// domains, then anything else alphabetically.
func buildGatusGroups(gatusBaseURL string, results map[string]data.GatusResult) []GatusGroup {
	// Canonical group order.
	orderedGroups := []string{"local", "cloud-run", "domains"}

	// Collect entries per group.
	groupMap := map[string][]GatusEntry{}
	for _, r := range results {
		entry := GatusEntry{
			Name:   r.Name,
			Key:    r.Key,
			Status: r.Status,
			// Gatus detail page: baseURL + /endpoints/<key>
			URL: gatusBaseURL + "/endpoints/" + r.Key,
		}
		groupMap[r.Group] = append(groupMap[r.Group], entry)
	}

	// Sort entries within each group alphabetically by name.
	for grp := range groupMap {
		entries := groupMap[grp]
		for i := 1; i < len(entries); i++ {
			for j := i; j > 0 && entries[j].Name < entries[j-1].Name; j-- {
				entries[j], entries[j-1] = entries[j-1], entries[j]
			}
		}
		groupMap[grp] = entries
	}

	// Append any groups not in the canonical order (alphabetically).
	extra := []string{}
	known := map[string]bool{}
	for _, g := range orderedGroups {
		known[g] = true
	}
	for g := range groupMap {
		if !known[g] {
			extra = append(extra, g)
		}
	}
	for i := 1; i < len(extra); i++ {
		for j := i; j > 0 && extra[j] < extra[j-1]; j-- {
			extra[j], extra[j-1] = extra[j-1], extra[j]
		}
	}
	allGroups := append(orderedGroups, extra...)

	var out []GatusGroup
	for _, grp := range allGroups {
		entries, ok := groupMap[grp]
		if !ok {
			continue
		}
		out = append(out, GatusGroup{Name: grp, Entries: entries})
	}
	return out
}

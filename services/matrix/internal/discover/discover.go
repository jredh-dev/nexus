// Package discover queries the Docker socket for containers labelled
// nexus.monitor=true and returns a structured description of each service.
//
// Labels consumed (all optional except nexus.monitor):
//
//	nexus.monitor      — "true" to include this container
//	nexus.group        — "app", "infra", "cloud" (default: "app")
//	nexus.description  — one-liner shown in the matrix card
//	nexus.health_url   — primary URL for health probing (also used as card link)
//	nexus.health_type  — "http", "tcp", "none"
//	nexus.port         — host-side port (used to build the localhost href)
//	nexus.workflows    — comma-separated workflow filenames (e.g. "ci.yml,deploy-foo.yml")
package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// ServiceDef is one discovered service.
type ServiceDef struct {
	// Name is the container name, stripped of leading "/" and the "agentic-" prefix.
	Name string

	// Group is one of "app", "infra", "cloud".
	Group string

	// Description is the nexus.description label value, or empty.
	Description string

	// HealthURL is the nexus.health_url label value, or empty.
	HealthURL string

	// HealthType is "http", "tcp", or "none".
	HealthType string

	// Port is the nexus.port label value (as a string), or empty.
	// Used by the page handler to build the localhost clickable href.
	Port string

	// Workflows is the split nexus.workflows label (comma-separated filenames).
	Workflows []string
}

// containerInfo is a minimal Docker API /containers/json response element.
type containerInfo struct {
	Names  []string          `json:"Names"`
	Labels map[string]string `json:"Labels"`
	State  string            `json:"State"`
}

// Services queries the Docker socket and returns all containers with
// nexus.monitor=true, ordered by group (app, infra, cloud, then alphabetical)
// then name.
func Services(ctx context.Context) ([]ServiceDef, error) {
	containers, err := listContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover: docker list: %w", err)
	}

	var svcs []ServiceDef
	for _, c := range containers {
		if c.Labels["nexus.monitor"] != "true" {
			continue
		}
		name := containerName(c)
		workflows := parseWorkflows(c.Labels["nexus.workflows"])
		svcs = append(svcs, ServiceDef{
			Name:        name,
			Group:       labelOr(c.Labels, "nexus.group", "app"),
			Description: c.Labels["nexus.description"],
			HealthURL:   c.Labels["nexus.health_url"],
			HealthType:  labelOr(c.Labels, "nexus.health_type", "http"),
			Port:        c.Labels["nexus.port"],
			Workflows:   workflows,
		})
	}

	return svcs, nil
}

// listContainers calls the Docker API via the Unix socket.
func listContainers(ctx context.Context) ([]containerInfo, error) {
	sock := os.Getenv("DOCKER_HOST")
	if sock == "" {
		sock = "/var/run/docker.sock"
	}
	sock = strings.TrimPrefix(sock, "unix://")

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", sock)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/containers/json?all=false", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var containers []containerInfo
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return nil, err
	}
	return containers, nil
}

// containerName strips the leading "/" and "agentic-" prefix from Docker
// container names, matching the convention used by gatus-config-gen.
func containerName(c containerInfo) string {
	if len(c.Names) == 0 {
		return "unknown"
	}
	name := strings.TrimPrefix(c.Names[0], "/")
	name = strings.TrimPrefix(name, "agentic-")
	return name
}

func labelOr(labels map[string]string, key, def string) string {
	if v, ok := labels[key]; ok && v != "" {
		return v
	}
	return def
}

// parseWorkflows splits a comma-separated workflow filename string.
// Empty strings and blank entries are dropped.
func parseWorkflows(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

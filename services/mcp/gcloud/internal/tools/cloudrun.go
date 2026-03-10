// Cloud Run tool: get_service_status
// Returns service metadata, latest revision, traffic splits, and readiness.
package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/jredh-dev/nexus/internal/mcp"
)

// cloudRunBase is the Cloud Run v1 API base URL.
const cloudRunBase = "https://run.googleapis.com/v1"

func registerCloudRunTools(s *mcp.Server) {
	s.RegisterTool(mcp.Tool{
		Name:        "get_service_status",
		Description: "Get Cloud Run service status: readiness, latest revision, traffic splits, and container image. Read-only.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"service": {
					Type:        "string",
					Description: "Cloud Run service name (e.g. nexus-portal-dev)",
				},
				"project": {
					Type:        "string",
					Description: "GCP project ID (default: dea-noctua)",
					Default:     "dea-noctua",
				},
				"region": {
					Type:        "string",
					Description: "GCP region (default: us-central1)",
					Default:     "us-central1",
				},
			},
			Required: []string{"service"},
		},
	}, handleGetServiceStatus)
}

type getServiceStatusArgs struct {
	Service string `json:"service"`
	Project string `json:"project"`
	Region  string `json:"region"`
}

func handleGetServiceStatus(raw json.RawMessage) (*mcp.ToolCallResult, error) {
	var args getServiceStatusArgs
	if err := parseArgs(raw, &args); err != nil {
		return nil, err
	}
	if args.Service == "" {
		return nil, errMissing("service")
	}
	if args.Project == "" {
		args.Project = "dea-noctua"
	}
	if args.Region == "" {
		args.Region = "us-central1"
	}

	token, err := getAccessToken()
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	// Fetch the service resource from Cloud Run API.
	url := fmt.Sprintf("%s/namespaces/%s/services/%s", cloudRunBase, args.Project, args.Service)
	body, err := gcpGET(url, token)
	if err != nil {
		return nil, err
	}

	// Parse only the fields we care about — return structured summary.
	var svc cloudRunService
	if err := json.Unmarshal(body, &svc); err != nil {
		return nil, fmt.Errorf("parse service response: %w", err)
	}

	summary := buildServiceSummary(args.Service, args.Project, args.Region, &svc)
	return jsonResult(summary)
}

// cloudRunService is a partial Cloud Run Service resource.
type cloudRunService struct {
	Metadata struct {
		Name              string            `json:"name"`
		Namespace         string            `json:"namespace"`
		CreationTimestamp string            `json:"creationTimestamp"`
		Labels            map[string]string `json:"labels"`
	} `json:"metadata"`
	Status struct {
		ObservedGeneration int    `json:"observedGeneration"`
		URL                string `json:"url"`
		Conditions         []struct {
			Type               string `json:"type"`
			Status             string `json:"status"`
			LastTransitionTime string `json:"lastTransitionTime"`
			Message            string `json:"message,omitempty"`
			Reason             string `json:"reason,omitempty"`
		} `json:"conditions"`
		LatestCreatedRevisionName string `json:"latestCreatedRevisionName"`
		LatestReadyRevisionName   string `json:"latestReadyRevisionName"`
		Traffic                   []struct {
			RevisionName   string `json:"revisionName"`
			Percent        int    `json:"percent"`
			LatestRevision bool   `json:"latestRevision"`
			Tag            string `json:"tag,omitempty"`
			URL            string `json:"url,omitempty"`
		} `json:"traffic"`
	} `json:"status"`
	Spec struct {
		Template struct {
			Spec struct {
				Containers []struct {
					Image string `json:"image"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
}

type serviceSummary struct {
	Name                  string          `json:"name"`
	Project               string          `json:"project"`
	Region                string          `json:"region"`
	URL                   string          `json:"url"`
	Ready                 bool            `json:"ready"`
	LatestCreatedRevision string          `json:"latest_created_revision"`
	LatestReadyRevision   string          `json:"latest_ready_revision"`
	Image                 string          `json:"image"`
	Traffic               []trafficTarget `json:"traffic"`
	Conditions            []condSummary   `json:"conditions"`
}

type trafficTarget struct {
	Revision string `json:"revision"`
	Percent  int    `json:"percent"`
	Latest   bool   `json:"latest"`
	Tag      string `json:"tag,omitempty"`
}

type condSummary struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

func buildServiceSummary(name, project, region string, svc *cloudRunService) serviceSummary {
	s := serviceSummary{
		Name:                  name,
		Project:               project,
		Region:                region,
		URL:                   svc.Status.URL,
		LatestCreatedRevision: svc.Status.LatestCreatedRevisionName,
		LatestReadyRevision:   svc.Status.LatestReadyRevisionName,
	}

	// Image from first container.
	if len(svc.Spec.Template.Spec.Containers) > 0 {
		s.Image = svc.Spec.Template.Spec.Containers[0].Image
	}

	// Ready = "Ready" condition is "True".
	for _, c := range svc.Status.Conditions {
		s.Conditions = append(s.Conditions, condSummary{
			Type:    c.Type,
			Status:  c.Status,
			Message: c.Message,
			Reason:  c.Reason,
		})
		if c.Type == "Ready" && c.Status == "True" {
			s.Ready = true
		}
	}

	for _, t := range svc.Status.Traffic {
		s.Traffic = append(s.Traffic, trafficTarget{
			Revision: t.RevisionName,
			Percent:  t.Percent,
			Latest:   t.LatestRevision,
			Tag:      t.Tag,
		})
	}

	return s
}

// ── GCP HTTP helpers ──────────────────────────────────────────────────────────

// gcpGET performs an authenticated GET to a GCP API endpoint.
func gcpGET(url, token string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GCP API error (status %d) %s: %s", resp.StatusCode, url, body)
	}
	return body, nil
}

// getAccessToken retrieves a GCP access token using the Application Default Credentials.
// It calls the metadata server if running on GCP, otherwise uses the ADC token from
// the gcloud CLI or the service account key file.
func getAccessToken() (string, error) {
	// Try the metadata server first (works inside GCP / Cloud Run).
	req, _ := http.NewRequest("GET",
		"http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token",
		nil)
	req.Header.Set("Metadata-Flavor", "Google")
	client := &http.Client{Timeout: 2e9} // 2s
	resp, err := client.Do(req)
	if err == nil && resp.StatusCode == 200 {
		defer resp.Body.Close()
		var tok struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tok); err == nil && tok.AccessToken != "" {
			return tok.AccessToken, nil
		}
	}

	// Fall back to gcloud ADC: run `gcloud auth application-default print-access-token`.
	// Requires GOOGLE_APPLICATION_CREDENTIALS or `gcloud auth application-default login`.
	return getTokenFromGcloud()
}

// getTokenFromGcloud runs `gcloud auth print-access-token` to get a short-lived token.
func getTokenFromGcloud() (string, error) {
	// Use the SA key file if set (Docker container scenario).
	credFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if credFile != "" {
		return getTokenFromSAKey(credFile)
	}
	return "", fmt.Errorf("GOOGLE_APPLICATION_CREDENTIALS not set and not running on GCP")
}

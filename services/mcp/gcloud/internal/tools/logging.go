// logging.go — Cloud Logging tool: query_logs
// Queries Cloud Logging v2 entries:list API with a structured filter.
package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jredh-dev/nexus/internal/mcp"
)

const loggingBase = "https://logging.googleapis.com/v2"

func registerLoggingTools(s *mcp.Server) {
	s.RegisterTool(mcp.Tool{
		Name:        "query_logs",
		Description: "Query Cloud Logging log entries for a GCP project. Supports filtering by service name, severity level, and time range. Read-only.",
		InputSchema: mcp.ToolSchema{
			Type: "object",
			Properties: map[string]mcp.SchemaProperty{
				"project": {
					Type:        "string",
					Description: "GCP project ID (default: dea-noctua)",
					Default:     "dea-noctua",
				},
				"service": {
					Type:        "string",
					Description: "Cloud Run service name to filter logs (e.g. nexus-portal-dev). Omit to query all services.",
				},
				"severity": {
					Type:        "string",
					Description: "Minimum severity level: DEBUG, INFO, WARNING, ERROR, CRITICAL (default: WARNING)",
					Default:     "WARNING",
				},
				"minutes": {
					Type:        "number",
					Description: "How many minutes back to search (default: 60, max: 1440)",
					Default:     "60",
				},
				"limit": {
					Type:        "number",
					Description: "Maximum number of log entries to return (default: 50, max: 200)",
					Default:     "50",
				},
			},
			Required: []string{},
		},
	}, handleQueryLogs)
}

type queryLogsArgs struct {
	Project  string  `json:"project"`
	Service  string  `json:"service"`
	Severity string  `json:"severity"`
	Minutes  float64 `json:"minutes"`
	Limit    float64 `json:"limit"`
}

func handleQueryLogs(raw json.RawMessage) (*mcp.ToolCallResult, error) {
	var args queryLogsArgs
	if err := parseArgs(raw, &args); err != nil {
		return nil, err
	}

	// Apply defaults.
	if args.Project == "" {
		args.Project = "dea-noctua"
	}
	if args.Severity == "" {
		args.Severity = "WARNING"
	}
	if args.Minutes <= 0 {
		args.Minutes = 60
	}
	if args.Minutes > 1440 {
		args.Minutes = 1440
	}
	if args.Limit <= 0 {
		args.Limit = 50
	}
	if args.Limit > 200 {
		args.Limit = 200
	}

	token, err := getAccessToken()
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	// Build the log filter expression.
	filter := buildLogFilter(args)

	// Build the request body for entries:list.
	reqBody := logEntriesRequest{
		ResourceNames: []string{"projects/" + args.Project},
		Filter:        filter,
		OrderBy:       "timestamp desc",
		PageSize:      int(args.Limit),
	}
	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// POST to Cloud Logging entries:list.
	apiURL := loggingBase + "/entries:list"
	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("logging API request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Cloud Logging API error (status %d): %s", resp.StatusCode, body)
	}

	// Parse and summarize the response.
	var apiResp logEntriesResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parse log response: %w", err)
	}

	summary := buildLogSummary(args, filter, &apiResp)
	return jsonResult(summary)
}

// buildLogFilter constructs a Cloud Logging filter expression from the query args.
func buildLogFilter(args queryLogsArgs) string {
	var parts []string

	// Time filter — go back N minutes from now.
	cutoff := time.Now().UTC().Add(-time.Duration(args.Minutes) * time.Minute)
	parts = append(parts, fmt.Sprintf("timestamp >= %q", cutoff.Format(time.RFC3339)))

	// Severity filter — Cloud Logging uses >= semantics with severity levels.
	// Map the requested minimum to the set of accepted levels.
	severities := severitiesAtOrAbove(strings.ToUpper(args.Severity))
	if len(severities) > 0 {
		parts = append(parts, "("+strings.Join(severities, " OR ")+")")
	}

	// Service filter — match the Cloud Run service name in the resource labels.
	if args.Service != "" {
		parts = append(parts, fmt.Sprintf(`resource.labels.service_name=%q`, args.Service))
	}

	return strings.Join(parts, " AND ")
}

// severitiesAtOrAbove returns Cloud Logging severity filter clauses for all levels
// at or above the given minimum.
func severitiesAtOrAbove(min string) []string {
	ordered := []string{"DEBUG", "INFO", "NOTICE", "WARNING", "ERROR", "CRITICAL", "ALERT", "EMERGENCY"}
	idx := -1
	for i, s := range ordered {
		if s == min {
			idx = i
			break
		}
	}
	if idx < 0 {
		idx = 0 // unknown severity — include everything
	}
	var out []string
	for _, s := range ordered[idx:] {
		out = append(out, "severity="+s)
	}
	return out
}

// ── API request / response types ─────────────────────────────────────────────

type logEntriesRequest struct {
	ResourceNames []string `json:"resourceNames"`
	Filter        string   `json:"filter"`
	OrderBy       string   `json:"orderBy"`
	PageSize      int      `json:"pageSize"`
}

type logEntriesResponse struct {
	Entries       []logEntry `json:"entries"`
	NextPageToken string     `json:"nextPageToken"`
}

type logEntry struct {
	LogName   string `json:"logName"`
	Timestamp string `json:"timestamp"`
	Severity  string `json:"severity"`
	Resource  struct {
		Type   string            `json:"type"`
		Labels map[string]string `json:"labels"`
	} `json:"resource"`
	// Log payload — can be textPayload, jsonPayload, or protoPayload.
	TextPayload  string          `json:"textPayload"`
	JSONPayload  json.RawMessage `json:"jsonPayload"`
	ProtoPayload json.RawMessage `json:"protoPayload"`
	// HTTP request details if present.
	HTTPRequest *struct {
		RequestMethod string `json:"requestMethod"`
		RequestURL    string `json:"requestURL"`
		Status        int    `json:"status"`
		Latency       string `json:"latency"`
	} `json:"httpRequest"`
	Labels map[string]string `json:"labels"`
}

// ── Output summary ────────────────────────────────────────────────────────────

type logQuerySummary struct {
	Project    string        `json:"project"`
	Service    string        `json:"service,omitempty"`
	Filter     string        `json:"filter"`
	TotalFound int           `json:"total_found"`
	Entries    []logEntryFmt `json:"entries"`
}

type logEntryFmt struct {
	Timestamp string `json:"timestamp"`
	Severity  string `json:"severity"`
	Service   string `json:"service,omitempty"`
	Message   string `json:"message"`
}

func buildLogSummary(args queryLogsArgs, filter string, resp *logEntriesResponse) logQuerySummary {
	s := logQuerySummary{
		Project: args.Project,
		Service: args.Service,
		Filter:  filter,
	}

	for _, e := range resp.Entries {
		msg := entryMessage(&e)
		svc := e.Resource.Labels["service_name"]
		s.Entries = append(s.Entries, logEntryFmt{
			Timestamp: e.Timestamp,
			Severity:  e.Severity,
			Service:   svc,
			Message:   msg,
		})
	}
	s.TotalFound = len(s.Entries)
	return s
}

// entryMessage extracts the best human-readable message from a log entry.
// Priority: textPayload → jsonPayload.message → jsonPayload → "(no message)"
func entryMessage(e *logEntry) string {
	if e.TextPayload != "" {
		return e.TextPayload
	}
	if len(e.JSONPayload) > 0 {
		// Try to extract a "message" or "msg" field.
		var m map[string]json.RawMessage
		if err := json.Unmarshal(e.JSONPayload, &m); err == nil {
			for _, key := range []string{"message", "msg", "error", "err"} {
				if v, ok := m[key]; ok {
					var s string
					if err := json.Unmarshal(v, &s); err == nil {
						return s
					}
				}
			}
		}
		// Fall back to the raw JSON payload (truncated).
		raw := string(e.JSONPayload)
		if len(raw) > 200 {
			raw = raw[:200] + "…"
		}
		return raw
	}
	if e.HTTPRequest != nil {
		return fmt.Sprintf("%s %s → %d", e.HTTPRequest.RequestMethod, e.HTTPRequest.RequestURL, e.HTTPRequest.Status)
	}
	return "(no message)"
}

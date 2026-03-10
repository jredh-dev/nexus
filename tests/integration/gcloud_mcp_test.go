//go:build integration

package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// gcloudMCPURL returns the base URL for the gcloud-mcp service.
// Reads GCLOUD_MCP_URL; defaults to http://localhost:8093.
func gcloudMCPURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("GCLOUD_MCP_URL")
	if u == "" {
		u = "http://localhost:8093"
	}
	return strings.TrimRight(u, "/")
}

// gcloudCall is a thin wrapper around mcpCall that targets the gcloud-mcp server.
// It skips the test (rather than fataling) if the server is not reachable, so
// CI environments without gcloud-mcp deployed don't produce false failures.
func gcloudCall(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	return mcpCallOrSkip(t, gcloudMCPURL(t), method, params)
}

// getResult extracts result["result"] as a map; fatals if absent.
func gcloudGetResult(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	raw, ok := resp["result"]
	if !ok {
		t.Fatalf("response has no 'result' field: %v", resp)
	}
	m, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T %v", raw, raw)
	}
	return m
}

// gcloudGetContent extracts result.content as a []any; fatals if absent or empty.
func gcloudGetContent(t *testing.T, result map[string]any) []any {
	t.Helper()
	raw, ok := result["content"]
	if !ok {
		t.Fatalf("result has no 'content' field: %v", result)
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("content is not an array: %T %v", raw, raw)
	}
	if len(arr) == 0 {
		t.Fatalf("content array is empty")
	}
	return arr
}

// gcloudContentText returns content[0].text; fatals if type != "text" or text is absent.
func gcloudContentText(t *testing.T, content []any) string {
	t.Helper()
	block, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] is not a map: %T", content[0])
	}
	typ, _ := block["type"].(string)
	if typ != "text" {
		t.Errorf("content[0].type: got %q, want \"text\"", typ)
	}
	text, _ := block["text"].(string)
	return text
}

// isToolError returns true if result.isError == true.
func isToolError(result map[string]any) bool {
	v, _ := result["isError"].(bool)
	return v
}

// ── health ────────────────────────────────────────────────────────────────────

func TestGcloudMCPHealth(t *testing.T) {
	url := gcloudMCPURL(t)
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(url + "/health")
	if err != nil {
		if isConnRefused(err) {
			t.Skipf("skipping: gcloud-mcp not reachable (%v)", err)
		}
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /health: status %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read health body: %v", err)
	}
	if !strings.Contains(string(body), "gcloud-mcp") {
		t.Errorf("health body does not contain \"gcloud-mcp\": %s", body)
	}
}

// ── initialize ────────────────────────────────────────────────────────────────

func TestGcloudMCPInitialize(t *testing.T) {
	resp := gcloudCall(t, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "0"},
	})

	if _, hasErr := resp["error"]; hasErr {
		t.Fatalf("initialize returned JSON-RPC error: %v", resp["error"])
	}

	result := gcloudGetResult(t, resp)

	// serverInfo.name
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("result.serverInfo missing or not an object: %v", result["serverInfo"])
	}
	if name, _ := serverInfo["name"].(string); name != "gcloud-mcp" {
		t.Errorf("serverInfo.name: got %q, want \"gcloud-mcp\"", name)
	}

	// protocolVersion
	if pv, _ := result["protocolVersion"].(string); pv != "2025-03-26" {
		t.Errorf("protocolVersion: got %q, want \"2025-03-26\"", pv)
	}

	// capabilities.tools must be present
	caps, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("result.capabilities missing or not an object: %v", result["capabilities"])
	}
	if _, hasTools := caps["tools"]; !hasTools {
		t.Errorf("capabilities.tools not present: %v", caps)
	}
}

// ── tools/list ────────────────────────────────────────────────────────────────

func TestGcloudMCPToolsList(t *testing.T) {
	resp := gcloudCall(t, "tools/list", nil)

	if _, hasErr := resp["error"]; hasErr {
		t.Fatalf("tools/list returned JSON-RPC error: %v", resp["error"])
	}

	result := gcloudGetResult(t, resp)
	toolsRaw, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("result.tools missing or not an array: %v", result["tools"])
	}

	// Build a map of name → tool for O(1) lookup.
	toolMap := make(map[string]map[string]any, len(toolsRaw))
	for _, raw := range toolsRaw {
		tool, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("tools entry is not an object: %T", raw)
		}
		name, _ := tool["name"].(string)
		toolMap[name] = tool
	}

	expected := []string{"get_service_status", "query_logs", "terraform_plan"}

	// Exact match: no more, no fewer.
	if len(toolMap) != len(expected) {
		names := make([]string, 0, len(toolMap))
		for n := range toolMap {
			names = append(names, n)
		}
		t.Errorf("tools count: got %d (%v), want %d (%v)", len(toolMap), names, len(expected), expected)
	}

	for _, name := range expected {
		tool, ok := toolMap[name]
		if !ok {
			t.Errorf("tool %q not found in list", name)
			continue
		}

		// Non-empty description.
		desc, _ := tool["description"].(string)
		if strings.TrimSpace(desc) == "" {
			t.Errorf("tool %q has empty description", name)
		}

		// inputSchema.type == "object"
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok {
			t.Errorf("tool %q: inputSchema missing or not an object", name)
			continue
		}
		typ, _ := schema["type"].(string)
		if typ != "object" {
			t.Errorf("tool %q: inputSchema.type: got %q, want \"object\"", name, typ)
		}
	}
}

// ── get_service_status ────────────────────────────────────────────────────────

func TestGcloudMCPGetServiceStatusShape(t *testing.T) {
	resp := gcloudCall(t, "tools/call", map[string]any{
		"name": "get_service_status",
		"arguments": map[string]any{
			"service": "nexus-portal-dev",
			"project": "dea-noctua",
			"region":  "us-central1",
		},
	})

	// No top-level JSON-RPC error.
	if errVal, hasErr := resp["error"]; hasErr {
		t.Fatalf("tools/call returned JSON-RPC error: %v", errVal)
	}

	result := gcloudGetResult(t, resp)
	content := gcloudGetContent(t, result)
	text := gcloudContentText(t, content)

	if isToolError(result) {
		// No GCP credentials — expected in CI without ADC configured.
		t.Logf("get_service_status returned isError (no GCP credentials likely): %s", text)
		return
	}

	// On success: content[0].text is JSON with name, project, region fields.
	var summary map[string]any
	if err := json.Unmarshal([]byte(text), &summary); err != nil {
		t.Fatalf("content[0].text is not valid JSON: %v\ntext: %s", err, text)
	}
	for _, field := range []string{"name", "project", "region"} {
		if _, ok := summary[field]; !ok {
			t.Errorf("service summary missing field %q: %v", field, summary)
		}
	}
}

// ── query_logs ────────────────────────────────────────────────────────────────

func TestGcloudMCPQueryLogsShape(t *testing.T) {
	resp := gcloudCall(t, "tools/call", map[string]any{
		"name": "query_logs",
		"arguments": map[string]any{
			"project":  "dea-noctua",
			"service":  "nexus-portal-dev",
			"severity": "ERROR",
			"minutes":  60,
			"limit":    10,
		},
	})

	if errVal, hasErr := resp["error"]; hasErr {
		t.Fatalf("tools/call returned JSON-RPC error: %v", errVal)
	}

	result := gcloudGetResult(t, resp)
	content := gcloudGetContent(t, result)
	text := gcloudContentText(t, content)

	if isToolError(result) {
		t.Logf("query_logs returned isError (no GCP credentials likely): %s", text)
		return
	}

	// On success: text is valid JSON.
	var summary map[string]any
	if err := json.Unmarshal([]byte(text), &summary); err != nil {
		t.Fatalf("content[0].text is not valid JSON: %v\ntext: %s", err, text)
	}
}

// ── terraform_plan ────────────────────────────────────────────────────────────

func TestGcloudMCPTerraformPlanShape(t *testing.T) {
	resp := gcloudCall(t, "tools/call", map[string]any{
		"name": "terraform_plan",
		"arguments": map[string]any{
			"service": "portal",
			"env":     "dev",
		},
	})

	if errVal, hasErr := resp["error"]; hasErr {
		t.Fatalf("tools/call returned JSON-RPC error: %v", errVal)
	}

	result := gcloudGetResult(t, resp)
	content := gcloudGetContent(t, result)
	text := gcloudContentText(t, content)

	if isToolError(result) {
		// No nexus mount, no credentials, no terraform binary — all expected in CI.
		t.Logf("terraform_plan returned isError (no nexus mount or credentials likely): %s", text)
		return
	}

	// On success: text is valid JSON.
	var planResult map[string]any
	if err := json.Unmarshal([]byte(text), &planResult); err != nil {
		t.Fatalf("content[0].text is not valid JSON: %v\ntext: %s", err, text)
	}
}

// ── terraform_plan unknown service ───────────────────────────────────────────

func TestGcloudMCPTerraformPlanUnknownService(t *testing.T) {
	resp := gcloudCall(t, "tools/call", map[string]any{
		"name": "terraform_plan",
		"arguments": map[string]any{
			"service": "nonexistent-svc",
		},
	})

	if errVal, hasErr := resp["error"]; hasErr {
		t.Fatalf("tools/call returned JSON-RPC error: %v", errVal)
	}

	result := gcloudGetResult(t, resp)

	if !isToolError(result) {
		t.Errorf("expected isError=true for unknown service, got false; result: %v", result)
	}

	content := gcloudGetContent(t, result)
	text := gcloudContentText(t, content)

	if !strings.Contains(text, "unknown service") {
		t.Errorf("expected content to contain \"unknown service\", got: %s", text)
	}
}

// ── get_service_status missing required param ─────────────────────────────────

func TestGcloudMCPGetServiceStatusMissingService(t *testing.T) {
	resp := gcloudCall(t, "tools/call", map[string]any{
		"name":      "get_service_status",
		"arguments": map[string]any{}, // service omitted
	})

	if errVal, hasErr := resp["error"]; hasErr {
		t.Fatalf("tools/call returned JSON-RPC error: %v", errVal)
	}

	result := gcloudGetResult(t, resp)

	if !isToolError(result) {
		t.Errorf("expected isError=true for missing service param, got false; result: %v", result)
	}

	content := gcloudGetContent(t, result)
	text := gcloudContentText(t, content)

	if !strings.Contains(text, "missing required parameter") {
		t.Errorf("expected content to contain \"missing required parameter\", got: %s", text)
	}
}

// ── unknown method ────────────────────────────────────────────────────────────

func TestGcloudMCPUnknownMethod(t *testing.T) {
	resp := gcloudCall(t, "no_such_method", nil)

	errVal, hasErr := resp["error"]
	if !hasErr {
		t.Fatalf("expected JSON-RPC error for unknown method, got result: %v", resp)
	}

	errMap, ok := errVal.(map[string]any)
	if !ok {
		t.Fatalf("error field is not a map: %T %v", errVal, errVal)
	}

	// JSON numbers decode as float64.
	code, ok := errMap["code"].(float64)
	if !ok {
		t.Fatalf("error.code is not a number: %T %v", errMap["code"], errMap["code"])
	}
	if int(code) != -32601 {
		t.Errorf("error.code: got %d, want -32601 (MethodNotFound)", int(code))
	}
}

// ── ping ──────────────────────────────────────────────────────────────────────

func TestGcloudMCPPing(t *testing.T) {
	resp := gcloudCall(t, "ping", nil)

	if errVal, hasErr := resp["error"]; hasErr {
		t.Errorf("ping returned JSON-RPC error: %v", errVal)
	}

	if _, hasResult := resp["result"]; !hasResult {
		t.Errorf("ping response missing 'result' field: %v", resp)
	}
}

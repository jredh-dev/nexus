//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// githubMCPURL returns the base URL for the github-mcp server.
// Reads GITHUB_MCP_URL env var, defaulting to http://localhost:8091.
func githubMCPURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("GITHUB_MCP_URL")
	if u == "" {
		u = "http://localhost:8091"
	}
	return strings.TrimRight(u, "/")
}

// mcpClient returns an HTTP client with a 10-second timeout, matching
// the latency budget OpenCode uses for tool calls.
func mcpClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// mcpCall sends a JSON-RPC 2.0 request to baseURL+"/mcp" and returns the
// parsed response map. Fatal on any network or parse error — the protocol
// layer must be healthy for any test to make progress.
func mcpCall(t *testing.T, baseURL, method string, params any) map[string]any {
	t.Helper()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("mcpCall marshal: %v", err)
	}

	resp, err := mcpClient().Post(baseURL+"/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("mcpCall POST %s: %v", method, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("mcpCall read body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("mcpCall unmarshal (method=%s, body=%q): %v", method, string(raw), err)
	}

	return result
}

// mcpCallOrSkip is identical to mcpCall but treats a connection-refused error
// as t.Skip rather than t.Fatal. Use this for optional services (e.g. gcloud-mcp)
// that may not be running in all CI environments.
func mcpCallOrSkip(t *testing.T, baseURL, method string, params any) map[string]any {
	t.Helper()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("mcpCallOrSkip marshal: %v", err)
	}

	resp, err := mcpClient().Post(baseURL+"/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		// Any connection-level error (refused, timeout, DNS) skips rather than fails.
		if isConnRefused(err) {
			t.Skipf("skipping: %s not reachable (%v)", baseURL, err)
		}
		t.Fatalf("mcpCallOrSkip POST %s: %v", method, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("mcpCallOrSkip read body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("mcpCallOrSkip unmarshal (method=%s, body=%q): %v", method, string(raw), err)
	}

	return result
}

// isConnRefused returns true if err is (or wraps) a connection-refused or
// dial-level network error — the signatures we see when a server isn't running.
func isConnRefused(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "dial tcp") ||
		errors.Is(err, io.EOF)
}

// TestGithubMCPHealth verifies the /health endpoint returns 200 and
// identifies this server as github-mcp.
func TestGithubMCPHealth(t *testing.T) {
	base := githubMCPURL(t)

	resp, err := mcpClient().Get(base + "/health")
	if err != nil {
		t.Fatalf("health GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health: got status %d, want 200", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("health read body: %v", err)
	}

	if !strings.Contains(string(raw), "github-mcp") {
		t.Errorf("health body %q does not contain \"github-mcp\"", string(raw))
	}
}

// TestGithubMCPInitialize verifies the MCP initialize handshake returns
// the negotiated protocol version, correct server name, and a tools
// capability block — exactly as OpenCode expects on startup.
func TestGithubMCPInitialize(t *testing.T) {
	base := githubMCPURL(t)

	resp := mcpCall(t, base, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "opencode-test",
			"version": "1.0",
		},
	})

	// Protocol errors should not be present.
	if errField, ok := resp["error"]; ok {
		t.Fatalf("initialize: unexpected error: %v", errField)
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize: result is not an object, got %T: %v", resp["result"], resp["result"])
	}

	// The server must echo back the same protocol version.
	if got, _ := result["protocolVersion"].(string); got != "2025-03-26" {
		t.Errorf("initialize: protocolVersion = %q, want \"2025-03-26\"", got)
	}

	// serverInfo.name must be "github-mcp".
	serverInfo, _ := result["serverInfo"].(map[string]any)
	if serverInfo == nil {
		t.Fatalf("initialize: result.serverInfo missing")
	}
	if name, _ := serverInfo["name"].(string); name != "github-mcp" {
		t.Errorf("initialize: serverInfo.name = %q, want \"github-mcp\"", name)
	}

	// capabilities.tools must be present so OpenCode knows tools are available.
	caps, _ := result["capabilities"].(map[string]any)
	if caps == nil {
		t.Fatalf("initialize: result.capabilities missing")
	}
	if _, ok := caps["tools"]; !ok {
		t.Errorf("initialize: capabilities.tools not present")
	}
}

// TestGithubMCPToolsList verifies that tools/list returns a non-empty tool
// array containing the core tools OpenCode depends on.
func TestGithubMCPToolsList(t *testing.T) {
	base := githubMCPURL(t)

	resp := mcpCall(t, base, "tools/list", map[string]any{})

	if errField, ok := resp["error"]; ok {
		t.Fatalf("tools/list: unexpected error: %v", errField)
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list: result is not an object: %v", resp["result"])
	}

	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("tools/list: result.tools is empty or missing")
	}

	// Index names for easy lookup.
	names := make(map[string]bool, len(tools))
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		if n, _ := tool["name"].(string); n != "" {
			names[n] = true
		}
	}

	// These are the tools OpenCode uses; they must be present.
	required := []string{
		"pr_list", "pr_get", "pr_create", "pr_merge",
		"issue_list", "issue_create",
	}
	for _, name := range required {
		if !names[name] {
			t.Errorf("tools/list: missing required tool %q", name)
		}
	}
}

// TestGithubMCPToolsListShape iterates every tool returned by tools/list and
// verifies each has the minimum schema OpenCode requires to render its UI and
// invoke the tool: a non-empty name, a non-empty description, and an input
// schema with type "object".
func TestGithubMCPToolsListShape(t *testing.T) {
	base := githubMCPURL(t)

	resp := mcpCall(t, base, "tools/list", map[string]any{})

	if errField, ok := resp["error"]; ok {
		t.Fatalf("tools/list shape: unexpected error: %v", errField)
	}

	result, _ := resp["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("tools/list shape: no tools returned")
	}

	for i, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			t.Errorf("tool[%d]: not an object", i)
			continue
		}

		name, _ := tool["name"].(string)
		if name == "" {
			t.Errorf("tool[%d]: name is empty", i)
		}

		desc, _ := tool["description"].(string)
		if desc == "" {
			t.Errorf("tool[%d] (%s): description is empty", i, name)
		}

		schema, _ := tool["inputSchema"].(map[string]any)
		if schema == nil {
			t.Errorf("tool[%d] (%s): inputSchema missing", i, name)
			continue
		}
		if typ, _ := schema["type"].(string); typ != "object" {
			t.Errorf("tool[%d] (%s): inputSchema.type = %q, want \"object\"", i, name, typ)
		}
	}
}

// TestGithubMCPPRList calls the real pr_list tool against jredh-dev/nexus.
// We do not assert on the returned PR data (GITHUB_TOKEN may not be set in
// CI), but we do assert the tool returned a well-formed MCP content array
// with at least one text content block — i.e., the tool executed and the
// protocol shape is correct even if the underlying API call failed.
func TestGithubMCPPRList(t *testing.T) {
	base := githubMCPURL(t)

	resp := mcpCall(t, base, "tools/call", map[string]any{
		"name": "pr_list",
		"arguments": map[string]any{
			"repo":  "jredh-dev/nexus",
			"state": "open",
		},
	})

	// A top-level JSON-RPC error means the protocol layer failed — fatal.
	if errField, ok := resp["error"]; ok {
		t.Fatalf("tools/call pr_list: unexpected top-level error: %v", errField)
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call pr_list: result is not an object: %v", resp["result"])
	}

	// result.content must be a non-empty array.
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("tools/call pr_list: result.content missing or empty")
	}

	// The first content block must have type "text".
	// (isError may be true if GITHUB_TOKEN is absent — that's fine.)
	first, _ := content[0].(map[string]any)
	if first == nil {
		t.Fatalf("tools/call pr_list: content[0] is not an object")
	}
	if typ, _ := first["type"].(string); typ != "text" {
		t.Errorf("tools/call pr_list: content[0].type = %q, want \"text\"", typ)
	}
}

// TestGithubMCPUnknownMethod verifies that calling a method the server does
// not recognise returns JSON-RPC error code -32601 (Method Not Found), not a
// 500 or a silent empty response.
func TestGithubMCPUnknownMethod(t *testing.T) {
	base := githubMCPURL(t)

	resp := mcpCall(t, base, "unknown_method/xyz", nil)

	errField, ok := resp["error"]
	if !ok {
		t.Fatalf("unknown method: expected error field, got none; full response: %v", resp)
	}

	errObj, ok := errField.(map[string]any)
	if !ok {
		t.Fatalf("unknown method: error is not an object: %v", errField)
	}

	// JSON-RPC spec §5.1: -32601 = Method Not Found.
	code, _ := errObj["code"].(float64)
	if int(code) != -32601 {
		t.Errorf("unknown method: error.code = %d, want -32601", int(code))
	}
}

// TestGithubMCPPing verifies the MCP ping heartbeat works. OpenCode sends
// periodic pings to detect stale connections; the server must respond with
// an empty result object and no error.
func TestGithubMCPPing(t *testing.T) {
	base := githubMCPURL(t)

	resp := mcpCall(t, base, "ping", nil)

	if errField, ok := resp["error"]; ok {
		t.Fatalf("ping: unexpected error: %v", errField)
	}

	// result must be present (even if it is an empty object {}).
	if _, ok := resp["result"]; !ok {
		t.Errorf("ping: result field missing from response")
	}
}

// TestGithubMCPNotification verifies the server handles MCP notifications
// (messages with no "id" field) by returning HTTP 202 Accepted with no body.
// OpenCode sends "initialized" after the handshake; the server must not
// block waiting to reply.
func TestGithubMCPNotification(t *testing.T) {
	base := githubMCPURL(t)

	notification := map[string]any{
		"jsonrpc": "2.0",
		// Deliberately no "id" field — this is a notification, not a request.
		"method": "notifications/initialized",
		"params": map[string]any{},
	}

	body, err := json.Marshal(notification)
	if err != nil {
		t.Fatalf("notification marshal: %v", err)
	}

	resp, err := mcpClient().Post(base+"/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("notification POST: %v", err)
	}
	defer resp.Body.Close()

	// MCP spec: notifications must not receive a response — HTTP 202 signals
	// "received and accepted, nothing to return".
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("notification: got status %d, want 202", resp.StatusCode)
	}
}

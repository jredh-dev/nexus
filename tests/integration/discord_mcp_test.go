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

// discordMCPURL returns the base URL for the discord-mcp server.
// Reads DISCORD_MCP_URL env; defaults to http://localhost:8092.
func discordMCPURL(t *testing.T) string {
	t.Helper()
	if u := os.Getenv("DISCORD_MCP_URL"); u != "" {
		return u
	}
	return "http://localhost:8092"
}

// discordCall sends an MCP JSON-RPC request to the discord-mcp server and
// returns the decoded response map. It delegates to the shared mcpCall helper
// declared in github_mcp_test.go, supplying the discord-mcp base URL.
func discordCall(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	return mcpCall(t, discordMCPURL(t), method, params)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestDiscordMCPHealth checks that the health endpoint returns 200 and
// identifies the service by name.
func TestDiscordMCPHealth(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(discordMCPURL(t) + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health check: got status %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read health body: %v", err)
	}

	if !strings.Contains(string(body), "discord-mcp") {
		t.Errorf("health body does not contain %q: %s", "discord-mcp", body)
	}
}

// TestDiscordMCPInitialize validates the MCP initialize handshake.
func TestDiscordMCPInitialize(t *testing.T) {
	result := discordCall(t, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "opencode-test",
			"version": "1.0",
		},
	})

	if _, hasErr := result["error"]; hasErr {
		t.Fatalf("initialize returned error: %v", result["error"])
	}

	res, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("result field missing or wrong type: %v", result["result"])
	}

	// protocolVersion
	if pv, _ := res["protocolVersion"].(string); pv != "2025-03-26" {
		t.Errorf("protocolVersion: got %q, want %q", pv, "2025-03-26")
	}

	// serverInfo.name
	serverInfo, _ := res["serverInfo"].(map[string]any)
	if serverInfo == nil {
		t.Fatalf("serverInfo missing from result")
	}
	if name, _ := serverInfo["name"].(string); name != "discord-mcp" {
		t.Errorf("serverInfo.name: got %q, want %q", name, "discord-mcp")
	}

	// capabilities.tools must be present
	caps, _ := res["capabilities"].(map[string]any)
	if caps == nil {
		t.Fatalf("capabilities missing from result")
	}
	if _, hasTools := caps["tools"]; !hasTools {
		t.Errorf("capabilities.tools not present in: %v", caps)
	}
}

// TestDiscordMCPToolsList verifies that tools/list returns at least one tool
// and that notify_discord is among them.
func TestDiscordMCPToolsList(t *testing.T) {
	result := discordCall(t, "tools/list", map[string]any{})

	if _, hasErr := result["error"]; hasErr {
		t.Fatalf("tools/list returned error: %v", result["error"])
	}

	res, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("result field missing or wrong type: %v", result["result"])
	}

	rawTools, ok := res["tools"]
	if !ok {
		t.Fatalf("tools field missing from result")
	}

	tools, ok := rawTools.([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("tools is empty or wrong type: %v", rawTools)
	}

	// Find notify_discord
	found := false
	for _, entry := range tools {
		tool, _ := entry.(map[string]any)
		if tool == nil {
			continue
		}
		if name, _ := tool["name"].(string); name == "notify_discord" {
			found = true
			break
		}
	}
	if !found {
		// Build a list of names for a helpful error message.
		names := make([]string, 0, len(tools))
		for _, entry := range tools {
			if tool, _ := entry.(map[string]any); tool != nil {
				if n, _ := tool["name"].(string); n != "" {
					names = append(names, n)
				}
			}
		}
		t.Errorf("notify_discord not in tools list; found: %v", names)
	}
}

// TestDiscordMCPNotifyDiscordShape calls notify_discord with valid arguments
// and asserts the MCP response has the correct shape, regardless of whether
// the webhook is actually configured.
func TestDiscordMCPNotifyDiscordShape(t *testing.T) {
	result := discordCall(t, "tools/call", map[string]any{
		"name": "notify_discord",
		"arguments": map[string]any{
			"event":   "test",
			"service": "integration-test",
			"status":  "success",
			"message": "integration test probe",
		},
	})

	// Top-level JSON-RPC error is an infrastructure failure.
	if _, hasErr := result["error"]; hasErr {
		t.Fatalf("tools/call returned top-level JSON-RPC error: %v", result["error"])
	}

	res, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("result field missing or wrong type: %v", result["result"])
	}

	// content must be present and non-empty.
	rawContent, ok := res["content"]
	if !ok {
		t.Fatalf("result.content missing: %v", res)
	}
	content, ok := rawContent.([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("result.content is empty or wrong type: %v", rawContent)
	}

	// content[0].type must be "text".
	first, _ := content[0].(map[string]any)
	if first == nil {
		t.Fatalf("content[0] is not an object: %v", content[0])
	}
	if typ, _ := first["type"].(string); typ != "text" {
		t.Errorf("content[0].type: got %q, want %q", typ, "text")
	}

	// isError == true is acceptable when webhook isn't configured; just log it.
	if isErr, _ := res["isError"].(bool); isErr {
		t.Logf("notify_discord returned isError=true (webhook likely not configured): %v", first["text"])
	}
}

// TestDiscordMCPNotifyDiscordMissingRequired calls notify_discord with no
// arguments and expects a well-formed error response (isError or error text).
func TestDiscordMCPNotifyDiscordMissingRequired(t *testing.T) {
	result := discordCall(t, "tools/call", map[string]any{
		"name":      "notify_discord",
		"arguments": map[string]any{},
	})

	// Top-level JSON-RPC error is also acceptable for missing params.
	if rpcErr, hasErr := result["error"]; hasErr {
		t.Logf("got top-level JSON-RPC error for missing params (acceptable): %v", rpcErr)
		return
	}

	res, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("result field missing or wrong type: %v", result["result"])
	}

	// Either isError==true or content[0].text contains an error description.
	isErr, _ := res["isError"].(bool)
	rawContent, _ := res["content"].([]any)
	if len(rawContent) == 0 {
		if !isErr {
			t.Errorf("expected isError=true or non-empty content for missing required fields; got: %v", res)
		}
		return
	}

	first, _ := rawContent[0].(map[string]any)
	text, _ := first["text"].(string)

	if !isErr && text == "" {
		t.Errorf("expected error indication for missing required fields; got result: %v", res)
	}

	if text != "" {
		t.Logf("error message for missing params: %s", text)
	}
}

// TestDiscordMCPUnknownTool calls a tool name that does not exist and expects
// isError==true with "unknown tool" in the error text.
func TestDiscordMCPUnknownTool(t *testing.T) {
	result := discordCall(t, "tools/call", map[string]any{
		"name":      "nonexistent_tool",
		"arguments": map[string]any{},
	})

	// Accept either a top-level JSON-RPC error or a result with isError==true.
	if rpcErr, hasErr := result["error"]; hasErr {
		msg := ""
		if errMap, ok := rpcErr.(map[string]any); ok {
			msg, _ = errMap["message"].(string)
		} else {
			raw, _ := json.Marshal(rpcErr)
			msg = string(raw)
		}
		if !strings.Contains(strings.ToLower(msg), "unknown tool") {
			t.Errorf("error message does not contain %q: %s", "unknown tool", msg)
		}
		return
	}

	res, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("result field missing or wrong type: %v", result["result"])
	}

	isErr, _ := res["isError"].(bool)
	if !isErr {
		t.Errorf("expected isError=true for unknown tool; got: %v", res)
	}

	rawContent, _ := res["content"].([]any)
	if len(rawContent) == 0 {
		t.Fatalf("content missing for unknown tool error response: %v", res)
	}

	first, _ := rawContent[0].(map[string]any)
	text, _ := first["text"].(string)
	if !strings.Contains(strings.ToLower(text), "unknown tool") {
		t.Errorf("content[0].text does not contain %q: %s", "unknown tool", text)
	}
}

// TestDiscordMCPPing sends a ping request and expects a clean response with no
// error.
func TestDiscordMCPPing(t *testing.T) {
	result := discordCall(t, "ping", map[string]any{})

	if _, hasErr := result["error"]; hasErr {
		t.Errorf("ping returned error: %v", result["error"])
	}
}

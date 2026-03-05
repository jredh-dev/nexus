//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func secretsURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("SECRETS_URL")
	if u == "" {
		u = "http://localhost:8082"
	}
	return strings.TrimRight(u, "/")
}

func secretsClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

func TestSecretsHealth(t *testing.T) {
	base := secretsURL(t)
	resp, err := secretsClient().Get(base + "/health")
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health: got %d, want 200", resp.StatusCode)
	}
}

func TestSecretsSubmitAndList(t *testing.T) {
	base := secretsURL(t)
	client := secretsClient()

	// Submit
	body := `{"value":"integration test secret","submitted_by":"test-harness"}`
	resp, err := client.Post(base+"/api/secrets", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Errorf("submit: got %d", resp.StatusCode)
	}

	// List
	resp2, err := client.Get(base + "/api/secrets")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer resp2.Body.Close()

	var secrets []map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&secrets); err != nil {
		t.Fatalf("list decode: %v", err)
	}
	if len(secrets) == 0 {
		t.Error("list: no secrets returned after submit")
	}
}

func TestSecretsStats(t *testing.T) {
	base := secretsURL(t)
	resp, err := secretsClient().Get(base + "/api/stats")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	defer resp.Body.Close()

	var stats map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("stats decode: %v", err)
	}
	// Verify expected fields exist (matches store.Stats JSON tags).
	for _, field := range []string{"total", "secrets", "not_secrets", "lenses"} {
		if _, ok := stats[field]; !ok {
			t.Errorf("stats: missing field %q", field)
		}
	}
}

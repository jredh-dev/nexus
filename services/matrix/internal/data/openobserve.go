// Package data provides data-fetching helpers for the matrix dashboard.
// This file implements OpenObserve log count queries.
package data

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// ooSearchRequest is the request body for the OpenObserve search API.
// POST /api/default/_search?type=logs
type ooSearchRequest struct {
	Query ooQuery `json:"query"`
}

type ooQuery struct {
	SQL       string `json:"sql"`
	From      int    `json:"from"`
	Size      int    `json:"size"`
	StartTime int64  `json:"start_time"` // microseconds epoch
	EndTime   int64  `json:"end_time"`   // microseconds epoch
}

// ooSearchResponse is the partial response from the OpenObserve search API.
type ooSearchResponse struct {
	Hits []map[string]any `json:"hits"`
}

// FetchLogCounts queries OpenObserve for the number of log lines per service
// in the last 5 minutes. The map key is the service name (e.g. "hermit"),
// matching the `service` field that Vector writes after stripping the
// "agentic-" prefix from container names.
//
// Returns an empty map (never nil) on error so callers can always do map[key].
func FetchLogCounts(ctx context.Context, baseURL, user, pass string) map[string]int {
	counts := map[string]int{}

	// 5-minute window in microseconds.
	now := time.Now()
	startUs := now.Add(-5 * time.Minute).UnixMicro()
	endUs := now.UnixMicro()

	// GROUP BY service, count per service, up to 100 distinct services.
	sql := `SELECT service, COUNT(*) as cnt FROM "agentic_logs" GROUP BY service`
	body, err := json.Marshal(ooSearchRequest{
		Query: ooQuery{
			SQL:       sql,
			From:      0,
			Size:      100,
			StartTime: startUs,
			EndTime:   endUs,
		},
	})
	if err != nil {
		log.Printf("openobserve: marshal: %v", err)
		return counts
	}

	url := fmt.Sprintf("%s/api/default/_search?type=logs", baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("openobserve: build request: %v", err)
		return counts
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(user, pass)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("openobserve: request: %v", err)
		return counts
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		log.Printf("openobserve: status %d: %s", resp.StatusCode, b)
		return counts
	}

	var result ooSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("openobserve: decode: %v", err)
		return counts
	}

	// Each hit is one row: {"service": "hermit", "cnt": 42}
	for _, hit := range result.Hits {
		svc, ok := hit["service"].(string)
		if !ok || svc == "" {
			continue
		}
		// cnt may come back as float64 from JSON unmarshalling.
		switch v := hit["cnt"].(type) {
		case float64:
			counts[svc] = int(v)
		case int:
			counts[svc] = v
		}
	}

	return counts
}

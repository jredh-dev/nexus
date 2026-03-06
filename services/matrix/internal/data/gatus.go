// Package data provides fetchers for Gatus, GitHub Actions, and Gitea Actions.
package data

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// FetchGatus fetches all endpoint statuses from the local Gatus instance.
// Returns a map keyed by Gatus endpoint key.
func FetchGatus(ctx context.Context, baseURL string) (map[string]GatusResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/endpoints/statuses", nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gatus returned %d", resp.StatusCode)
	}

	// Gatus response structure
	var raw []struct {
		Name    string `json:"name"`
		Group   string `json:"group"`
		Key     string `json:"key"`
		Results []struct {
			Success bool `json:"success"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make(map[string]GatusResult, len(raw))
	for _, ep := range raw {
		status := StatusUnknown
		if len(ep.Results) > 0 {
			if ep.Results[len(ep.Results)-1].Success {
				status = StatusUp
			} else {
				status = StatusDown
			}
		}
		out[ep.Key] = GatusResult{
			Name:   ep.Name,
			Group:  ep.Group,
			Key:    ep.Key,
			Status: status,
		}
	}
	return out, nil
}

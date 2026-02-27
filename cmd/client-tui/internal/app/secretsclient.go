// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// SecretsClient is the interface for the secrets REST API.
// The real implementation talks to nexus/services/go-http over HTTP.
// Tests inject a mock.
type SecretsClient interface {
	List() ([]Secret, error)
	Submit(value, submittedBy string) (*SubmitResult, error)
	Stats() (SecretsStats, error)
}

// --- Domain types (mirrors nexus/services/go-http/internal/store) ---

type Secret struct {
	ID          string     `json:"id"`
	Value       string     `json:"value"`
	SubmittedBy string     `json:"submitted_by"`
	State       string     `json:"state"` // "truth" | "lie"
	ExposedBy   string     `json:"exposed_by,omitempty"`
	ExposedVia  string     `json:"exposed_via,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ExposedAt   *time.Time `json:"exposed_at,omitempty"`
}

type SecretsStats struct {
	Total  int `json:"total"`
	Truths int `json:"truths"`
	Lies   int `json:"lies"`
	Lenses int `json:"lenses"`
}

type SubmitResult struct {
	Secret       *Secret `json:"secret"`
	WasNew       bool    `json:"was_new"`
	SelfBetrayal bool    `json:"self_betrayal,omitempty"`
	Message      string  `json:"message"`
}

// --- HTTP implementation ---

type httpSecretsClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewSecretsClient(baseURL string) SecretsClient {
	return &httpSecretsClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *httpSecretsClient) List() ([]Secret, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/secrets", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("secrets list: %w", err)
	}
	defer resp.Body.Close()

	var secrets []Secret
	if err := json.NewDecoder(resp.Body).Decode(&secrets); err != nil {
		return nil, fmt.Errorf("secrets list decode: %w", err)
	}
	return secrets, nil
}

func (c *httpSecretsClient) Submit(value, submittedBy string) (*SubmitResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	body := fmt.Sprintf(`{"value":%q,"submitted_by":%q}`, value, submittedBy)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/secrets",
		strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("secrets submit: %w", err)
	}
	defer resp.Body.Close()

	var result SubmitResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("secrets submit decode: %w", err)
	}
	return &result, nil
}

func (c *httpSecretsClient) Stats() (SecretsStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/stats", nil)
	if err != nil {
		return SecretsStats{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return SecretsStats{}, fmt.Errorf("secrets stats: %w", err)
	}
	defer resp.Body.Close()

	var stats SecretsStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return SecretsStats{}, fmt.Errorf("secrets stats decode: %w", err)
	}
	return stats, nil
}

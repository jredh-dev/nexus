// Copyright (C) 2026 jredh-dev. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// This file is part of nexus.
//
// nexus is free software: you can redistribute it and/or modify it under
// the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version.

// docker.go manages Docker container lifecycle for integration testing.
// When the --docker flag is passed to `servicectl test`, these functions
// handle starting containers via docker compose, polling health endpoints
// for readiness, and tearing down containers after tests complete.
package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// defaultComposeFile is the path to the agentic workspace's
// docker-compose.yml which defines all nexus services.
const defaultComposeFile = "/Users/jredh/Development/agentic/docker-compose.yml"

// DockerConfig holds settings for container lifecycle management.
// Populated from the service registry and any CLI overrides.
type DockerConfig struct {
	// ComposeFile is the path to the docker-compose.yml file.
	ComposeFile string

	// ServiceName is the docker-compose service name (e.g., "vn").
	ServiceName string

	// HealthEndpoint is the HTTP URL to poll for readiness.
	// Empty means skip health polling (e.g., gRPC services).
	HealthEndpoint string

	// Timeout is how long to wait for the health endpoint to return 200.
	Timeout time.Duration
}

// DockerUp starts the Docker containers for the service using
// `docker compose up -d`. The -d flag runs containers in detached
// mode so we get control back to run tests.
func DockerUp(cfg DockerConfig) error {
	fmt.Printf("  docker: starting %s...\n", cfg.ServiceName)

	cmd := exec.Command("docker", "compose",
		"-f", cfg.ComposeFile,
		"up", "-d", cfg.ServiceName,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose up failed: %w", err)
	}

	fmt.Printf("  docker: %s started\n", cfg.ServiceName)
	return nil
}

// DockerDown stops and removes the Docker containers for the service.
// Called in a defer so cleanup happens even if tests fail or panic.
func DockerDown(cfg DockerConfig) error {
	fmt.Printf("  docker: stopping %s...\n", cfg.ServiceName)

	cmd := exec.Command("docker", "compose",
		"-f", cfg.ComposeFile,
		"down", cfg.ServiceName,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose down failed: %w", err)
	}

	fmt.Printf("  docker: %s stopped\n", cfg.ServiceName)
	return nil
}

// WaitForHealth polls the health endpoint every 2 seconds until it
// returns HTTP 200 or the timeout expires. This ensures the service
// is fully initialized before we run tests against it.
//
// If endpoint is empty, this is a no-op (some services like hermit
// use gRPC and don't expose an HTTP health endpoint).
func WaitForHealth(endpoint string, timeout time.Duration) error {
	if endpoint == "" {
		return nil
	}

	fmt.Printf("  docker: waiting for health at %s (timeout %s)...\n", endpoint, timeout)

	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(endpoint)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Printf("  docker: health check passed\n")
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("health check timed out after %s polling %s", timeout, endpoint)
}

// checkDockerAvailable verifies that the docker CLI is installed and
// accessible. Returns an error with a helpful message if not found.
func checkDockerAvailable() error {
	_, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found in PATH — install Docker Desktop or Docker Engine to use --docker")
	}
	return nil
}

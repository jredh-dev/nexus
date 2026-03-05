// Copyright (C) 2026 jredh-dev. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// This file is part of nexus.
//
// nexus is free software: you can redistribute it and/or modify it under
// the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version.

// registry.go defines the service registry — the central map of nexus
// services, their integration test patterns, environment variables, and
// default connection endpoints.
//
// Adding a new service:
//  1. Add an entry to the services map below.
//  2. Ensure tests in tests/integration/ follow the naming convention
//     (e.g., TestFoo* for service "foo").
//  3. The test pattern is a Go test -run regex. Use "|" to combine
//     multiple distinct prefixes if needed.
package main

// EnvVar represents an environment variable that a service's integration
// tests expect. DefaultValue is used when the user doesn't override it.
// Description is shown in the list command for discoverability.
type EnvVar struct {
	Name         string
	DefaultValue string
	Description  string
}

// Service describes a nexus service and how to run its integration tests.
type Service struct {
	// Name is the short identifier used on the command line (e.g., "vn").
	Name string

	// Description is a human-readable one-liner shown in `servicectl list`.
	Description string

	// TestPattern is the -run regex passed to `go test`. It should match
	// all test function names for this service. For example, "^TestVN"
	// matches TestVNHealth, TestVNGetStory, etc.
	TestPattern string

	// TestCount is the number of test functions matching TestPattern.
	// This is a static count used for display purposes in `servicectl list`.
	// It doesn't affect test execution — `go test -run` handles discovery.
	TestCount int

	// EnvVars lists the environment variables this service's tests read.
	// The runner sets these before exec'ing `go test`.
	EnvVars []EnvVar

	// URLEnvVar is the name of the primary URL/address env var. This is
	// the one that --url / --addr overrides. For HTTP services this is
	// typically something like VN_URL; for gRPC it's HERMIT_ADDR.
	URLEnvVar string
}

// services is the canonical registry of all nexus services that have
// integration tests. Keyed by the CLI-facing service name.
//
// The test patterns are derived from the naming convention in
// tests/integration/*_test.go:
//   - hermit: TestPing, TestServerInfo, TestBenchmark, TestKv*, TestSql*, TestDbStats
//   - secrets: TestSecrets*
//   - vn: TestVN*
var services = map[string]Service{
	"hermit": {
		Name:        "hermit",
		Description: "Rust gRPC server (key-value, SQL, benchmarks)",
		// Hermit tests don't share a common prefix — they use bare names
		// like TestPing, TestServerInfo, etc. We enumerate them explicitly.
		TestPattern: "^(TestPing|TestServerInfo|TestBenchmark|TestKv|TestSql|TestDbStats)",
		TestCount:   6,
		EnvVars: []EnvVar{
			{Name: "HERMIT_ADDR", DefaultValue: "localhost:9090", Description: "gRPC address"},
			{Name: "HERMIT_SECRET", DefaultValue: "", Description: "shared secret (x-hermit-secret header)"},
			{Name: "HERMIT_INSECURE", DefaultValue: "true", Description: "disable TLS (true for local dev)"},
			{Name: "HERMIT_BEARER_TOKEN", DefaultValue: "", Description: "IAM bearer token (Cloud Run auth)"},
		},
		URLEnvVar: "HERMIT_ADDR",
	},

	"secrets": {
		Name:        "secrets",
		Description: "Confession/secrets HTTP service",
		TestPattern: "^TestSecrets",
		TestCount:   3,
		EnvVars: []EnvVar{
			{Name: "SECRETS_URL", DefaultValue: "http://localhost:8082", Description: "HTTP base URL"},
		},
		URLEnvVar: "SECRETS_URL",
	},

	"vn": {
		Name:        "vn",
		Description: "Visual novel engine (story, chapters, voting)",
		TestPattern: "^TestVN",
		TestCount:   10,
		EnvVars: []EnvVar{
			{Name: "VN_URL", DefaultValue: "http://localhost:8082", Description: "HTTP base URL"},
		},
		URLEnvVar: "VN_URL",
	},
}

// serviceNames returns the service names in a stable, alphabetical order.
// Used for consistent output in list and help commands.
func serviceNames() []string {
	// Hardcoded rather than sorting the map each time. Update when
	// adding services. Three entries don't justify pulling in sort.
	return []string{"hermit", "secrets", "vn"}
}

// Copyright (C) 2026 jredh-dev. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// This file is part of nexus.
//
// nexus is free software: you can redistribute it and/or modify it under
// the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version.

// servicectl is a per-service integration test runner for the nexus
// monorepo. It wraps `go test -tags integration` with service-aware
// defaults for environment variables, test patterns, and connection
// endpoints.
//
// Usage:
//
//	servicectl test <service>   Run integration tests for a service
//	servicectl test all         Run integration tests for all services
//	servicectl list             List available services and test counts
//
// Examples:
//
//	servicectl test vn                          # test vn against localhost:8082
//	servicectl test vn --url http://staging:80  # test against staging
//	servicectl test hermit -v --timeout 60s     # verbose hermit tests, 60s timeout
//	servicectl test all --dry-run               # show commands without running
//	servicectl list                             # show available services
//
// Each service has a registry entry (see registry.go) that defines its
// test pattern, environment variables, and defaults. The runner (runner.go)
// handles subprocess execution.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	// Top-level usage. We don't use flag.Parse() at the top level because
	// subcommands have their own flag sets.
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "test":
		os.Exit(cmdTest(os.Args[2:]))
	case "list":
		cmdList()
	case "help", "--help", "-h", "-?":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "servicectl: unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

// cmdTest parses flags for the "test" subcommand and runs integration
// tests for the specified service(s).
//
// The flag set is attached to the "test" subcommand, so flags like
// --url and --verbose don't pollute the top-level namespace.
func cmdTest(args []string) int {
	fs := flag.NewFlagSet("test", flag.ExitOnError)

	// --url and --addr are aliases. Both override the service's primary
	// connection endpoint. We use two flags pointing at the same variable
	// because HTTP services use "URL" and gRPC uses "addr" — the user
	// shouldn't have to remember which one a service uses.
	var url string
	fs.StringVar(&url, "url", "", "override service URL/address")
	fs.StringVar(&url, "addr", "", "override service URL/address (alias for --url)")

	verbose := fs.Bool("verbose", false, "pass -v to go test for verbose output")
	fs.BoolVar(verbose, "v", false, "pass -v to go test (shorthand for --verbose)")

	timeout := fs.String("timeout", "30s", "test timeout (passed to go test -timeout)")
	dryRun := fs.Bool("dry-run", false, "print the go test command without running it")
	docker := fs.Bool("docker", false, "start/stop Docker containers for the service")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: servicectl test <service> [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Services: %s, all\n\n", strings.Join(serviceNames(), ", "))
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	// The first positional arg after "test" is the service name.
	// Everything after that is flags.
	if len(args) == 0 {
		fs.Usage()
		return 1
	}

	// Extract service name before parsing flags. The service name is
	// always the first positional argument. Handle --help/-h here so
	// "servicectl test --help" shows flag help instead of "unknown service".
	serviceName := args[0]
	if serviceName == "--help" || serviceName == "-h" || serviceName == "-?" || serviceName == "help" {
		fs.Usage()
		return 0
	}
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}

	// Handle "test all" — run every registered service sequentially.
	// We track the worst exit code so the overall result reflects any
	// failure, but we don't stop on the first failure because you want
	// to see results for all services.
	if serviceName == "all" {
		return runAll(url, *verbose, *timeout, *dryRun, *docker)
	}

	// Look up the service in the registry.
	svc, ok := services[serviceName]
	if !ok {
		fmt.Fprintf(os.Stderr, "servicectl: unknown service %q\n", serviceName)
		fmt.Fprintf(os.Stderr, "available: %s\n", strings.Join(serviceNames(), ", "))
		return 1
	}

	// When --docker is set, manage the container lifecycle around the
	// test run: start containers, wait for health, run tests, tear down.
	if *docker {
		return runWithDocker(svc, RunConfig{
			Service: svc,
			URL:     url,
			Verbose: *verbose,
			Timeout: *timeout,
			DryRun:  *dryRun,
		})
	}

	return Run(RunConfig{
		Service: svc,
		URL:     url,
		Verbose: *verbose,
		Timeout: *timeout,
		DryRun:  *dryRun,
	})
}

// runWithDocker wraps a test run with Docker container lifecycle:
// start containers, poll health, run tests, tear down.
func runWithDocker(svc Service, cfg RunConfig) int {
	// Verify docker is available before attempting anything.
	if err := checkDockerAvailable(); err != nil {
		fmt.Fprintf(os.Stderr, "servicectl: %v\n", err)
		return 1
	}

	// Ensure the service supports Docker mode.
	if svc.DockerService == "" {
		fmt.Fprintf(os.Stderr, "servicectl: service %q does not support --docker (no DockerService defined)\n", svc.Name)
		return 1
	}

	// Resolve the compose file path. Service-specific override wins,
	// otherwise fall back to the default agentic workspace compose file.
	composeFile := svc.ComposeFile
	if composeFile == "" {
		composeFile = defaultComposeFile
	}

	dockerCfg := DockerConfig{
		ComposeFile:    composeFile,
		ServiceName:    svc.DockerService,
		HealthEndpoint: svc.HealthEndpoint,
		Timeout:        30 * time.Second,
	}

	// Start containers.
	if err := DockerUp(dockerCfg); err != nil {
		fmt.Fprintf(os.Stderr, "servicectl: %v\n", err)
		return 1
	}

	// Ensure teardown happens even if tests panic or fail.
	defer func() {
		if err := DockerDown(dockerCfg); err != nil {
			fmt.Fprintf(os.Stderr, "servicectl: warning: %v\n", err)
		}
	}()

	// Wait for the service to be ready before running tests.
	if err := WaitForHealth(dockerCfg.HealthEndpoint, dockerCfg.Timeout); err != nil {
		fmt.Fprintf(os.Stderr, "servicectl: %v\n", err)
		return 1
	}

	return Run(cfg)
}

// runAll runs integration tests for every registered service in
// alphabetical order. Returns the highest exit code seen.
func runAll(url string, verbose bool, timeout string, dryRun bool, docker bool) int {
	worst := 0
	for _, name := range serviceNames() {
		svc := services[name]

		// When running all services, the --url flag only makes sense if
		// there's a single service. For "all", we ignore it and use
		// defaults (or whatever's in the environment).
		// However, if someone explicitly passes --url with "all", warn
		// them rather than silently ignoring it.
		effectiveURL := url
		if url != "" {
			fmt.Fprintf(os.Stderr, "warning: --url applies to service %q only (ignored for others in 'all')\n", name)
			// Only apply to the first service, then clear.
			// Actually, this is confusing. Just skip it entirely.
			effectiveURL = ""
		}

		rcfg := RunConfig{
			Service: svc,
			URL:     effectiveURL,
			Verbose: verbose,
			Timeout: timeout,
			DryRun:  dryRun,
		}

		var code int
		if docker {
			code = runWithDocker(svc, rcfg)
		} else {
			code = Run(rcfg)
		}

		if code > worst {
			worst = code
		}

		// Visual separator between services (skip after the last one,
		// and skip for dry-run since the output is already compact).
		if !dryRun {
			fmt.Println()
		}
	}
	return worst
}

// cmdList prints a table of registered services, their descriptions, and
// test counts. Useful for discoverability.
func cmdList() {
	fmt.Println("Available services:")
	fmt.Println()

	// Simple aligned output. We know service names are short, so a
	// fixed-width format works fine without measuring column widths.
	fmt.Printf("  %-10s %-50s %s\n", "SERVICE", "DESCRIPTION", "TESTS")
	fmt.Printf("  %-10s %-50s %s\n", "-------", "-----------", "-----")

	for _, name := range serviceNames() {
		svc := services[name]
		fmt.Printf("  %-10s %-50s %d\n", svc.Name, svc.Description, svc.TestCount)
	}

	fmt.Println()
	fmt.Println("Run tests:  servicectl test <service>")
	fmt.Println("Run all:    servicectl test all")
}

// printUsage prints the top-level help text.
func printUsage() {
	fmt.Fprintf(os.Stderr, `servicectl — per-service integration test runner for nexus

Usage:
  servicectl test <service> [flags]    Run integration tests
  servicectl test all [flags]          Run all integration tests
  servicectl list                      List available services

Run 'servicectl test --help' for test flags.
`)
}

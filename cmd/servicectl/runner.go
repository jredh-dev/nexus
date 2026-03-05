// Copyright (C) 2026 jredh-dev. All rights reserved.
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// This file is part of nexus.
//
// nexus is free software: you can redistribute it and/or modify it under
// the terms of the GNU Affero General Public License as published by the
// Free Software Foundation, either version 3 of the License, or (at your
// option) any later version.

// runner.go handles the actual execution of integration tests. It builds
// the `go test` command with the right flags, sets environment variables,
// streams output to stdout/stderr, and propagates the exit code.
//
// Design decisions:
//   - We exec `go test` as a subprocess rather than importing the test
//     framework because integration tests need the -tags build constraint
//     and their own process isolation.
//   - Environment variables are set on the child process only — we never
//     mutate the parent's environment.
//   - The runner finds the repo root by walking up from the executable's
//     working directory looking for go.mod. This means servicectl works
//     from any directory within the nexus repo.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RunConfig holds everything needed to execute a test run for one service.
type RunConfig struct {
	Service Service
	URL     string // Override for the service's primary URL/address env var.
	Verbose bool   // Pass -v to go test.
	Timeout string // Timeout string for go test -timeout (e.g., "30s").
	DryRun  bool   // Print the command instead of running it.
}

// Run executes integration tests for a single service. It returns the
// exit code from `go test` (0 on success, non-zero on failure).
//
// The function:
//  1. Builds the environment by merging defaults with any user overrides.
//  2. Constructs the `go test` command with appropriate flags.
//  3. Either prints the command (dry-run) or executes it.
func Run(cfg RunConfig) int {
	// Build the environment for the child process. Start with the
	// current process environment so things like PATH, GOPATH, HOME
	// are inherited. Then layer on service-specific defaults, and
	// finally apply any user overrides.
	env := buildEnv(cfg)

	// Construct the go test arguments.
	args := buildArgs(cfg)

	if cfg.DryRun {
		printDryRun(cfg, env, args)
		return 0
	}

	// Print a header so the user knows what's running.
	printHeader(cfg, env)

	return execute(args, env)
}

// buildEnv constructs the environment variable slice for the child process.
// Order of precedence (highest wins):
//  1. Explicit --url/--addr flag (overrides the service's URLEnvVar)
//  2. Already-set environment variables from the parent process
//  3. Service defaults from the registry
//
// This means if someone already exported VN_URL=http://staging:8080,
// we respect that unless they also pass --url.
func buildEnv(cfg RunConfig) []string {
	// Start with the full parent environment.
	env := os.Environ()

	// Apply service defaults, but only for variables not already set.
	// This lets users export variables in their shell and have them
	// take precedence over defaults.
	for _, ev := range cfg.Service.EnvVars {
		if ev.DefaultValue == "" {
			continue // Don't set empty defaults — they'd shadow intentional unsets.
		}
		if _, exists := os.LookupEnv(ev.Name); !exists {
			env = append(env, ev.Name+"="+ev.DefaultValue)
		}
	}

	// The --url/--addr flag always wins. If provided, override the
	// service's primary URL env var regardless of what's in the environment.
	if cfg.URL != "" {
		// Remove any existing entry for this var to avoid duplicates.
		// os.Environ() returns KEY=VALUE pairs; the last one wins in most
		// implementations, but removing is cleaner.
		target := cfg.Service.URLEnvVar + "="
		filtered := make([]string, 0, len(env))
		for _, e := range env {
			if !strings.HasPrefix(e, target) {
				filtered = append(filtered, e)
			}
		}
		env = append(filtered, cfg.Service.URLEnvVar+"="+cfg.URL)
	}

	return env
}

// buildArgs constructs the argument list for `go test`.
//
// The resulting command looks like:
//   go test -tags integration -run "^TestVN" -timeout 30s [-v] ./tests/integration/...
func buildArgs(cfg RunConfig) []string {
	args := []string{
		"test",
		"-tags", "integration",
		"-run", cfg.Service.TestPattern,
		"-timeout", cfg.Timeout,
	}

	if cfg.Verbose {
		args = append(args, "-v")
	}

	// The test package path. All integration tests live under this
	// directory regardless of service.
	args = append(args, "./tests/integration/...")

	return args
}

// printHeader outputs a summary of what's about to run, including the
// service name and resolved environment variables. This helps with
// debugging connection issues ("oh, I'm pointing at the wrong URL").
func printHeader(cfg RunConfig, env []string) {
	fmt.Printf("=== servicectl: testing %s ===\n", cfg.Service.Name)
	fmt.Printf("  description: %s\n", cfg.Service.Description)
	fmt.Printf("  test pattern: %s\n", cfg.Service.TestPattern)
	fmt.Printf("  timeout: %s\n", cfg.Timeout)

	// Show the resolved values of service-specific env vars. We look
	// them up from the constructed env slice so the user sees exactly
	// what the child process will get.
	fmt.Println("  environment:")
	for _, ev := range cfg.Service.EnvVars {
		val := lookupEnv(env, ev.Name)
		if val == "" {
			val = "(not set)"
		}
		fmt.Printf("    %s=%s\n", ev.Name, val)
	}
	fmt.Println()
}

// printDryRun prints the full command that would be executed, including
// environment variable assignments, without actually running it.
func printDryRun(cfg RunConfig, env []string, args []string) {
	fmt.Println("# dry-run: would execute:")

	// Print env vars that differ from the parent process. This shows
	// only the service-specific variables, not the entire inherited env.
	for _, ev := range cfg.Service.EnvVars {
		val := lookupEnv(env, ev.Name)
		if val != "" {
			fmt.Printf("%s=%s \\\n", ev.Name, val)
		}
	}

	fmt.Printf("  go %s\n", strings.Join(args, " "))
}

// execute runs `go test` with the given args and environment, streaming
// stdout and stderr to the terminal. Returns the process exit code.
func execute(args []string, env []string) int {
	cmd := exec.Command("go", args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Run and capture the exit code. exec.ExitError carries the code;
	// other errors (e.g., go binary not found) get exit code 1.
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		// Non-exit errors (binary not found, permission denied, etc.)
		fmt.Fprintf(os.Stderr, "servicectl: exec error: %v\n", err)
		return 1
	}
	return 0
}

// lookupEnv finds the value of a named variable in a KEY=VALUE slice.
// Returns empty string if not found. This searches the slice rather than
// os.Getenv because we need to look up values in the *child* environment,
// which may differ from the parent's.
func lookupEnv(env []string, name string) string {
	prefix := name + "="
	// Walk backwards so later entries (overrides) win.
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return env[i][len(prefix):]
		}
	}
	return ""
}

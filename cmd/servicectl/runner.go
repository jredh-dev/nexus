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
	"path/filepath"
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
//  1. Finds the repo root (walks up looking for go.mod).
//  2. Builds the environment by merging defaults with any user overrides.
//  3. Constructs the `go test` command with appropriate flags.
//  4. Either prints the command (dry-run) or executes it.
func Run(cfg RunConfig) int {
	// Find the nexus repo root so `./tests/integration/...` resolves
	// regardless of where servicectl is invoked from.
	repoRoot, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "servicectl: %v\n", err)
		return 1
	}

	// Build the environment for the child process. Start with the
	// current process environment so things like PATH, GOPATH, HOME
	// are inherited. Then layer on service-specific defaults, and
	// finally apply any user overrides.
	env := buildEnv(cfg)

	// Construct the go test arguments.
	args := buildArgs(cfg)

	if cfg.DryRun {
		printDryRun(cfg, env, args, repoRoot)
		return 0
	}

	// Print a header so the user knows what's running.
	printHeader(cfg, env, repoRoot)

	return execute(args, env, repoRoot)
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
//
//	go test -tags integration -run "^TestVN" -timeout 30s [-v] ./tests/integration/...
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
// service name, repo root, and resolved environment variables. This helps
// with debugging connection issues ("oh, I'm pointing at the wrong URL")
// and path issues ("tests ran from the wrong directory").
func printHeader(cfg RunConfig, env []string, repoRoot string) {
	fmt.Printf("=== servicectl: testing %s ===\n", cfg.Service.Name)
	fmt.Printf("  description: %s\n", cfg.Service.Description)
	fmt.Printf("  test pattern: %s\n", cfg.Service.TestPattern)
	fmt.Printf("  timeout: %s\n", cfg.Timeout)
	fmt.Printf("  repo root: %s\n", repoRoot)

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
// environment variable assignments and working directory, without
// actually running it.
func printDryRun(cfg RunConfig, env []string, args []string, repoRoot string) {
	fmt.Println("# dry-run: would execute:")
	fmt.Printf("# working directory: %s\n", repoRoot)

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
// stdout and stderr to the terminal. The command runs from repoRoot so
// that relative test paths like ./tests/integration/... resolve correctly
// regardless of where servicectl was invoked.
func execute(args []string, env []string, repoRoot string) int {
	cmd := exec.Command("go", args...)
	cmd.Dir = repoRoot
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

// findRepoRoot locates the nexus repo root. It tries three strategies
// in order:
//  1. Walk up from cwd looking for go.mod (works inside the repo or a worktree).
//  2. Check $WORK/work/source/jredh-dev/nexus (the canonical source checkout).
//  3. Give up with a helpful error.
//
// This means `servicectl test vn` works from anywhere — inside the repo,
// inside a worktree, or from ~ with $WORK set.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}

	// Strategy 1: walk up from cwd.
	if root, ok := walkUpForGoMod(dir); ok {
		return root, nil
	}

	// Strategy 2: check $WORK/work/source/jredh-dev/nexus.
	if work := os.Getenv("WORK"); work != "" {
		candidate := filepath.Join(work, "work", "source", "jredh-dev", "nexus")
		if isNexusRoot(candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf(
		"nexus repo root not found (searched up from %s, WORK=%s); "+
			"run from within the nexus repo, or set $WORK to the agentic root",
		dir, os.Getenv("WORK"),
	)
}

// walkUpForGoMod walks from dir toward the filesystem root looking for
// a go.mod containing the nexus module declaration.
func walkUpForGoMod(dir string) (string, bool) {
	for {
		if isNexusRoot(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// isNexusRoot returns true if dir contains a go.mod with the nexus module.
func isNexusRoot(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "module github.com/jredh-dev/nexus")
}

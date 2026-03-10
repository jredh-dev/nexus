// Command github-mcp is a Model Context Protocol server exposing GitHub API operations.
// It provides tools for PR management, CI checks, RC branch lifecycle, and issue tracking.
//
// Auth: reads GITHUB_TOKEN from the environment.
// Usage:
//
//	github-mcp [--addr :8091] [--help]
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/jredh-dev/nexus/internal/mcp"
	"github.com/jredh-dev/nexus/services/mcp/github/internal/tools"
)

// version is set via -ldflags at build time.
var version = "dev"

const instructions = `GitHub MCP server — exposes GitHub API operations as MCP tools.

Available tools:
  pr_list     — list open PRs for a repo
  pr_get      — get PR details + CI check status
  pr_create   — open a new pull request
  pr_merge    — merge a pull request (squash/merge/rebase)
  pr_checks   — get CI check runs for a PR or branch ref
  rc_list     — list rc/* branches
  rc_current  — get the current rc/* branch name
  rc_create   — create the next rc/vX.Y.Z branch
  issue_list  — list open issues
  issue_create — create a new issue
  issue_label — add or remove labels on an issue

Auth: GITHUB_TOKEN must be set in the environment.`

func main() {
	os.Exit(run())
}

func run() int {
	// Support --help, -h, -?
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--help", "-h", "-?", "help":
			printUsage()
			return 0
		}
	}

	fs := flag.NewFlagSet("github-mcp", flag.ExitOnError)
	addr := fs.String("addr", ":8091", "HTTP listen address")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	logger := log.New(os.Stderr, "", log.LstdFlags)

	// Warn early if token is missing (tools will fail at call time, not at startup).
	if os.Getenv("GITHUB_TOKEN") == "" {
		logger.Println("[warn] GITHUB_TOKEN not set — all tool calls will fail")
	}

	mcp.Version = version
	srv := mcp.NewServer(logger, "github-mcp", instructions)
	tools.RegisterAll(srv)

	logger.Printf("[github-mcp] version=%s addr=%s", version, *addr)
	if err := mcp.ServeHTTP(srv, *addr); err != nil {
		logger.Printf("[github-mcp] fatal: %v", err)
		return 1
	}
	return 0
}

func printUsage() {
	fmt.Println(`github-mcp — GitHub MCP server

Usage:
  github-mcp [--addr <host:port>]

Flags:
  --addr  HTTP listen address (default :8091)
  --help  Show this help

Environment:
  GITHUB_TOKEN  GitHub personal access token (required)

Endpoints:
  POST   /mcp     JSON-RPC MCP requests
  GET    /mcp     SSE stream
  GET    /health  Health check`)
}

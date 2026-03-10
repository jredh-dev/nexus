// Command gcloud-mcp is a Model Context Protocol server exposing GCP infrastructure
// visibility as MCP tools. All operations are read-only — plan only, no apply.
//
// Tools:
//
//	get_service_status  — Cloud Run service health, revisions, traffic splits
//	query_logs          — Cloud Logging entries filtered by service/severity/time
//	terraform_plan      — Run terraform plan (no apply) for a nexus service
//
// Auth: reads GOOGLE_APPLICATION_CREDENTIALS from the environment (path to SA key JSON).
// Falls back to GCE metadata server when running inside GCP.
//
// Usage:
//
//	gcloud-mcp [--addr :8093] [--help]
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/jredh-dev/nexus/internal/mcp"
	"github.com/jredh-dev/nexus/services/mcp/gcloud/internal/tools"
)

// version is set via -ldflags at build time.
var version = "dev"

const instructions = `GCP infrastructure MCP server — read-only visibility into Cloud Run, Cloud Logging, and Terraform.

Available tools:
  get_service_status  — Cloud Run service status: readiness, revisions, traffic splits, image
  query_logs          — Cloud Logging entries filtered by service, severity, and time range
  terraform_plan      — Run terraform plan (no apply) for a nexus service; returns the diff

All operations are read-only. terraform_plan runs plan only — use the GitHub PR workflow to apply changes.

Auth: GOOGLE_APPLICATION_CREDENTIALS must point to a GCP service account key JSON file.
      Falls back to the GCE metadata server when running inside GCP.`

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

	fs := flag.NewFlagSet("gcloud-mcp", flag.ExitOnError)
	addr := fs.String("addr", ":8093", "HTTP listen address")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	logger := log.New(os.Stderr, "", log.LstdFlags)

	// Warn early if credentials are missing (tools will fail at call time).
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
		logger.Println("[warn] GOOGLE_APPLICATION_CREDENTIALS not set — falling back to GCE metadata server")
	}

	mcp.Version = version
	srv := mcp.NewServer(logger, "gcloud-mcp", instructions)
	tools.RegisterAll(srv)

	logger.Printf("[gcloud-mcp] version=%s addr=%s", version, *addr)
	if err := mcp.ServeHTTP(srv, *addr); err != nil {
		logger.Printf("[gcloud-mcp] fatal: %v", err)
		return 1
	}
	return 0
}

func printUsage() {
	fmt.Println(`gcloud-mcp — GCP infrastructure MCP server (read-only)

Usage:
  gcloud-mcp [--addr <host:port>]

Flags:
  --addr  HTTP listen address (default :8093)
  --help  Show this help

Environment:
  GOOGLE_APPLICATION_CREDENTIALS  Path to GCP service account key JSON file (required outside GCP)

Endpoints:
  POST   /mcp     JSON-RPC MCP requests
  GET    /mcp     SSE stream
  GET    /health  Health check`)
}

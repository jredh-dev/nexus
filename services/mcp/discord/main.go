// Command discord-mcp is a Model Context Protocol server exposing Discord webhook operations.
// It provides a single tool for sending event notifications to Discord.
//
// Auth: reads DISCORD_WEBHOOK_URL and DISCORD_ENABLED from the environment.
// Usage:
//
//	discord-mcp [--addr :8092] [--help]
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/jredh-dev/nexus/internal/mcp"
	"github.com/jredh-dev/nexus/services/mcp/discord/internal/tools"
)

// version is set via -ldflags at build time.
var version = "dev"

const instructions = `Discord MCP server — sends event notifications to Discord via webhook.

Available tools:
  notify_discord — send a deploy/CI/build/test event notification

Auth:
  DISCORD_WEBHOOK_URL  Discord webhook URL (required)
  DISCORD_ENABLED      Must be "true" to send notifications (defaults to disabled)`

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--help", "-h", "-?", "help":
			printUsage()
			return 0
		}
	}

	fs := flag.NewFlagSet("discord-mcp", flag.ExitOnError)
	addr := fs.String("addr", ":8092", "HTTP listen address")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	logger := log.New(os.Stderr, "", log.LstdFlags)

	if os.Getenv("DISCORD_WEBHOOK_URL") == "" {
		logger.Println("[warn] DISCORD_WEBHOOK_URL not set — notifications will be skipped")
	}
	if os.Getenv("DISCORD_ENABLED") != "true" {
		logger.Println("[warn] DISCORD_ENABLED != true — notifications are disabled")
	}

	mcp.Version = version
	srv := mcp.NewServer(logger, "discord-mcp", instructions)
	tools.RegisterAll(srv)

	logger.Printf("[discord-mcp] version=%s addr=%s", version, *addr)
	if err := mcp.ServeHTTP(srv, *addr); err != nil {
		logger.Printf("[discord-mcp] fatal: %v", err)
		return 1
	}
	return 0
}

func printUsage() {
	fmt.Println(`discord-mcp — Discord MCP server

Usage:
  discord-mcp [--addr <host:port>]

Flags:
  --addr  HTTP listen address (default :8092)
  --help  Show this help

Environment:
  DISCORD_WEBHOOK_URL  Discord webhook URL (required)
  DISCORD_ENABLED      Set to "true" to enable sending (default: disabled)

Endpoints:
  POST   /mcp     JSON-RPC MCP requests
  GET    /mcp     SSE stream
  GET    /health  Health check`)
}

// Command server runs the discord-monitor HTTP server.
//
// It connects to PostgreSQL (running migrations on startup), optionally
// initializes a selfbot client for Discord user API access, and serves
// the monitoring HTTP API.
//
// Environment variables:
//
//	PORT                     HTTP listen port (default: "8080")
//	DATABASE_URL             PostgreSQL connection string
//	                         (default: "host=/tmp/ctl-pg dbname=discord_monitor user=jredh")
//	DISCORD_SELFBOT_TOKEN    Discord user token for selfbot mode (optional)
//	SCAN_INTERVAL_SELFBOT    Polling interval for selfbot scanner (default: "60s")
//
// Flags:
//
//	--port    HTTP listen port (overrides PORT env)
//	--db      PostgreSQL connection string (overrides DATABASE_URL env)
//	--help, -h, -?    Print usage and exit
//
// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jredh-dev/nexus/services/discord-monitor/internal/database"
	"github.com/jredh-dev/nexus/services/discord-monitor/internal/selfbot"
	"github.com/jredh-dev/nexus/services/discord-monitor/internal/server"
)

func main() {
	os.Exit(run())
}

func run() int {
	fs := flag.NewFlagSet("discord-monitor", flag.ContinueOnError)
	portFlag := fs.String("port", "", "HTTP listen port (overrides PORT env)")
	dbFlag := fs.String("db", "", "PostgreSQL connection string (overrides DATABASE_URL env)")

	// Check for help flags before parsing to handle -? which flag doesn't
	// support natively.
	if len(os.Args) > 1 {
		arg := os.Args[1]
		if arg == "--help" || arg == "-h" || arg == "-?" || arg == "help" {
			fs.Usage()
			return 0
		}
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
		return 1
	}

	// Resolve config: flag > env > default.
	port := resolve(*portFlag, os.Getenv("PORT"), "8080")
	dbURL := resolve(*dbFlag, os.Getenv("DATABASE_URL"), "host=/tmp/ctl-pg dbname=discord_monitor user=jredh")
	selfbotToken := os.Getenv("DISCORD_SELFBOT_TOKEN")
	scanInterval := parseDuration(os.Getenv("SCAN_INTERVAL_SELFBOT"), 60*time.Second)

	log.Printf("[discord-monitor] starting: port=%s db_configured=true selfbot=%v scan_interval=%s",
		port, selfbotToken != "", scanInterval)

	// Connect to PostgreSQL and run migrations.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := database.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("[discord-monitor] database: %v", err)
	}
	defer db.Close()
	log.Println("[discord-monitor] database connected, migrations applied")

	// Optionally initialize selfbot client and verify token.
	selfbotConnected := false
	if selfbotToken != "" {
		client := selfbot.New(selfbotToken)
		user, err := client.GetMe(ctx)
		if err != nil {
			log.Printf("[discord-monitor] selfbot token validation failed: %v", err)
			log.Println("[discord-monitor] continuing without selfbot — API-only mode")
		} else {
			selfbotConnected = true
			log.Printf("[discord-monitor] selfbot authenticated as %s#%s (%s)",
				user.Username, user.Discriminator, user.ID)
			// The scan loop will be added in a future phase — for now we
			// just validate the token and log success.
			_ = client
			_ = scanInterval
		}
	}

	// Build and start the HTTP server.
	srv := server.New(server.Config{
		DB:               db,
		SelfbotConnected: selfbotConnected,
	})

	srv.OnStop(func() {
		log.Println("[discord-monitor] shutting down database connection")
		db.Close()
	})

	log.Printf("[discord-monitor] listening on :%s", port)
	if err := srv.ListenAndServe(":" + port); err != nil {
		log.Fatalf("[discord-monitor] server: %v", err)
	}

	return 0
}

// resolve returns the first non-empty string from the arguments.
// Used for flag > env > default priority.
func resolve(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseDuration parses a duration string, returning fallback on error or
// empty input. Supports Go duration strings like "30s", "5m", "1h".
func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Printf("[discord-monitor] invalid duration %q, using default %s", s, fallback)
		return fallback
	}
	return d
}

// Ensure fmt is used (for future expansion).
var _ = fmt.Sprintf

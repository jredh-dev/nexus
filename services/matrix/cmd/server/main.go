// nexus-matrix — dev hub dashboard
// Copyright (C) 2026  nexus contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// matrix serves a live dev hub dashboard: service health from Gatus,
// CI/deploy status from GitHub Actions and Gitea Actions, all grouped
// by service. No JS — server-side rendered on every request.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	gohttp "github.com/jredh-dev/nexus/services/go-http"
	"github.com/jredh-dev/nexus/services/go-http/config"
	"github.com/jredh-dev/nexus/services/matrix/internal/events"
	"github.com/jredh-dev/nexus/services/matrix/internal/page"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("nexus-matrix %s\n", version)
		fmt.Printf("Commit: %s\n", commit)
		fmt.Printf("Built: %s\n", buildDate)
		os.Exit(0)
	}

	cfg := config.Load()
	pageCfg := page.ConfigFromEnv()

	// --- Portal events subscriber ---
	// PORTAL_EVENTS_DSN is the libpq DSN for the portal database.
	// The matrix service subscribes read-only on the portal.events channel.
	// If unset, the event feed is disabled (matrix still serves the dashboard).
	evtBuf := events.NewBuffer()
	if dsn := os.Getenv("PORTAL_EVENTS_DSN"); dsn != "" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		events.StartSubscriber(ctx, dsn, evtBuf)
		log.Printf("matrix: portal.events subscriber started")
	} else {
		log.Printf("matrix: PORTAL_EVENTS_DSN not set — event feed disabled")
	}

	srv := gohttp.New()

	// SSE endpoint: clients connect to receive a live stream of portal events.
	srv.Router.Get("/events/stream", events.SSEHandler(evtBuf))

	// Single route: render the live dashboard.
	srv.Router.Get("/", page.Handler(pageCfg))

	addr := ":" + cfg.Port
	log.Printf("nexus-matrix starting on %s", addr)
	log.Printf("  Dashboard:    http://localhost%s/", addr)
	log.Printf("  Events SSE:   http://localhost%s/events/stream", addr)
	log.Printf("  Gatus:        %s", pageCfg.GatusURL)
	log.Printf("  Gitea:        %s", pageCfg.GiteaURL)

	if err := srv.ListenAndServe(addr); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

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
	"flag"
	"fmt"
	"log"
	"os"

	gohttp "github.com/jredh-dev/nexus/services/go-http"
	"github.com/jredh-dev/nexus/services/go-http/config"
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

	srv := gohttp.New()

	// Single route: render the live dashboard.
	srv.Router.Get("/", page.Handler(pageCfg))

	addr := ":" + cfg.Port
	log.Printf("nexus-matrix starting on %s", addr)
	log.Printf("  Dashboard: http://localhost%s/", addr)
	log.Printf("  Gatus:     %s", pageCfg.GatusURL)
	log.Printf("  Gitea:     %s", pageCfg.GiteaURL)

	if err := srv.ListenAndServe(addr); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

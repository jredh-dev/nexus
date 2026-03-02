// nexus-secrets - count-based secret admission service
// Copyright (C) 2026  nexus contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	gohttp "github.com/jredh-dev/nexus/services/go-http"
	"github.com/jredh-dev/nexus/services/go-http/config"
	"github.com/jredh-dev/nexus/services/secrets/internal/handlers"
	"github.com/jredh-dev/nexus/services/secrets/internal/store"
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
		fmt.Printf("nexus-secrets %s\n", version)
		fmt.Printf("Commit: %s\n", commit)
		fmt.Printf("Built: %s\n", buildDate)
		os.Exit(0)
	}

	cfg := config.Load()
	s := store.New()
	h := handlers.New(s)

	srv := gohttp.New()
	srv.OnStop(h.Stop)

	// The riddle — start here
	srv.Router.Get("/", h.Riddle)
	srv.Router.Get("/api/riddle", h.Riddle)

	// Secrets API
	srv.Router.Post("/api/secrets", h.Submit)
	srv.Router.Get("/api/secrets", h.List)
	srv.Router.Get("/api/secrets/{id}", h.Get)
	srv.Router.Get("/api/stats", h.Stats)
	srv.Router.Get("/api/exposed", h.Exposed)

	addr := ":" + cfg.Port
	log.Printf("nexus-secrets starting on %s", addr)
	log.Printf("  Riddle:  http://localhost%s/", addr)
	log.Printf("  API:     http://localhost%s/api/", addr)

	if err := srv.ListenAndServe(addr); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

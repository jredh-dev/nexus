// Command server runs the visual novel engine HTTP server.
//
// It connects to PostgreSQL, loads YAML story definitions (with optional
// hot-reload via fsnotify), and serves the HTTP API for story navigation,
// chapter browsing, voting, reader tracking, and video streaming.
//
// Environment variables:
//
//	PORT           HTTP listen port (default: 8080)
//	DATABASE_URL   PostgreSQL connection string
//	               (default: "host=/tmp/ctl-pg dbname=vn user=jredh")
//	STORY_DIR      Path to directory containing YAML story files
//	               (default: "./stories")
//	HOT_RELOAD     Enable fsnotify hot-reload of story files ("true"/"false")
//	               (default: "true")
//
// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/jredh-dev/nexus/services/vn/internal/database"
	"github.com/jredh-dev/nexus/services/vn/internal/engine"
	"github.com/jredh-dev/nexus/services/vn/internal/server"
)

func main() {
	port := envOr("PORT", "8080")
	dbURL := envOr("DATABASE_URL", "host=/tmp/ctl-pg dbname=vn user=jredh")
	storyDir := envOr("STORY_DIR", "./stories")
	hotReload := envOr("HOT_RELOAD", "true")

	log.Printf("[vn] starting: port=%s db=%s stories=%s hot_reload=%s",
		port, dbURL, storyDir, hotReload)

	// Connect to PostgreSQL and run migrations.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := database.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("[vn] database: %v", err)
	}
	defer db.Close()
	log.Println("[vn] database connected, migrations applied")

	// Load story definitions.
	var nav *engine.Navigator
	var loader *engine.HotLoader

	if hotReload == "true" {
		// Hot-reload mode: watch story dir for changes, atomically swap the
		// navigator's story on each reload.
		loader, err = engine.NewHotLoader(storyDir, func(s *engine.Story) {
			log.Printf("[vn] story reloaded: %q v%d (%d chapters)",
				s.Title, s.Version, len(s.Chapters))
		})
		if err != nil {
			log.Fatalf("[vn] hot-reload story from %s: %v", storyDir, err)
		}
		defer loader.Close()
		nav = engine.NewNavigator(loader.Story())
		log.Printf("[vn] hot-reload enabled, watching %s", storyDir)
	} else {
		// Static mode: load once at startup.
		story, err := engine.LoadStoryDir(storyDir)
		if err != nil {
			log.Fatalf("[vn] load story from %s: %v", storyDir, err)
		}
		nav = engine.NewNavigator(story)
		log.Printf("[vn] story loaded: %q v%d (%d chapters)",
			story.Title, story.Version, len(story.Chapters))
	}

	// Build and start the HTTP server.
	srv := server.New(server.Config{
		DB:        db,
		Navigator: nav,
		Loader:    loader,
	})

	// Register cleanup: close DB on graceful shutdown.
	srv.OnStop(func() {
		log.Println("[vn] shutting down database connection")
		db.Close()
	})

	log.Printf("[vn] listening on :%s", port)
	if err := srv.ListenAndServe(":" + port); err != nil {
		log.Fatalf("[vn] server: %v", err)
	}
}

// envOr returns the value of the environment variable named key, or fallback
// if the variable is empty or unset.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

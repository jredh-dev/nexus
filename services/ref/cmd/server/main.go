// ref-server — async prompt execution queue HTTP API
//
// Environment variables:
//
//	DATABASE_URL            PostgreSQL DSN (default: host=postgres dbname=ref user=ref password=ref-dev-password)
//	PORT                    listen port (default: 8086)
//	OPENCODE_URL            OpenCode server base URL (default: http://opencode:4096)
//	OPENCODE_SERVER_PASSWORD  OpenCode basic-auth password (default: bigdaddy)
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jredh-dev/nexus/services/ref/internal/db"
	"github.com/jredh-dev/nexus/services/ref/internal/handler"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg := loadConfig()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	slog.Info("database migrations applied")

	h := handler.New(pool, cfg.openCodeURL, cfg.openCodePW)
	mux := http.NewServeMux()
	h.Register(mux)

	addr := ":" + cfg.port
	slog.Info("ref-server starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

type config struct {
	databaseURL string
	port        string
	openCodeURL string
	openCodePW  string
}

func loadConfig() config {
	c := config{
		databaseURL: env("DATABASE_URL", "host=postgres dbname=ref user=ref password=ref-dev-password"),
		port:        env("PORT", "8086"),
		openCodeURL: env("OPENCODE_URL", "http://opencode:4096"),
		openCodePW:  env("OPENCODE_SERVER_PASSWORD", "bigdaddy"),
	}
	slog.Info("config loaded",
		"port", c.port,
		"opencode_url", c.openCodeURL,
		"database_url", maskPassword(c.databaseURL),
	)
	return c
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// maskPassword replaces the password= value in a DSN with *****.
func maskPassword(dsn string) string {
	// Simple: hide anything after "password="
	const marker = "password="
	idx := 0
	for i := 0; i < len(dsn)-len(marker); i++ {
		if dsn[i:i+len(marker)] == marker {
			idx = i + len(marker)
			break
		}
	}
	if idx == 0 {
		return dsn
	}
	end := idx
	for end < len(dsn) && dsn[end] != ' ' {
		end++
	}
	return dsn[:idx] + "*****" + dsn[end:]
}

// Ensure maskPassword is used (avoids unused import in tests).
var _ = fmt.Sprintf

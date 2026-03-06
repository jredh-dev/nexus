# matrix

Local dev hub dashboard. Single static HTML page served at `:8085` — links to all services, Cloud Run deployments, CI badges, deploy workflow badges, repos, and external tooling.

## Stop here if...

- You're looking for a service API — this has none
- You're working on health monitoring — that's Gatus at `:8084`

## What's here

- `cmd/server/main.go` — entry point, uses go-http scaffold
- `internal/page/handler.go` — single HTTP handler, serves embedded HTML
- `internal/page/index.html` — the dashboard (inline CSS, no JS, no CDN)
- `Dockerfile` — build context is repo root

## Run / Build / Test

```bash
# Build
go build ./services/matrix/cmd/server

# Run locally
PORT=8085 go run ./services/matrix/cmd/server

# Docker (from repo root)
docker build -f services/matrix/Dockerfile -t nexus-matrix .
docker run -p 8085:8080 nexus-matrix

# Via docker compose (agentic root)
docker compose up -d matrix
```

Dashboard at http://localhost:8085

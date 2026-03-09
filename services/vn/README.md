# vn

Visual novel engine for branching narrative with video, subtitles, and community voting.

## Stop here if...
- You're looking for the TUI client -- that's in `cmd/tui/`
- You're looking for the gRPC server -- that's in `services/rust-grpc/`
- You're looking for the web portal -- that's in `services/portal/`

## What's here

```
services/vn/
├── cmd/server/             -- server entrypoint (main.go)
├── internal/
│   ├── database/           -- PostgreSQL schema, migrations, CRUD (pgx + large objects)
│   ├── engine/             -- Story graph traversal, chapter state machine, YAML hot-reload
│   ├── server/             -- HTTP API handlers (chi router, uses shared go-http helpers)
│   ├── state/              -- Schema-free YAML state persistence (JSON in → YAML store → JSON out)
│   ├── subtitle/           -- Toggle-point visibility engine
│   └── video/              -- FFmpeg palindrome generation, transcoding, HTTP streaming
├── stories/                -- YAML story definitions (seed.yaml)
├── web/                    -- Mobile-first HTML/CSS/JS client (TODO)
├── Dockerfile              -- Multi-stage build with ffmpeg
└── README.md               -- this file
```

## Database

PostgreSQL 16 (Docker, TCP). Database: `vn`.

```bash
psql -h localhost -d vn
```

**Tables:** `videos`, `significant_events`, `subtitles`, `readers`, `votes`, `schema_version`

## Run / Build / Test

```bash
# Build server
go build ./services/vn/cmd/server

# Run locally (requires PostgreSQL at localhost:5432 with database 'vn')
DATABASE_URL="host=localhost port=5432 dbname=vn user=jredh" \
STORY_DIR=services/vn/stories \
HOT_RELOAD=true \
go run ./services/vn/cmd/server

# Run unit tests
go test ./services/vn/...

# Run integration tests (requires running server)
VN_URL=http://localhost:8082 go test -tags integration ./tests/integration/ -run TestVN
```

## Docker

```bash
# From agentic root (docker-compose.yml lives there)
docker compose up -d vn        # starts vn + vn-postgres
docker compose logs -f vn      # tail logs
```

Port 8082 (host) -> 8080 (container). Uses dedicated `vn-postgres` container with `vn-pgdata` volume.

## Environment

| Var | Default | Description |
|-----|---------|-------------|
| `PORT` | `8080` | HTTP listen port |
| `DATABASE_URL` | (required) | PostgreSQL connection string |
| `STORY_DIR` | `stories` | Path to YAML story files |
| `HOT_RELOAD` | `false` | Watch story files with fsnotify |

# discord-monitor

Automated Discord server monitoring with activity tracking, keyword alerts, and digest generation.

## Stop here if...
- You're looking for the TUI client -- that's in `cmd/tui/`
- You're looking for the web portal -- that's in `services/portal/`
- You're looking for the visual novel engine -- that's in `services/vn/`

## What's here

```
services/discord-monitor/
├── cmd/server/             -- server entrypoint (main.go)
├── internal/
│   ├── database/           -- PostgreSQL schema, migrations, CRUD (pgx)
│   ├── selfbot/            -- Discord user API client (browser-mimicking HTTP)
│   └── server/             -- HTTP API handlers (chi router, uses go-http scaffold)
├── Dockerfile              -- multi-stage build
└── README.md               -- this file
```

## Database

PostgreSQL 16 (Docker, TCP). Database: `discord_monitor`.

```bash
psql -h localhost -d discord_monitor
```

**Tables:** `guilds`, `channels`, `messages`, `read_cursors`, `activity_hourly`, `keywords`, `digests`, `schema_version`

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check |
| GET | `/api/guilds` | List tracked guilds (?active=true) |
| GET | `/api/unread` | Unread messages across channels (?guild_id=X) |
| GET | `/api/status` | Service status and uptime |

## Run / Build / Test

```bash
# Build server
go build ./services/discord-monitor/cmd/server

# Run locally (requires PostgreSQL at localhost:5432 with database 'discord_monitor')
DATABASE_URL="host=localhost port=5432 dbname=discord_monitor user=jredh" \
go run ./services/discord-monitor/cmd/server

# With selfbot (optional)
DISCORD_SELFBOT_TOKEN="your_user_token" \
DATABASE_URL="host=localhost port=5432 dbname=discord_monitor user=jredh" \
go run ./services/discord-monitor/cmd/server
```

## Environment

| Var | Default | Description |
|-----|---------|-------------|
| `PORT` | `8080` | HTTP listen port |
| `DATABASE_URL` | `host=localhost port=5432 dbname=discord_monitor user=jredh` | PostgreSQL connection string |
| `DISCORD_SELFBOT_TOKEN` | (optional) | Discord user token for selfbot mode |
| `SCAN_INTERVAL_SELFBOT` | `60s` | Polling interval for selfbot scanner |

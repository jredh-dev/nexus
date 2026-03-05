# vn

Visual novel engine for branching narrative with video, subtitles, and community voting.

## Stop here if...
- You're looking for the TUI client -- that's in `cmd/tui/`
- You're looking for the gRPC server -- that's in `services/rust-grpc/`
- You're looking for the web portal -- that's in `services/portal/`

## What's here

```
services/vn/
├── internal/
│   ├── database/          -- PostgreSQL schema, migrations, CRUD (pgx + large objects)
│   ├── engine/            -- Story graph traversal, chapter state machine, YAML hot-reload
│   ├── server/            -- HTTP API handlers (chi router)
│   ├── subtitle/          -- Toggle-point visibility engine
│   └── video/             -- FFmpeg palindrome generation, transcoding, HTTP streaming
├── web/                   -- Mobile-first HTML/CSS/JS client (TODO)
└── README.md              -- this file
```

## Database

PostgreSQL 16 (native, Unix socket). Database: `vn`.

```bash
export PATH="/usr/local/Cellar/postgresql@16/16.13/bin:$PATH"
psql -h /tmp/ctl-pg -d vn
```

**Tables:** `videos`, `significant_events`, `subtitles`, `readers`, `votes`, `schema_version`

## Run / Build / Test

```bash
# Run tests (requires PostgreSQL at /tmp/ctl-pg)
go test ./services/vn/...
```

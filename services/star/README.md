# star

The Star (XVII) -- visual novel engine for branching narrative with video, subtitles, and community voting.

## Stop here if...
- You're looking for the TUI client -- that's in `cmd/tui/`
- You're looking for the gRPC server -- that's in `services/rust-grpc/`
- You're looking for the web portal -- that's in `services/portal/`

## What's here

```
services/star/
├── cmd/starctl/           -- CLI: video import, palindrome loops, story tools
├── internal/
│   ├── database/          -- PostgreSQL schema, migrations, CRUD (pgx + large objects)
│   ├── video/             -- FFmpeg palindrome generation, transcoding, HTTP streaming
│   └── subtitle/          -- Toggle-point visibility engine
├── web/                   -- Mobile-first HTML/CSS/JS client (TODO)
└── README.md              -- this file
```

## Database

PostgreSQL 16 (native, Unix socket). Database: `star`.

```bash
export PATH="/usr/local/Cellar/postgresql@16/16.13/bin:$PATH"
psql -h /tmp/ctl-pg -d star
```

**Tables:** `videos`, `significant_events`, `subtitles`, `readers`, `votes`, `schema_version`

## Run / Build / Test

```bash
# Build CLI
go build -o bin/starctl ./services/star/cmd/starctl

# Run tests (requires PostgreSQL at /tmp/ctl-pg)
go test ./services/star/...

# Import a video
./bin/starctl video import clip.mp4 --name "prologue" --codec h264 --duration 5000

# Generate palindrome loop
./bin/starctl video palindrome input.mp4 --output loop.mp4

# List videos
./bin/starctl video list
```

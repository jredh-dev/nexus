# digest

Kafka consumer service that reduces the `realtime` event stream into per-tile
metric snapshots and publishes them to the `digest` topic every 15 minutes.

## Stop here if...

- You're looking for the realtime event producer — that's in `services/realtime/`
- You're looking for the digest TUI / CLI — that's in `cmd/digest/`
- You're working on tile type definitions — see `internal/tiles/tiles.go`

## What's here

- `cmd/main.go` — entry point; reads env, wires consumer + HTTP server
- `internal/tiles/` — TileValue, TileSnapshot, OverrideRecord types
- `internal/consumer/` — dual Kafka consumer (realtime + digest) + reducer ticker
- `internal/server/` — go-http routes: GET /health, GET /tiles
- `Dockerfile` — multi-stage build (build context: repo root)

## Run / Build / Test

```bash
# from nexus root
go build ./services/digest/...

# required env
export REALTIME_KEY=<64-char hex>
export KAFKA_ADDR=kafka:9092        # default
export KAFKA_TOPIC_REALTIME=realtime # default
export KAFKA_TOPIC_DIGEST=digest    # default
export KAFKA_GROUP_DIGEST=digest-service # default
export DIGEST_TICK_INTERVAL=15m    # default
export PORT=8096                   # default

go run ./services/digest/cmd
```

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /health | 200 OK |
| GET | /tiles | Current TileSnapshot as JSON |

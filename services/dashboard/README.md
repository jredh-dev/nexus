# nexus-dashboard

Live ops dashboard — realtime event log (SSE) + digest tiles (polling).

## Stop here if...
- You're looking for the Kafka producer → `services/realtime`
- You're looking for tile aggregation logic → `services/digest`
- You're looking for shared Kafka types → `internal/kafka`

## What's here
- `cmd/main.go` — entry point, env config, HTTP server start
- `internal/server/server.go` — HTTP routes, SSE hub, Kafka consumer, tiles proxy
- `internal/server/html.go` — embedded dashboard HTML/CSS/JS

## Run / Build / Test
```bash
PORT=8098 KAFKA_ADDR=localhost:9092 REALTIME_KEY=<64-hex> DIGEST_ADDR=http://localhost:8096 \
  go run ./services/dashboard/cmd

docker compose build nexus-dashboard && docker compose up -d nexus-dashboard
```

## Env vars
| Var | Default | Description |
|-----|---------|-------------|
| `PORT` | `8098` | Listen port |
| `KAFKA_ADDR` | `kafka:9092` | Kafka broker |
| `KAFKA_TOPIC_REALTIME` | `realtime` | Topic to consume |
| `DIGEST_ADDR` | `http://nexus-digest:8096` | Digest service base URL |
| `REALTIME_KEY` | *(required)* | 64-char hex AES-256-GCM key |

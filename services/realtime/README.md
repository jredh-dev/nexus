# realtime

Kafka producer service: encrypts event envelopes with AES-256-GCM and publishes them to the `realtime` topic. Includes a synthetic ticker for dev/demo, and a `/publish` HTTP endpoint.

## Stop here if...
- You're looking for the Kafka consumer — that's in `services/digest/`
- You're looking for shared event types / crypto — those are in `internal/kafka/`
- You're working on the HTTP scaffold — see `services/go-http/`

## What's here
- `cmd/main.go` — entry point: config, producer init, ticker goroutine, HTTP server
- `internal/producer/producer.go` — kafka-go writer + AES-256-GCM encrypt + publish
- `internal/server/server.go` — chi routes: `GET /health`, `POST /publish`
- `Dockerfile` — multi-stage, Go 1.24, scratch final image

## Env vars
| Var | Default | Notes |
|---|---|---|
| `REALTIME_KEY` | *(required)* | 64-char hex AES-256-GCM key |
| `KAFKA_ADDR` | `kafka:9092` | Broker address |
| `KAFKA_TOPIC_REALTIME` | `realtime` | Topic name |
| `REALTIME_SOURCE` | `realtime` | Source label in envelopes |
| `REALTIME_TICKER_INTERVAL` | `2s` | Synthetic event interval |
| `PORT` | `8095` | HTTP port |

## Run / Build / Test
```bash
# from nexus root
go build ./services/realtime/...

# generate a dev key
export REALTIME_KEY=$(openssl rand -hex 32)

# run locally (assumes Kafka on localhost:9092)
KAFKA_ADDR=localhost:9092 go run ./services/realtime/cmd/

# POST a manual event
curl -s -X POST http://localhost:8095/publish \
  -H 'Content-Type: application/json' \
  -d '{"level":"WARN","msg":"test event","fields":{"env":"local"}}'
```

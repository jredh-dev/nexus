# sms-sender

Kafka-driven outbound SMS service for nascent-nexus.

Reads `OutboundMessage` records from the `sms-outbox` Kafka topic and delivers
them as real SMS messages via a pluggable backend.  Phase 1 backend: Telnyx.
Planned Phase 2 backend: Android phone with a local SIM (zero third-party
visibility).

---

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                     agentic-network                      │
│                                                          │
│  [agentic-kafka:9092]                                    │
│         │                                                │
│         │  consume sms-outbox                           │
│         ▼                                                │
│  [nascent-nexus sms-sender container]                    │
│   internal/sms.Consumer                                  │
│         │                                                │
│         │  internal/sms.Sender interface                 │
│         ▼                                                │
│   internal/sms.TelnyxSender  (Phase 1)                  │
└──────────────────┬───────────────────────────────────────┘
                   │  HTTPS POST /v2/messages
                   ▼
            api.telnyx.com
                   │
                   ▼
            your phone
```

The `sms-sender` container joins the existing `agentic-network` Docker network
so it can reach the Kafka broker at `kafka:9092` by service-discovery hostname,
without any port-mapping changes to the existing stack.

---

## Kafka Topics

| Topic | Direction | Purpose |
|---|---|---|
| `sms-outbox` | consumed | Outbound messages to deliver |
| `sms-dlq` | produced | Messages that exhausted all retries |

### Message schema

```json
{
  "id":   "550e8400-e29b-41d4-a716-446655440000",
  "to":   "+15551234567",
  "body": "hello world"
}
```

`id` — client-generated UUID.  Used for correlation in logs.  If you replay a
partition, the same `id` will appear in logs twice — useful for detecting
duplicate sends.

`to` — E.164 phone number of the recipient.

`body` — UTF-8 SMS text.  Carriers truncate at 160 GSM-7 characters for a
single segment; concatenation is handled transparently by the carrier for longer
messages.

---

## Delivery guarantees

**At-least-once.**  The consumer commits the Kafka offset only after
`Sender.Send` returns nil.  If the process crashes between a successful send
and a successful commit, the message will be re-delivered and sent again.

This is an intentional trade-off: a silent miss (losing a message) is worse
than a duplicate text.  Producers that need deduplication should use stable
`id` values and check logs or a Redis set on replay.

**Retry policy:**  up to 3 attempts with 2s / 4s exponential backoff before
routing to `sms-dlq`.

---

## Sender interface

```go
type Sender interface {
    Send(ctx context.Context, msg OutboundMessage) error
}
```

Any backend only needs to implement this one method.  Swapping providers
requires:

1. Create `internal/sms/<name>_sender.go` implementing `Sender`
2. Change one line in `cmd/sms-sender/main.go` to instantiate the new sender

No changes to the Kafka consumer, Docker setup, or message schema.

### Phase 1 — Telnyx (`TelnyxSender`)

Uses the Telnyx REST API (`POST /v2/messages`) via stdlib `net/http`.  No SDK
dependency.  See [docs/telnyx-setup.md](../../docs/telnyx-setup.md) for
provisioning steps.

**Privacy trade-off:** Telnyx is a US commercial entity.  It does not sell data
to third parties, but it is subject to US law enforcement requests (FISA, NSLs,
CALEA).  Use this backend for development and low-sensitivity notifications.

### Phase 2 — Android phone + SIM (planned)

An old Android phone running
[android-sms-gateway-server](https://github.com/RebekkaMa/android-sms-gateway-server)
exposes a REST API on your LAN.  The phone uses its own SIM to send real SMS
directly through the carrier — no third party ever sees the message content.

**Privacy profile:** identical to sending from your own phone.  Only the
carrier and recipient carrier are involved.

**macOS Docker Desktop compatibility:** full.  No USB passthrough required —
the Android phone communicates over WiFi/LAN.

See [docs/telnyx-setup.md](../../docs/telnyx-setup.md) for the swap
instructions.

---

## Configuration

All configuration is via environment variables:

| Variable | Required | Example | Description |
|---|---|---|---|
| `KAFKA_BROKERS` | yes | `kafka:9092` | Comma-separated broker list |
| `TELNYX_API_KEY` | yes | `KEY...` | Telnyx API v2 key |
| `TELNYX_FROM_NUMBER` | yes | `+15550001234` | Your provisioned Telnyx number |

Copy `.env.example` to `.env` and fill in your values.

---

## Running locally

### Prerequisites

- Docker Desktop (macOS) or Docker Engine (Linux)
- The `agentic-network` Docker network must exist (created by the main stack)
- A Telnyx account and phone number — see [docs/telnyx-setup.md](../../docs/telnyx-setup.md)

### Start

```bash
cp .env.example .env
# edit .env with your Telnyx credentials

docker compose -f docker-compose.sms-sender.yml up -d
docker compose -f docker-compose.sms-sender.yml logs -f
```

### Send a hello world

```bash
docker exec -i agentic-kafka kafka-console-producer \
  --topic sms-outbox \
  --bootstrap-server localhost:9092 \
  <<< '{"id":"hello-1","to":"+1XXXXXXXXXX","body":"hello world from kafka"}'
```

Replace `+1XXXXXXXXXX` with your own number.  Watch the logs and your phone.

### Inspect the DLQ

```bash
docker exec -it agentic-kafka kafka-console-consumer \
  --topic sms-dlq \
  --from-beginning \
  --bootstrap-server localhost:9092
```

---

## File layout

```
cmd/sms-sender/
  main.go              entry point — env config, wiring, graceful shutdown

internal/sms/
  message.go           OutboundMessage struct + JSON schema docs
  sender.go            Sender interface + TelnyxSender implementation
  kafka.go             Consumer — reads sms-outbox, retries, DLQ routing

docker/sms-sender/
  Dockerfile           two-stage build: Go alpine → scratch (~8 MB image)

docker-compose.sms-sender.yml   compose file (joins agentic-network)
docs/telnyx-setup.md            Telnyx provisioning walkthrough
.env.example                    environment variable template
```

---

## Design decisions

**Pure Go Kafka client (`segmentio/kafka-go`)** over `confluent-kafka-go`.
confluent-kafka-go requires CGO and links against librdkafka, which makes
static builds and scratch Docker images impossible.  segmentio/kafka-go is
pure Go, produces a single static binary, and has no native dependencies.

**stdlib `net/http` for Telnyx** rather than any Telnyx SDK.  The API is a
simple POST with a Bearer token.  Adding an SDK for that would introduce
unnecessary transitive dependencies.

**`scratch` base image.**  The runtime container has no OS, no shell, no
package manager.  Attack surface is the binary and CA certificates only.
Image size is ~8 MB.

**Explicit offset commits.**  `kafka-go` is configured with `CommitInterval: 0`
so offsets are committed manually after each successful send.  Auto-commit
could silently advance past a failed message.

**DLQ over halt.**  A message that fails 3 times is forwarded to `sms-dlq`
rather than blocking the consumer indefinitely.  This keeps the pipeline
moving while preserving the failed record for manual inspection and replay.

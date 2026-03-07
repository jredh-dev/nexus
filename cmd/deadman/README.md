# deadman

SMS deadman switch. Text the Twilio number every 72 hours. Miss the window and it texts you a warning — 24 hours to reply before the final alert fires.

## Stop here if...
- You're looking for the Twilio number config — that's in your `.env` file, not here
- You're working on portal/secrets/hermit — see their respective directories

## What's here

- `main.go` — HTTP server, Twilio webhook handler, ticker loop, DB migrations
- `Dockerfile` — multi-stage build (Go builder → distroless runtime)

## How it works

```
[you text Twilio number]
        │
        ▼
POST /sms (Twilio webhook)
        │
        ▼
upsert last_checkin = NOW(), clear warn/final flags
        │
        ▼
ticker runs every 1 min:
  T+72h → send warning SMS ("24 hours left")
  T+96h → send final SMS  ("deadman triggered")
```

Any text from your registered number resets the clock. Content doesn't matter.

## Setup

### 1. Twilio console

1. Buy a number (you already have `+15706006135`)
2. Under that number → **Messaging** → change from "Messaging Service" to **Webhook**
3. Set webhook URL: `https://<your-host>/sms` (POST)
4. Save

### 2. Environment variables

Create/add to your `.env` (never commit this file):

```bash
TWILIO_ACCOUNT_SID=ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
TWILIO_AUTH_TOKEN=your_auth_token_here
TWILIO_FROM=+15706006135
DEADMAN_PHONE=+1XXXXXXXXXX        # your personal cell — texts must come FROM here
DEADMAN_DB_PASSWORD=change-me     # postgres password for the deadman container
```

### 3. Run with Docker

```bash
# From the agentic workspace root
docker compose up -d deadman

# Check it's alive
curl http://localhost:8095/health

# Check timer status
curl http://localhost:8095/status
```

### 4. Expose the webhook (local dev)

Twilio needs to reach `/sms` over the internet. Use ngrok:

```bash
ngrok http 8095
# Copy the https://xxxx.ngrok.io URL
# Set it in Twilio console: https://xxxx.ngrok.io/sms
```

For production, deploy to Cloud Run and point Twilio at the Cloud Run URL.

### 5. Test it

Text anything to `+15706006135` from your personal cell. You should get back:
> "Check-in received. Timer reset. Next check-in due in 72 hours."

Check the timer:
```bash
curl http://localhost:8095/status
# {"elapsed":"0s","final_sent":false,"last_checkin":"...","trigger_due_in":"96h0m0s","warn_due_in":"72h0m0s","warn_sent":false}
```

## Run / Build / Test

```bash
# Build binary locally
go build ./cmd/deadman

# Build Docker image
docker build -f cmd/deadman/Dockerfile -t nexus-deadman:local .

# Run standalone (no compose)
docker run --rm \
  -e TWILIO_ACCOUNT_SID=... \
  -e TWILIO_AUTH_TOKEN=... \
  -e TWILIO_FROM=+15706006135 \
  -e DEADMAN_PHONE=+1XXXXXXXXXX \
  -e DATABASE_URL="host=deadman-postgres dbname=deadman user=deadman password=..." \
  -p 8095:8095 \
  nexus-deadman:local
```

## Logs

```bash
docker compose logs -f deadman
```

Ticker emits a `DEBUG` line every minute. Warning and final alerts emit `WARN`.

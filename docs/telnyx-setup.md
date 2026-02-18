# Telnyx Setup Guide

This guide covers the one-time manual steps needed to provision a Telnyx phone
number and API key so the `sms-sender` service can deliver real SMS messages.

Telnyx was chosen as the initial backend because:

- Transparent privacy policy: no third-party data sales, no ad-targeting
- Very cheap: ~$1/month for a number + $0.004/SMS outbound
- Clean REST API, no SDK required
- Easily swappable: the `Sender` interface means you can replace Telnyx with an
  Android phone gateway (or any other backend) without touching the Kafka consumer

> **Long-term plan**: replace Telnyx with an Android phone running
> [android-sms-gateway-server](https://github.com/RebekkaMa/android-sms-gateway-server)
> + a cheap prepaid SIM.  That eliminates all third-party visibility into
> message content.  See the sms-sender README for the swap instructions.

---

## Steps

### 1. Create an account

Go to [telnyx.com](https://telnyx.com) and sign up.  Free trial credits are
available — enough to test the hello world without a credit card.

### 2. Buy a US phone number

1. Log in to the [Telnyx Portal](https://portal.telnyx.com)
2. Navigate to **Numbers → Search & Buy Numbers**
3. Search for a US number (any area code is fine for outbound-only)
4. Add to cart and purchase (~$1/month)
5. Note the number in **E.164 format**: `+15550001234`

### 3. Create a Messaging Profile

Telnyx requires numbers to be attached to a Messaging Profile before they can
send SMS.

1. Navigate to **Messaging → Messaging Profiles**
2. Click **Add new profile**
3. Give it a name (e.g. `nascent-nexus-dev`)
4. Save — you don't need webhooks for send-only
5. Go to **Numbers**, find your number, and assign it to this profile

### 4. Create an API Key

1. Navigate to **Auth → API Keys**
2. Click **Create API Key**
3. Name it `nascent-nexus-sms-sender`
4. Copy the key — it starts with `KEY...` and is shown only once

### 5. Configure your .env

```bash
cp .env.example .env
```

Edit `.env`:

```
TELNYX_API_KEY=KEY...your-key-here...
TELNYX_FROM_NUMBER=+15550001234
```

### 6. Test it

Start the sms-sender container:

```bash
docker compose -f docker-compose.sms-sender.yml up -d
docker compose -f docker-compose.sms-sender.yml logs -f
```

Publish a test message to Kafka (replace the `to` number with your own):

```bash
docker exec -i agentic-kafka kafka-console-producer \
  --topic sms-outbox \
  --bootstrap-server localhost:9092 \
  <<< '{"id":"hello-1","to":"+1XXXXXXXXXX","body":"hello world from kafka"}'
```

You should receive the text within a few seconds and see a log line like:

```
sms-sender: sent id=hello-1 to=+1XXXXXXXXXX (attempt 1)
```

---

## Swapping to Android Phone (Privacy Upgrade)

Once you have an old Android phone + cheap prepaid SIM:

1. Install [android-sms-gateway-server](https://github.com/RebekkaMa/android-sms-gateway-server) APK
2. Start the server on the phone — note its LAN IP (e.g. `192.168.1.50:9090`)
3. Implement `sms.Sender` for the Android REST API in `internal/sms/android_sender.go`:

```go
func (s *AndroidSender) Send(ctx context.Context, msg OutboundMessage) error {
    // POST http://192.168.1.50:9090/messages
    // Basic auth: admin / your-password
    // Body: {"phone_number": msg.To, "message": msg.Body}
}
```

4. Swap the sender in `cmd/sms-sender/main.go`:

```go
// before:
sender := sms.NewTelnyxSender(apiKey, fromNumber)

// after:
sender := sms.NewAndroidSender("http://192.168.1.50:9090", "admin", "password")
```

No changes needed to `internal/sms/kafka.go` or the Docker setup.

# nascent-nexus

**Personal AI assistant system** - SMS-based memory, scheduling, and service integration hub.

> "The mind that remembers, reminds, and coordinates everything."

## 🎯 Vision

nascent-nexus is a personal AI assistant accessible via SMS that acts as your "second brain":

- 📝 **Memory system** - Text your thoughts, schedules, and questions
- 🔗 **Service integration** - IFTTT/BetterCloud-like connector for all your apps
- 💬 **Community questions** - Reddit-style Q&A with your own crowd
- 🎙️ **Voice chat** - General lobby for conversations
- 🔐 **LLM-based auth** - Unique security questions generated per user
- 🤝 **Human-in-the-loop** - AI asks real humans for advice on sensitive topics
- ❓ **The Big Question** - Daily prompts with optional training data contribution (rewarded)

## 🚀 Current Status

**Phase 1**: SMS "hello world" - Text any message, get "world" back.

## ⚡ Quick Start

### Prerequisites

- Go 1.21+ installed
- ngrok account (free tier works)
- Twilio account (see setup below)

### 1. Build and Run Server

```bash
# Build
go build -o bin/nascent-nexus cmd/server/main.go

# Run
./bin/nascent-nexus
```

Server starts on `http://localhost:8080`

### 2. Expose via ngrok

In another terminal:

```bash
ngrok http 8080 --config=ngrok.yml
```

Copy the `https://` URL shown (e.g., `https://abc123.ngrok.io`)

### 3. Configure Twilio Webhook

1. Go to your Twilio phone number settings
2. Find "Messaging" → "A MESSAGE COMES IN"
3. Set webhook to: `https://your-ngrok-url.ngrok.io/sms`
4. Method: `HTTP POST`
5. Save

### 4. Test!

Text anything to your Twilio number. You should receive "world" back!

## 📱 Twilio Setup (One-Time)

### Create Account

1. Go to [twilio.com/try-twilio](https://www.twilio.com/try-twilio)
2. Sign up (requires email + phone verification)
3. Complete verification

### Get Phone Number

1. Navigate to "Phone Numbers" → "Manage" → "Buy a number"
2. Choose country (US recommended)
3. Select "SMS" capability
4. Purchase number (~$1-2/month)

### Get API Credentials

1. Go to Console Dashboard
2. Find "Account SID" and "Auth Token"
3. Save these (you'll need them later for programmatic SMS sending)

### Set Webhook

See **Quick Start** step 3 above.

### Costs

- **Phone number**: ~$1-2/month
- **Incoming SMS**: Free (on most numbers)
- **Outgoing SMS**: ~$0.0075 per message (US)
- **Trial account**: $15 credit (plenty for testing!)

## 🏗️ Architecture

```
nascent-nexus/
├── cmd/
│   └── server/          # HTTP server entry point
├── internal/
│   └── handlers/        # HTTP handlers (SMS webhook, health)
├── CONTEXT.md           # Development state tracking
├── CHANGELOG.md         # Release history
└── ngrok.yml            # ngrok configuration
```

**Tech Stack:**
- **Go** - Minimal footprint, native compilation
- **PostgreSQL** - (Coming soon) Multi-format data storage
- **Twilio** - SMS provider
- **ngrok** - Local development tunneling

## 📖 Development

### Run Tests

```bash
go test ./...
```

### Check Health

```bash
curl http://localhost:8080/health
# Response: OK
```

### Test SMS Webhook Locally

```bash
curl -X POST http://localhost:8080/sms \
  -d "From=%2B15555555555" \
  -d "Body=hello"
```

Expected response:
```xml
<?xml version="1.0" encoding="UTF-8"?>
<Response>
    <Message>world</Message>
</Response>
```

## 📚 Documentation

- **CONTEXT.md** - Development state, task tracking, session continuity
- **CHANGELOG.md** - Release notes and version history
- **AGENTS.md** (workspace root) - Development workflow standards

## 📜 License

AGPL-3.0 - See [LICENSE](LICENSE)

This ensures derivative works remain open source. Corporations can't take the code without contributing back.

## 🛣️ Roadmap

- [x] **Phase 1**: SMS "hello world"
- [ ] **Phase 2**: PostgreSQL integration + basic memory storage
- [ ] **Phase 3**: LLM integration (OpenAI/Anthropic) for intelligent responses
- [ ] **Phase 4**: Service integrations (calendar, email, etc.)
- [ ] **Phase 5**: Community features ("The Big Question", voice chat)
- [ ] **Phase 6**: Human-in-the-loop advisory system

## 🤝 Contributing

Not yet accepting contributions (private prototyping phase). Will open source after initial validation.

---

Built with ❤️ by jredh-dev

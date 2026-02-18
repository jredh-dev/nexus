# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `sms-sender` service: Kafka consumer that reads from `sms-outbox` topic and
  delivers outbound SMS via Telnyx REST API (`internal/sms` package)
- `sms.Sender` interface enabling pluggable SMS backends — swap Telnyx for an
  Android phone gateway without touching the Kafka consumer
- Dead-letter queue: failed messages routed to `sms-dlq` after 3 retries with
  exponential backoff so the consumer never halts on a bad message
- `docker/sms-sender/Dockerfile`: two-stage build producing a static `scratch`
  image (~8 MB) with no OS, shell, or package manager
- `docker-compose.sms-sender.yml`: joins existing `agentic-network` so the
  service reaches Kafka at `kafka:9092` by service-discovery hostname
- `.env.example`: environment variable template for Telnyx credentials
- `docs/telnyx-setup.md`: step-by-step Telnyx provisioning guide including
  instructions for upgrading to an Android phone backend
- `docs/sms-sender.md`: full design documentation for the sms-sender service

### Added (earlier)
- Initial project structure
- SMS webhook endpoint (responds "world" to any incoming message)
- CONTEXT.md for development state tracking
- CHANGELOG.md for release tracking
- AGPL-3.0 license

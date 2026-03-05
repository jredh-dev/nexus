# vn — Long-Term Goals

Living document. Updated as the project evolves.

---

## Vision

A visual novel engine that combines full-screen video backgrounds with text overlays,
branching narrative, and community-driven story progression. Personal art project first,
public release when it's ready.

---

## Platform

- **Phase 1**: Mobile-first responsive web (no frameworks, vanilla HTML/CSS/JS)
- **Phase 2**: Native apps (iOS via Swift/SwiftUI, Android TBD)
- **Phase 3**: Offline/PWA support for mobile web

## State Management

- Schema-free YAML persistence: JSON in → YAML on disk → JSON out
- Only requirement is an `id` field — the engine preserves whatever state it receives
- No concrete entity structs in the engine; schemas are defined by creative content
- YAML is for versioned entity state, not branching narrative scripts
- Git-backed version control for all state definitions
- Content is hand-written, never AI-generated

## Story Engine

- Branching narrative with YAML-defined story graphs
- Git-backed version control for all story content
- Hot-reloadable story definitions (fsnotify, no server restart)
- Chapter-based progressive loading (don't download the whole story upfront)
- Multiple concurrent storylines with shared world events
- Content is hand-written, never AI-generated

## Video & Media

- Full-screen video backgrounds behind text overlays
- Palindrome (racecar) loop generation via FFmpeg (server-side, seamless loops)
- Adaptive streaming (quality tiers based on connection/device)
- HTTP range-request streaming for large video files
- PostgreSQL large object storage (no filesystem, no object store)

## Subtitle & Text System

- Toggle-point visibility model (initialize_visible / end_visible / timestamps)
- Client-side subtitle renderer synced to video playback
- Text effects, transitions, typography (the text IS the experience)

## Community & Voting

- Hybrid model: some shared world events that affect everyone, some individual paths
- Fake currency system for voting weight
- Community votes influence story direction at branch points
- Per-chapter vote aggregation with results visibility

## Economy

- Ethereum / crypto integration for the token economy
- On-chain voting records (transparent, verifiable)
- Token-gated content or voting tiers (explore, don't commit yet)
- Fake currency as the starting point; crypto layer added when the mechanics are proven

## Infrastructure

- Local-first development (Docker Compose with dedicated PostgreSQL)
- Cloud deployment deferred until database provisioning is sorted (Cloud SQL or equivalent)
- Self-contained — no external dependencies beyond PostgreSQL and FFmpeg
- CI/CD with Discord notifications on all deploy events

## Quality

- Integration tests with real PostgreSQL (Docker-based test lifecycle)
- Unit tests for engine, subtitle visibility, storyrepo
- Per-service test runner via servicectl
- Heavy commenting — code explains intent, not just behavior

## Build Order

Phased implementation, backend-first:

1. **Database + engine + story** — schema, migrations, story graph, hot-reload, voting ✅
2. **State management** — schema-free YAML persistence, JSON↔YAML round-trip ✅
3. **Video processing** — FFmpeg palindrome loop generation, adaptive transcoding, HTTP streaming
4. **Subtitle visibility engine** — toggle-point model implementation, client sync
5. **Web client** — mobile-first HTML/CSS/JS, video player, subtitle renderer

## Not Goals (at least not yet)

- Multiplayer real-time chat or social features
- User-generated content / modding
- AI-generated narrative content
- Mobile native before web is solid

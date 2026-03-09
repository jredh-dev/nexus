# Build stage — compile the binary inside the Go toolchain image.
FROM golang:1.24-alpine AS builder

WORKDIR /src

# Copy dependency manifests first for layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy full source and build only the deadman binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /deadman ./cmd/deadman

# Runtime stage — debian:slim for shell support.
#
# distroless/static has no shell, so we cannot source secrets.env from the
# Vault Agent sidecar before exec. debian:slim adds ~30 MB but gives us /bin/sh
# for the entrypoint wrapper — a worthwhile trade for the Vault injection pattern.
FROM debian:bookworm-slim

# Install ca-certificates for outbound TLS (Twilio API calls).
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /deadman /deadman

# entrypoint.sh — sources Vault-rendered secrets before exec.
#
# Vault Agent sidecar writes /vault/secrets/secrets.env on the shared tmpfs.
# If the file exists, all KEY=VALUE lines are exported into the environment
# before the deadman binary is exec'd. This keeps secrets out of the image,
# docker-compose env blocks, and process listings.
COPY cmd/deadman/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Expose the default port (override with PORT env var).
EXPOSE 8095

ENTRYPOINT ["/entrypoint.sh"]
# Default command: start the HTTP server.
# Without this, Cloud Run passes no arguments → printUsage → exit(0) → health check fails.
CMD ["serve"]

# syntax=docker/dockerfile:1

# ─── build stage ──────────────────────────────────────────────────────────────
# Use the official Go image to compile a fully static binary.  CGO is disabled
# because segmentio/kafka-go is pure Go (no librdkafka), so we can produce a
# zero-dependency binary that runs on scratch.
FROM golang:1.24-alpine AS builder

WORKDIR /src

# Copy dependency manifests first so Docker can cache this layer separately
# from the application source.  A change to *.go files won't invalidate the
# module download layer.
COPY go.mod go.sum ./
RUN go mod download

# Copy the full source tree and build only the sms-sender binary.
# CGO_ENABLED=0 + GOOS=linux produces a portable static binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/sms-sender ./cmd/sms-sender

# ─── runtime stage ────────────────────────────────────────────────────────────
# scratch is the smallest possible base image: no shell, no package manager,
# no OS libraries.  The binary is the entire container filesystem.
# This minimises the attack surface and image size (~8 MB vs ~300 MB for alpine).
FROM scratch

# Copy CA certificates so the binary can verify TLS when calling api.telnyx.com.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /out/sms-sender /sms-sender

ENTRYPOINT ["/sms-sender"]

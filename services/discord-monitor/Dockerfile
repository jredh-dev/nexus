# Build stage
# Build context is the repo root (docker build -f services/discord-monitor/Dockerfile .)
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /app/server \
    ./services/discord-monitor/cmd/server

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder /app/server /app/server

RUN chown -R appuser:appuser /app

USER appuser

EXPOSE 8080

# DATABASE_URL must be provided at runtime (TCP connection to PostgreSQL).
# DISCORD_SELFBOT_TOKEN is optional — omit for API-only mode.
ENV PORT=8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/server"]

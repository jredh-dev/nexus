# Build stage
# Build context is the repo root (docker build -f services/mcp/discord/Dockerfile .)
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /app/discord-mcp \
    ./services/mcp/discord

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder /app/discord-mcp /app/discord-mcp

RUN chown -R appuser:appuser /app

USER appuser

EXPOSE 8092

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8092/health || exit 1

ENTRYPOINT ["/app/discord-mcp"]

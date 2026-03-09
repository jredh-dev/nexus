# Build stage
# Build context is the repo root (docker build -f services/humanish/Dockerfile .)
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /app/humanish \
    ./services/humanish/cmd/server

# Runtime stage
FROM alpine:3.19

# git is required for the shadow repo operations
RUN apk --no-cache add ca-certificates git

RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder /app/humanish /app/humanish

# entrypoint.sh: sources Vault-injected secrets before launching.
# OPENCODE_PASSWORD is written by vault-agent-opencode into
# /vault/secrets/humanish.env as:  OPENCODE_PASSWORD=<value>
COPY services/humanish/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh && chown -R appuser:appuser /app

USER appuser

# HUMANISH_DIR is the bind-mounted humanish volume (e.g. /humanish)
ENV HUMANISH_DIR=/humanish
ENV OPENCODE_URL=http://opencode:4096
ENV HUMANISH_QUIET_SECONDS=5
ENV HUMANISH_MAX_QUIET_SEC=300
ENV HUMANISH_BRANCH=on-demand

ENTRYPOINT ["/app/entrypoint.sh"]

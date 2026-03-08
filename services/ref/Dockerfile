# Build stage
# Build context is the repo root (docker build -f services/ref/Dockerfile .)
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
    -o /app/server \
    ./services/ref/cmd/server

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates wget

RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder /app/server /app/server

RUN chown -R appuser:appuser /app

USER appuser

EXPOSE 8086

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8086/health || exit 1

ENTRYPOINT ["/app/server"]

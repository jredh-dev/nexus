# Build stage
# Build context is the repo root (docker build -f services/vn/Dockerfile .)
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
    ./services/vn/cmd/server

# Runtime stage
FROM alpine:3.19

# ffmpeg for palindrome video generation; ca-certificates for HTTPS.
RUN apk --no-cache add ca-certificates ffmpeg

RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder /app/server /app/server

# Copy the seed story so the container can start without an external volume.
# In production, mount a volume at /app/stories to override.
COPY services/vn/stories/ /app/stories/

RUN chown -R appuser:appuser /app

USER appuser

EXPOSE 8080

# DATABASE_URL must be provided at runtime (TCP connection to PostgreSQL).
# STORY_DIR defaults to /app/stories (baked-in seed story).
ENV PORT=8080 \
    STORY_DIR=/app/stories \
    HOT_RELOAD=false

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/server"]

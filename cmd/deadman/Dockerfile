# Build stage — compile the binary inside the Go toolchain image.
FROM golang:1.24-alpine AS builder

WORKDIR /src

# Copy dependency manifests first for layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy full source and build only the deadman binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /deadman ./cmd/deadman

# Runtime stage — minimal image, no shell, no toolchain.
FROM gcr.io/distroless/static-debian12

COPY --from=builder /deadman /deadman

# Expose the default port (override with PORT env var).
EXPOSE 8095

ENTRYPOINT ["/deadman"]

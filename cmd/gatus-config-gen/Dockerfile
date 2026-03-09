FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /app/gatus-config-gen \
    ./cmd/gatus-config-gen

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/gatus-config-gen /usr/local/bin/gatus-config-gen

# Default: write to /vault/secrets/config.yaml every 60 seconds.
ENV OUT=/vault/secrets/config.yaml
ENV GATUS_INTERVAL=60s

CMD ["/bin/sh", "-c", "gatus-config-gen --out $OUT --interval $GATUS_INTERVAL"]

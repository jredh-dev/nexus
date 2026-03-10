# Build stage
# Build context is the repo root (docker build -f services/mcp/gcloud/Dockerfile .)
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
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /app/gcloud-mcp \
    ./services/mcp/gcloud

# Runtime stage
FROM alpine:3.19

ARG TERRAFORM_VERSION=1.7.5

RUN apk --no-cache add ca-certificates wget unzip

RUN wget -q https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip \
    && unzip terraform_${TERRAFORM_VERSION}_linux_amd64.zip \
    && mv terraform /usr/local/bin/terraform \
    && rm terraform_${TERRAFORM_VERSION}_linux_amd64.zip

RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder /app/gcloud-mcp /app/gcloud-mcp

RUN chown -R appuser:appuser /app

USER appuser

EXPOSE 8093

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8093/health || exit 1

ENTRYPOINT ["/app/gcloud-mcp"]

# SPDX-License-Identifier: AGPL-3.0-or-later

# Build stage
FROM rust:1.85-slim-bookworm AS builder

RUN apt-get update && apt-get install -y protobuf-compiler && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Cache dependencies by building with empty src first
COPY Cargo.toml Cargo.lock ./
COPY proto/ proto/
COPY build.rs ./
RUN mkdir src && echo 'fn main() {}' > src/main.rs
RUN cargo build --release 2>/dev/null || true
RUN rm -rf src

# Build real binary
COPY src/ src/
RUN cargo build --release

# Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

RUN groupadd -g 1000 appuser && useradd -u 1000 -g appuser -m appuser

COPY --from=builder /app/target/release/hermit-server /usr/local/bin/hermit-server

USER appuser

# Cloud Run sends traffic to PORT env var (default 8080)
# hermit uses --grpc-port flag, --no-tls for plaintext h2c (Cloud Run terminates TLS)
ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["hermit-server"]
CMD ["--grpc-port", "8080", "--no-tls"]

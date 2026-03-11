FROM golang:1.24-alpine AS builder
WORKDIR /src
# Copy module files first for layer-cached dependency download.
COPY go.mod go.sum ./
RUN go mod download
# Copy the full repo so the build can resolve internal/ imports.
COPY . .
RUN CGO_ENABLED=0 go build -o /realtime ./services/realtime/cmd/

FROM scratch
COPY --from=builder /realtime /realtime
ENTRYPOINT ["/realtime"]

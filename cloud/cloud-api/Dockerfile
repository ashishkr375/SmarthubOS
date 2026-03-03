# ──────────────────────────────────────────────────────────
# Stage 1: Build
# ──────────────────────────────────────────────────────────
FROM golang:1.21-alpine AS builder

# Install ca-certificates so they can be copied to the scratch image
RUN apk --no-cache add ca-certificates

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a static binary. CGO disabled, debug symbols stripped.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o cloud-api ./...

# ──────────────────────────────────────────────────────────
# Stage 2: Final image — FROM scratch (no shell, no OS)
# ──────────────────────────────────────────────────────────
FROM scratch

# Needed for HTTPS calls to Grafana and Let's Encrypt from inside the container
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /build/cloud-api /cloud-api

EXPOSE 9090

ENTRYPOINT ["/cloud-api"]

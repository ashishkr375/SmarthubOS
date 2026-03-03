# Implementation Plan: Cloud API

**Branch**: `001-cloud-api` | **Date**: 2026-03-03 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `specs/001-cloud-api/spec.md`

## Summary

Build the SmartHubOS Cloud API — a Go HTTP/WebSocket server deployed on the VPS as a
Docker container. It exposes the full REST surface for tenant, hub, device, telemetry,
and rule management; an internal hub API for Pi bridge authentication, telemetry ingest,
cache-sync, and heartbeat; and a persistent per-hub WebSocket channel for real-time
command delivery and device revocation. JWTs authenticate user-facing endpoints; hub
API keys (bcrypt-verified) authenticate the Pi. All canonical data lives in PostgreSQL 16
+ TimescaleDB on the same VPS network.

## Technical Context

**Language/Version**: Go 1.21+
**Primary Dependencies**:
- `net/http` stdlib router (or `chi` v5 for grouping + middleware)
- `gorilla/websocket` v1 — per-hub WebSocket hub manager
- `golang-jwt/jwt` v5 — HS256 JWT sign/verify
- `golang.org/x/crypto/bcrypt` — device token and hub API key hashing (cost = 12)
- `jackc/pgx` v5 — PostgreSQL driver (pgxpool for connection pooling)
- `golang-migrate/migrate` v4 — versioned SQL migrations
- `go.uber.org/zap` — structured JSON logging
- Grafana HTTP API (internal HTTP calls to `http://grafana:3000`)

**Storage**: PostgreSQL 16 + TimescaleDB (same Docker network, `postgres:5432`)
**Testing**: `go test -race -cover ./...`; `testcontainers-go` for integration tests against real PostgreSQL
**Target Platform**: Linux/amd64 Docker container on VPS (Ubuntu 22.04)
**Project Type**: web-service
**Performance Goals**: p95 < 200 ms for CRUD; batch ingest of 100 events < 500 ms p95; WebSocket command delivery < 1 s
**Constraints**: Docker image < 20 MB (`FROM scratch`); no CGO; single statically-linked binary; all secrets via env vars
**Scale/Scope**: MVP — single VPS, dozens of hubs, hundreds of devices per hub

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] No main PostgreSQL or Grafana proposed for the Pi — Cloud API runs on VPS only
- [x] `tenant_id` resolved from validated JWT claim — enforced by auth middleware; never from request body
- [x] `hub_id` resolved from hub API key credential — enforced in hub middleware; never from request field
- [x] NATS `TELEMETRY` stream configured with file storage — N/A (Cloud API does not touch NATS)
- [x] All adapters normalize to Standard Event Schema before publishing to NATS — N/A (ingest endpoint *receives* Standard Schema events from the Pi; does not publish to NATS)
- [x] New protocol = new container only — N/A (Cloud API has no protocol-specific logic)
- [x] Edge images use `FROM scratch` — Cloud API image also uses `FROM scratch` (CGO_ENABLED=0, GOOS=linux, static binary)
- [x] Pi auth API bound to `127.0.0.1:8080` — N/A (this is the VPS-side service)
- [x] No credentials in code or committed `.env` files — all secrets via env vars: `JWT_SECRET`, `POSTGRES_URL`, `GRAFANA_URL`, `GRAFANA_ADMIN_USER`, `GRAFANA_ADMIN_PASSWORD`
- [x] bcrypt cost ≥ 12 — enforced for device tokens (FR-005) and hub API keys (FR-006)
- [x] NATS consumers use `AckExplicit` — N/A
- [x] Cloud Bridge uses exponential backoff — N/A (Cloud API is the server)

**Result: ALL GATES PASS ✅**

## Project Structure

### Documentation (this feature)

```text
specs/001-cloud-api/
├── plan.md          ← this file
├── research.md      ← Phase 0 output
├── data-model.md    ← Phase 1 output
├── quickstart.md    ← Phase 1 output
├── contracts/
│   └── openapi.yaml ← Phase 1 output
└── tasks.md         ← Phase 2 output (/speckit.tasks — NOT created here)
```

### Source Code (repository root)

```text
cloud/
└── cloud-api/
    ├── main.go                  ← entry point: load config, wire deps, start server
    ├── config/
    │   └── config.go            ← env var loading (PORT, POSTGRES_URL, JWT_SECRET, …)
    ├── db/
    │   ├── db.go                ← pgxpool setup, Ping health check
    │   └── queries/             ← typed query functions (no ORM)
    │       ├── tenants.go
    │       ├── hubs.go
    │       ├── devices.go
    │       ├── telemetry.go
    │       └── rules.go
    ├── auth/
    │   ├── jwt.go               ← sign/parse HS256 tokens
    │   ├── bcrypt.go            ← hash + compare (cost=12 enforced)
    │   └── middleware.go        ← JWT middleware, hub API key middleware
    ├── handlers/
    │   ├── auth.go              ← POST /api/v1/auth/login, /refresh
    │   ├── tenants.go
    │   ├── hubs.go
    │   ├── devices.go
    │   ├── telemetry.go
    │   ├── rules.go
    │   └── health.go            ← GET /health
    ├── hub/
    │   ├── manager.go           ← per-hub WebSocket connection registry
    │   └── ws.go                ← GET /hub/ws upgrade + read/write pump
    ├── internal/
    │   ├── ingest.go            ← POST /internal/hub/ingest
    │   ├── cachesync.go         ← GET /internal/hub/cache-sync
    │   └── heartbeat.go         ← POST /internal/hub/heartbeat
    ├── grafana/
    │   └── client.go            ← Grafana HTTP API: create org, create user
    └── Dockerfile               ← multi-stage: golang:1.21-alpine builder → FROM scratch

cloud/migrations/
    ├── 001_initial_schema.up.sql
    ├── 001_initial_schema.down.sql
    ├── 002_timescale_hypertable.up.sql
    └── 002_timescale_hypertable.down.sql

deploy/cloud/
    ├── docker-compose.yml       ← already exists; cloud-api service to be added
    └── .env.example             ← PORT, POSTGRES_URL, JWT_SECRET, GRAFANA_* vars
```

**Structure Decision**: Standard Go flat-package layout under `cloud/cloud-api/`. No
monorepo module nesting — one `go.mod` at `cloud/cloud-api/go.mod`. Migrations live
in `cloud/migrations/` separate from the binary so they can be applied independently
by CI. Tests live alongside source files (`_test.go`) with integration tests in
`cloud/cloud-api/integration/` using `testcontainers-go`.

## Post-Design Constitution Check

*Re-evaluated after Phase 1 artifacts: research.md, data-model.md, contracts/openapi.yaml, quickstart.md.*

- [x] **`tenant_id` from JWT claims** — `CacheSyncDevice` response in openapi.yaml confirms `token_hash` is only returned via `/internal/hub/cache-sync` (hub auth), never the public API. All handler descriptions state claim resolution.
- [x] **`hub_id` from API key** — `/internal/hub/ingest` schema validates that `hub_id` inside each `StandardEvent` MUST match the authenticated hub's ID (403 if mismatch). Documented in openapi.yaml.
- [x] **bcrypt cost = 12** — `auth/bcrypt.go` wrapper confirmed in research.md Decision 7. Cost is hardcoded, no caller can pass different value.
- [x] **`FROM scratch`** — Dockerfile decision confirmed in research.md Decision 9. `ca-certificates.crt` copied for HTTPS to Grafana.
- [x] **No partial writes on ingest** — openapi.yaml `/internal/hub/ingest` explicitly documents single-transaction rejection. data-model.md §migration confirms telemetry table is in migration 002.
- [x] **No cross-tenant data leak** — `/internal/hub/cache-sync` contract explicitly states "Cross-tenant data is never returned." SC-004 acceptance test verifies two tenants + two hubs with zero cross-tenant leakage.
- [x] **Refresh token rotation** — data-model.md refresh_tokens entity documents that on rotation the old row is revoked (revoked_at = NOW()) and a new row is inserted. No token reuse.
- [x] **Rate limiting in-process** — research.md Decision 8 confirms `golang.org/x/time/rate` token bucket per `device_id` in `sync.Map`. No Redis introduced.

**Result: ALL POST-DESIGN GATES PASS ✅**

## Complexity Tracking

> No Constitution Check violations — no entries required.

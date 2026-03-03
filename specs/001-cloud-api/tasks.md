# Tasks: Cloud API (001-cloud-api)

**Branch**: `001-cloud-api`
**Input**: [spec.md](spec.md) · [plan.md](plan.md) · [data-model.md](data-model.md) · [contracts/openapi.yaml](contracts/openapi.yaml) · [research.md](research.md)
**Tech stack**: Go 1.21+, chi v5, gorilla/websocket v1, golang-jwt/jwt v5, pgx v5, golang-migrate v4, zap, testcontainers-go
**Generated**: 2026-03-03

---

## Format

- `[P]` — Parallelizable (different files, no pending dependencies)
- `[US#]` — User story label (required for Phase 3+)
- All tasks include exact file paths

---

## Phase 1: Setup

**Purpose**: Initialize the Go module, project structure, Docker build, and environment config.

- [X] T001 Initialize Go module with all dependencies in `cloud/cloud-api/go.mod` (`chi/v5`, `gorilla/websocket`, `golang-jwt/jwt/v5`, `pgx/v5`, `golang-migrate/migrate/v4`, `zap`, `testcontainers-go`, `golang.org/x/crypto`, `golang.org/x/time`)
- [X] T002 [P] Scaffold source directories matching plan.md tree under `cloud/cloud-api/` (`config/`, `db/queries/`, `auth/`, `handlers/`, `hub/`, `internal/`, `grafana/`, `integration/`)
- [X] T003 [P] Create multi-stage Dockerfile (`golang:1.21-alpine` builder → `FROM scratch` + `ca-certificates.crt`) at `cloud/cloud-api/Dockerfile`
- [X] T004 [P] Create `deploy/cloud/.env.example` with all required env vars (`DATABASE_URL`, `JWT_SECRET`, `PORT`, `AUTO_MIGRATE`, `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `GRAFANA_URL`, `GRAFANA_ADMIN_USER`, `GRAFANA_ADMIN_PASS`)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: DB migrations, configuration, connection pool, auth primitives, and router — MUST be complete before any user story work begins.

**⚠️ CRITICAL**: All Phase 3–6 tasks depend on this phase being complete.

- [X] T005 Write `cloud/migrations/001_initial_schema.up.sql` — `tenants`, `hubs`, `devices`, `rules`, `users`, `refresh_tokens` tables with all constraints and indexes per data-model.md
- [X] T006 [P] Write `cloud/migrations/001_initial_schema.down.sql` — DROP all tables from T005 in reverse dependency order
- [X] T007 Write `cloud/migrations/002_timescale_hypertable.up.sql` — `telemetry` table, `create_hypertable('telemetry','time')`, retention policy (90 days), continuous aggregate `telemetry_hourly` per data-model.md
- [X] T008 [P] Write `cloud/migrations/002_timescale_hypertable.down.sql` — DROP continuous aggregate and hypertable
- [X] T009 Implement `cloud/cloud-api/config/config.go` — load all env vars with defaults (`PORT=9090`, `AUTO_MIGRATE=false`), fail fast on missing required vars (`DATABASE_URL`, `JWT_SECRET`, `ADMIN_EMAIL`, `ADMIN_PASSWORD`)
- [X] T010 Implement `cloud/cloud-api/db/db.go` — `pgxpool.New`, `Ping` health check, optional `AutoMigrate` via `golang-migrate` on startup when `AUTO_MIGRATE=true`
- [X] T011 [P] Implement `cloud/cloud-api/auth/bcrypt.go` — `Hash(password string) (string, error)` and `Compare(hash, password string) bool` with `bcrypt.MinCost` hardcoded to 12; no caller can pass a different cost
- [X] T012 [P] Implement `cloud/cloud-api/auth/jwt.go` — `SignAccessToken(sub, tenantID, role string) (string, error)` (15 min), `ParseAccessToken(token string) (*Claims, error)` with `jwt.WithValidMethods([]string{"HS256"})` enforced; `Claims` struct with `Sub`, `TenantID`, `Role`
- [X] T013 Implement `cloud/cloud-api/auth/middleware.go` — `JWTMiddleware` (validate HS256 token, inject `Claims` into `ctx`), `HubAPIKeyMiddleware` (bcrypt-compare raw key against `hubs.api_key_hash`, inject resolved `Hub` into `ctx`); return `{"error":"unauthorized"}` on failure
- [X] T014 [P] Implement `cloud/cloud-api/db/queries/auth.go` — `GetUserByEmail`, `InsertRefreshToken`, `GetNonRevokedRefreshTokensByUserID`, `RevokeRefreshToken(id UUID)`; used by login and refresh endpoints
- [X] T015 Implement `cloud/cloud-api/main.go` — load config, connect DB, construct chi router with route groups: unauthenticated (`/api/v1/auth/...`), JWT-protected (`/api/v1/...` with `JWTMiddleware`), hub-key-protected (`/internal/hub/...` and `/hub/ws` with `HubAPIKeyMiddleware`); register all handler mounts; start server on configured port

**Checkpoint**: DB migrations, pgxpool, bcrypt, JWT, middleware, and chi router are wired. Server starts and `/health` returns 200.

---

## Phase 3: User Story 1 — Admin Provisions Tenant, Hub, and Device (Priority: P1) 🎯 MVP

**Goal**: Full provisioning flow: login → create tenant (+ Grafana org) → register hub (bcrypt API key) → provision device (bcrypt token). All records queryable via GET endpoints.

**Independent Test**: `docker compose up -d`, run curl sequence from `quickstart.md` §6. Verify records in `psql`. No Pi needed.

- [X] T016 [P] [US1] Implement `cloud/cloud-api/db/queries/tenants.go` — `InsertTenant`, `GetTenantByID`, `GetTenantByNamespace`, `ListTenants`
- [X] T017 [P] [US1] Implement `cloud/cloud-api/db/queries/hubs.go` — `InsertHub`, `GetHubByID`, `GetHubByAPIKeyHash` (returns hub for middleware lookup), `ListHubs`
- [X] T018 [P] [US1] Implement `cloud/cloud-api/db/queries/devices.go` — `InsertDevice`, `GetDeviceByID`, `ListDevicesByTenantID`, `UpdateDeviceStatus`; NOTE: does NOT include `token_hash` in `ListDevicesByTenantID` response (public API, FR-005)
- [X] T019 [P] [US1] Implement `cloud/cloud-api/grafana/client.go` — `CreateOrg(namespace string) (int64, error)` (`POST /api/orgs`), `AddAdminToOrg(orgID int64, user, pass string) error` (`POST /api/orgs/{id}/users`); skip if `GRAFANA_URL` is empty; handle 409 idempotently
- [X] T020 [P] [US1] Implement `cloud/cloud-api/handlers/health.go` — `GET /health`: ping DB, return `{"status":"ok","db":"up"}` (200) or `{"status":"degraded","db":"down"}` (503) per FR-013
- [X] T021 [US1] Implement `cloud/cloud-api/handlers/auth.go` — `POST /api/v1/auth/login`: load user by email, `bcrypt.Compare` password, `jwt.SignAccessToken`, generate 32-byte random refresh token, `bcrypt.Hash` it, `InsertRefreshToken`, return both tokens (FR-002)
- [X] T022 [US1] Implement `cloud/cloud-api/handlers/tenants.go` — `GET /api/v1/tenants` (admin), `POST /api/v1/tenants` (admin, calls `grafana.CreateOrg`, returns 409 on namespace conflict), `GET /api/v1/tenants/{id}`; `tenant_id` from JWT claim (FR-003)
- [X] T023 [US1] Implement `cloud/cloud-api/handlers/hubs.go` — `GET /api/v1/hubs` (admin), `POST /api/v1/hubs/register` (admin): generate 32-byte random API key via `crypto/rand`, `bcrypt.Hash` it, `InsertHub`, return `hub_id` + plaintext `api_key` once (FR-006)
- [X] T024 [US1] Implement `cloud/cloud-api/handlers/devices.go` — `GET /api/v1/devices` (JWT, tenant from claim), `POST /api/v1/devices`: generate 32-byte random token, `bcrypt.Hash` it, `InsertDevice`, return `device_id` + plaintext `token` once; validate `protocol` enum; never return `token_hash` (FR-005)

**Checkpoint**: Login → create tenant → register hub → provision device → `GET /api/v1/devices` all work. Records verified in `psql`. Grafana org visible in Grafana UI.

---

## Phase 4: User Story 2 — Hub Telemetry Ingest and WebSocket Commands (Priority: P1)

**Goal**: Hub authenticates with API key, POSTs telemetry batch to TimescaleDB. Persistent WebSocket delivers commands and revocation messages to the hub.

**Independent Test**: Use `websocat` or Go test client to open `/hub/ws`. POST 100 events to `/internal/hub/ingest`. Verify rows in `telemetry` table. Push `POST /api/v1/devices/{id}/command` and verify WebSocket receives it within 1 second.

- [X] T025 [P] [US2] Implement `cloud/cloud-api/db/queries/telemetry.go` — `BatchInsertTelemetry(events []StandardEvent) error` using `pgx.Batch` pipeline for single-transaction bulk INSERT into `telemetry` hypertable; reject entire batch on any validation failure (FR-007)
- [X] T026 [US2] Implement `cloud/cloud-api/hub/manager.go` — `HubManager` with `sync.Map[hubID → *HubConn]`; `Register(hubID, conn)`, `Unregister(hubID)`, `SendCommand(hubID, msg) error`; pending-command buffer (`map[hubID][]bufferedCmd`) with 5-minute TTL per CONSTITUTION.md §10.1C; drain buffer on hub reconnect
- [X] T027 [US2] Implement `cloud/cloud-api/hub/ws.go` — `GET /hub/ws`: upgrade HTTP to WebSocket (gorilla), authenticate via hub API key from `Authorization` header, call `manager.Register`, start read pump (ping/pong keepalive) and write pump as separate goroutines; call `manager.Unregister` on disconnect
- [X] T028 [US2] Implement `cloud/cloud-api/internal/ingest.go` — `POST /internal/hub/ingest`: decode up to 100 `StandardEvent` objects, validate all fields (return `400` if any event invalid — no partial writes), verify `hub_id` in each event matches authenticated hub (return `403` on mismatch), call `BatchInsertTelemetry` (FR-007)
- [X] T029 [US2] Implement `cloud/cloud-api/internal/heartbeat.go` — `POST /internal/hub/heartbeat`: update `hubs.is_online = true`, `hubs.last_ping = NOW()` for authenticated hub using a single UPDATE query (FR spec US2 scenario 4)
- [X] T030 [US2] Extend `cloud/cloud-api/handlers/devices.go` — add `DELETE /api/v1/devices/{id}`: atomically set `status = 'revoked'` in DB + call `manager.SendCommand` with `{"type":"revoke","device_id":"…"}`; idempotent on already-revoked (FR-009); add `POST /api/v1/devices/{id}/command`: look up device's hub, call `manager.SendCommand`, return `{"status":"delivered"}` or `{"status":"buffered_hub_offline"}` (FR-008)

**Checkpoint**: Hub WebSocket connects, heartbeat updates online status, 100-event batch inserts in < 500 ms, command delivered to hub WS within 1 second, device revoke pushes WS message.

---

## Phase 5: User Story 3 — Cache Sync Returns Tenant-Scoped Devices and Rules (Priority: P1)

**Goal**: `GET /internal/hub/cache-sync` returns only active devices + active rules for the hub's tenant. Zero cross-tenant leakage. Pi can populate its SQLite auth cache.

**Independent Test**: Register two tenants, two hubs, 3+1 devices for hub-A (3 active, 1 revoked). Call cache-sync with hub-A key. Verify only 3 active devices returned. Verify hub-B data absent (SC-004).

- [X] T031 [P] [US3] Implement `cloud/cloud-api/db/queries/rules.go` — `InsertRule`, `GetRulesByTenantID` (active only), `UpdateRule`, `DeleteRule`; all queries scope by `tenant_id`
- [X] T032 [P] [US3] Extend `cloud/cloud-api/db/queries/devices.go` — add `GetActiveDevicesForCacheSync(tenantID UUID) ([]CacheSyncDevice, error)`: SELECT `device_id`, `tenant_id`, `token_hash`, `status` WHERE `status = 'active'` AND `tenant_id = $1`; `token_hash` IS included (Pi needs it for bcrypt offline auth; NOT exposed by public API)
- [X] T033 [US3] Implement `cloud/cloud-api/internal/cachesync.go` — `GET /internal/hub/cache-sync`: resolve `tenant_id` from authenticated hub (via `devices` or a hub→tenant relationship lookup), call `GetActiveDevicesForCacheSync`, call `GetRulesByTenantID`, assemble and return `{"devices":[…],"rules":[…],"synced_at":"…"}` (FR-010, SC-004)
- [X] T034 [P] [US3] Implement `cloud/cloud-api/handlers/rules.go` — `GET /api/v1/rules`, `POST /api/v1/rules`, `PUT /api/v1/rules/{id}`, `DELETE /api/v1/rules/{id}`; all scoped to `tenant_id` from JWT claim (FR-003)
- [X] T035 [P] [US3] Implement `cloud/cloud-api/handlers/telemetry.go` — `GET /api/v1/telemetry`: query `telemetry` table with `tenant_id` from JWT, optional `device_id` filter, `from`/`to` time range (default last 24 h), `limit` (max 10000); scoped to authenticated tenant

**Checkpoint**: Cache-sync returns correct tenant-scoped data. Two-tenant cross-leak verified absent. Rules CRUD works. Telemetry query returns filtered rows.

---

## Phase 6: User Story 4 — JWT Refresh and Revocation (Priority: P2)

**Goal**: Refresh token rotation — present valid refresh token, get new access + refresh token pair. Revoked or expired tokens return 401.

**Independent Test**: Login, use long-lived refresh token to call `/auth/refresh`. Verify new access token returned and old refresh token is now revoked (next call with old token → 401).

- [X] T036 [US4] Extend `cloud/cloud-api/handlers/auth.go` — add `POST /api/v1/auth/refresh`: decode `refresh_token` from body, call `GetNonRevokedRefreshTokensByUserID`, bcrypt-compare against each valid row, on match: call `RevokeRefreshToken(matchedID)`, generate new access token + new refresh token, `InsertRefreshToken`, return both; return `{"error":"unauthorized"}` on expired/revoked/not-found (FR spec US4)
- [X] T037 [US4] Update `cloud/cloud-api/auth/middleware.go` — add expired-token handler: when JWT middleware detects an expired token (vs. invalid), return `{"error":"token_expired"}` specifically (US4 scenario 3)

**Checkpoint**: Full token lifecycle: login → protected endpoint → refresh → protected endpoint works. Revoked token returns 401.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Rate limiting, graceful shutdown, background jobs, integration tests, Docker image validation.

- [X] T038 Extend `cloud/cloud-api/auth/middleware.go` — add `RateLimitMiddleware`: `golang.org/x/time/rate` token bucket (10 req/s) per `device_id` extracted from request context, stored in a `sync.Map`; apply to `/internal/hub/ingest` route
- [X] T039 [P] Add background goroutine in `cloud/cloud-api/main.go` — ticker every 30 s: `UPDATE hubs SET is_online = false WHERE last_ping < NOW() - INTERVAL '2 minutes' AND is_online = true`; cancel on server shutdown
- [X] T040 [P] Add graceful shutdown in `cloud/cloud-api/main.go` — `os.Signal` channel listening for `SIGINT`/`SIGTERM`; call `server.Shutdown(ctx)` with 15-second timeout; log shutdown start and completion via zap
- [X] T041 [P] Add structured zap logging to all handlers in `cloud/cloud-api/handlers/` — log each incoming request (method, path, status, latency) via chi middleware; log provisioning events (tenant create, hub register, device provision) at INFO level; never log tokens or hashes
- [X] T042 [P] Add cloud-api service block to `deploy/cloud/docker-compose.yml` — `image: cloud-api:latest`, env_file `.env`, `ports: ["127.0.0.1:9090:9090"]`, `depends_on: postgres`, `healthcheck: GET /health`
- [X] T043 [P] Write integration test for US1 in `cloud/cloud-api/integration/provisioning_test.go` — use `testcontainers-go` to spin up TimescaleDB; test full provision flow: login → create tenant → register hub → provision device → verify via GET; covers SC-001
- [X] T044 [P] Write integration test for US2 in `cloud/cloud-api/integration/telemetry_ws_test.go` — POST 100-event batch to `/internal/hub/ingest`, measure latency (SC-002); open WebSocket, send command via REST, verify delivery within 1 s (SC-003)
- [X] T045 [P] Write integration test for US3 in `cloud/cloud-api/integration/cachesync_test.go` — seed two tenants + two hubs + 3 active + 1 revoked device; verify cache-sync returns only active devices for own tenant, zero cross-tenant leakage (SC-004)
- [X] T046 [P] Write integration test for US4 in `cloud/cloud-api/integration/auth_refresh_test.go` — login, refresh, verify old token rejected, verify new token works; test expired-token 401 with `token_expired` message
- [X] T047 Verify Docker image size < 20 MB: `docker build -t cloud-api:dev cloud/cloud-api/ && docker image inspect cloud-api:dev --format='{{.Size}}'`; confirms SC-006 (`FROM scratch` + `-ldflags="-s -w"`)

---

## Dependency Graph

```
Phase 1 (Setup) ──────────────────────────────────────────────────► 
Phase 2 (Foundational: T005–T015) ────────────────────────────────►
  ├─► Phase 3: US1 (T016–T024) — can start as soon as Phase 2 complete
  ├─► Phase 4: US2 (T025–T030) — depends on Phase 3 complete (needs devices/hubs queries)
  ├─► Phase 5: US3 (T031–T035) — depends on Phase 3 (needs devices table + tenant scoping)
  └─► Phase 6: US4 (T036–T037) — depends on Phase 2 (needs auth queries from T014)
Phase 7 (Polish) ─── depends on Phases 3–6 complete ──────────────►
```

**Story completion order** (dependency-first):
1. Phase 1 → Phase 2 (sequential — each phase blocks the next)
2. US1 (Phase 3) → US2 (Phase 4) → US3 (Phase 5)
3. US4 (Phase 6) can be developed after Phase 2 with only `handlers/auth.go` from US1 as dependency

**Independent stories**: US3 (cache-sync) and US4 (refresh) can be worked in parallel once Phase 3 is complete.

---

## Parallel Execution Opportunities

### Within Phase 2 (after T009, T010):
```
T011 (bcrypt.go) ──┐
T012 (jwt.go)    ──┤─► T013 (middleware.go) ──► T015 (main.go)
T014 (auth.go)   ──┘
```

### Within Phase 3 (US1, after Phase 2):
```
T016 (tenants.go) ──┐
T017 (hubs.go)    ──┤
T018 (devices.go) ──┤─► T021 (handlers/auth.go) ──► T022, T023, T024
T019 (grafana)    ──┘
T020 (health.go)  ─── independent
```

### Within Phase 5 (US3, after Phase 3):
```
T031 (rules queries)   ──┐
T032 (devices cache)   ──┤─► T033 (cachesync.go)
T034 (rules handler)   ──┘ [independent of T033]
T035 (telemetry handler) ── independent
```

### Phase 7 — all integration tests in parallel:
```
T043 [US1 test]  ──┐
T044 [US2 test]  ──┤
T045 [US3 test]  ──┤─► all run concurrently (separate test files, separate containers)
T046 [US4 test]  ──┘
```

---

## Implementation Strategy

**MVP Scope** (deliver first): **Phase 1 + Phase 2 + Phase 3 (US1)**

With just US1 complete, the following work independently:
- Admin can provision the full tenant → hub → device chain
- Cloud Bridge can register its hub and retrieve its API key
- Grafana org exists per tenant

**Increment 2**: Add Phase 4 (US2) — live telemetry and WebSocket commands. This enables end-to-end Pi ↔ Cloud data flow.

**Increment 3**: Add Phase 5 (US3) — cache-sync. Pi can now populate its offline auth cache.

**Increment 4**: Add Phase 6 (US4) — JWT refresh. Production-grade auth lifecycle.

**Final**: Phase 7 — Polish, rate limiting, graceful shutdown, Docker integration, integration tests, image size verification.

---

## Task Count Summary

| Phase | Tasks | Parallelizable |
|---|---|---|
| Phase 1: Setup | 4 (T001–T004) | 3 |
| Phase 2: Foundational | 11 (T005–T015) | 6 |
| Phase 3: US1 (P1) | 9 (T016–T024) | 5 |
| Phase 4: US2 (P1) | 6 (T025–T030) | 1 |
| Phase 5: US3 (P1) | 5 (T031–T035) | 4 |
| Phase 6: US4 (P2) | 2 (T036–T037) | 0 |
| Phase 7: Polish | 10 (T038–T047) | 8 |
| **Total** | **47** | **27** |

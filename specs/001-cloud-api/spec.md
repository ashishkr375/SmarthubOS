# Feature Specification: Cloud API

**Feature Branch**: `001-cloud-api`
**Created**: 2026-03-03
**Status**: Draft
**Input**: Go REST + WebSocket Cloud API with JWT auth, tenant/hub/device management, hub ingest endpoint, and per-hub WebSocket command channel

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Admin provisions a new tenant, hub, and device (Priority: P1)

An administrator logs in, creates a tenant (e.g. "student lab 01"), registers a
Raspberry Pi hub and obtains its API key, then provisions a device and obtains its
one-time token. All three records are persisted in PostgreSQL and can be retrieved
via the API.

**Why this priority**: Everything downstream — Cloud Bridge, MQTT adapter, telemetry
ingest — depends on tenants, hubs, and devices existing with valid hashed credentials
in the database. This is the foundational provisioning flow.

**Independent Test**: Run cloud stack locally (`docker compose up -d`), call provisioning
endpoints via curl/Postman. Verify records exist in `psql`. No Pi or device needed.

**Acceptance Scenarios**:

1. **Given** a running Cloud API with an empty DB, **When** `POST /api/v1/auth/login` is called with valid admin credentials, **Then** a 200 response returns `access_token` (JWT, 15 min expiry) and `refresh_token` (7 day expiry).
2. **Given** a valid admin JWT, **When** `POST /api/v1/tenants` is called with `{"name":"lab01"}`, **Then** a 201 response returns the tenant object including a generated `namespace` (e.g. `lab01`), and a Grafana Organization is created for that tenant.
3. **Given** a valid admin JWT and an existing tenant, **When** `POST /api/v1/hubs/register` is called with `{"name":"pi-01"}`, **Then** a 201 response returns `hub_id` and `api_key` (plaintext, shown once). The stored `api_key_hash` is bcrypt(cost=12).
4. **Given** a valid admin JWT, tenant, and hub, **When** `POST /api/v1/devices` is called with `{"tenant_id":…,"hub_id":…,"device_name":"sensor-1","protocol":"mqtt"}`, **Then** a 201 response returns `device_id` and `token` (plaintext, shown once). The stored `token_hash` is bcrypt(cost=12). Subsequent `GET /api/v1/devices` never returns plaintext tokens.

---

### User Story 2 — Pi hub sends telemetry batch and receives commands (Priority: P1)

The Cloud Bridge (running on the Pi) authenticates with its hub API key and POSTs a
batch of Standard Event Schema objects. The Cloud API persists them to the TimescaleDB
`telemetry` hypertable. A persistent WebSocket connection from the same hub receives
command and revocation messages pushed by the API.

**Why this priority**: This is the live data path. Without it, no telemetry reaches
the cloud and no commands reach devices.

**Independent Test**: Use `websocat` or a simple Go test client to open the hub WebSocket.
POST a batch to `/internal/hub/ingest` with a valid hub API key. Verify rows in `telemetry`
table and that the WebSocket receives a test command pushed via `POST /api/v1/devices/{id}/command`.

**Acceptance Scenarios**:

1. **Given** a registered hub, **When** `POST /internal/hub/ingest` is called with `Authorization: Bearer {hub_api_key}` and a body of up to 100 Standard Event objects, **Then** all events are written to `telemetry` and the API returns `204 No Content`.
2. **Given** an authenticated hub WebSocket at `GET /hub/ws`, **When** `POST /api/v1/devices/{id}/command` is called by an admin, **Then** the WebSocket receives `{"type":"command","tenant_id":…,"device_id":…,"payload":{…}}` within 1 second.
3. **Given** an authenticated hub WebSocket, **When** `DELETE /api/v1/devices/{id}` is called, **Then** the WebSocket receives `{"type":"revoke","device_id":…}` within 1 second, and the device status in DB is set to `revoked`.
4. **Given** a hub API key, **When** `POST /internal/hub/heartbeat` is called, **Then** `hubs.is_online` is set to `true` and `hubs.last_ping` is updated. If no heartbeat arrives for > 2 minutes, `is_online` is set to `false`.

---

### User Story 3 — Cache sync endpoint returns devices and rules for a hub (Priority: P1)

The Cloud Bridge calls `GET /internal/hub/cache-sync` every 60 seconds. The API returns
all `active` devices belonging to hubs of the same tenant set, along with all active
rules for those tenants — exactly the data the Pi needs to populate its SQLite auth cache.

**Why this priority**: The Pi's entire offline-resilient auth model depends on this sync.
Without it, the local auth cache is empty and no devices can connect.

**Independent Test**: Register a hub + 3 devices (two active, one revoked). Call
`GET /internal/hub/cache-sync` with the hub API key. Verify response contains only the
two active devices with correct `token_hash` and `allowed_topics`, and excludes the
revoked one.

**Acceptance Scenarios**:

1. **Given** a registered hub with 3 active devices and 1 revoked device, **When** `GET /internal/hub/cache-sync` is called with the hub's API key, **Then** the response contains only the 3 active devices, each with `device_id`, `tenant_id`, `token_hash`, `allowed_topics`, and `status: "active"`.
2. **Given** 2 active rules for the hub's tenant, **When** `GET /internal/hub/cache-sync` is called, **Then** the response `rules` array contains both rules with `rule_id`, `tenant_id`, `rule_json`, and `is_active: true`.
3. **Given** a hub API key belonging to hub A, **When** `GET /internal/hub/cache-sync` is called, **Then** devices belonging to other hubs' tenants are NOT included (tenant isolation enforced).

---

### User Story 4 — JWT token refresh and revocation (Priority: P2)

An authenticated user can refresh their access token using a valid refresh token. Refresh
tokens are stored in the DB and can be revoked (e.g. on logout or forced sign-out).

**Why this priority**: 15-minute access token expiry makes refresh essential for UX.
Revocability is required for security.

**Independent Test**: Login, wait for access token to be near-expired (or mock the expiry),
call `POST /api/v1/auth/refresh`. Verify a new access token is returned. Then revoke the
refresh token and verify a subsequent refresh attempt returns 401.

**Acceptance Scenarios**:

1. **Given** a valid refresh token, **When** `POST /api/v1/auth/refresh` is called, **Then** a new `access_token` (15 min) is returned and the old one is invalidated.
2. **Given** a revoked or expired refresh token, **When** `POST /api/v1/auth/refresh` is called, **Then** a `401 Unauthorized` is returned.
3. **Given** an expired access token, **When** any protected endpoint is called, **Then** a `401 Unauthorized` with `{"error":"token_expired"}` is returned.

---

### Edge Cases

- Hub API key used on a hub-scoped endpoint belonging to a different hub → `403 Forbidden`
- `POST /internal/hub/ingest` with malformed Standard Event Schema → `400 Bad Request`, no partial writes
- `POST /api/v1/tenants` with a duplicate namespace → `409 Conflict`
- `POST /api/v1/devices` with `protocol` value not in the allowed enum → `400 Bad Request`
- Hub WebSocket disconnects mid-command delivery → VPS buffers the command for up to 5 minutes (TTL), delivers on reconnect
- `DELETE /api/v1/devices/{id}` for a device that is already `revoked` → idempotent `200 OK`

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The API MUST expose all endpoints defined in CONSTITUTION.md §13 (Public API + Internal Hub API + Health).
- **FR-002**: `POST /api/v1/auth/login` MUST return a signed HS256 JWT access token (15 min expiry) and a refresh token (7 day expiry). Algorithm: HS256, secret from `JWT_SECRET` env var.
- **FR-003**: `tenant_id` MUST be resolved from the authenticated JWT claim — never from the request body or URL parameter.
- **FR-004**: `hub_id` MUST be resolved from the hub API key credential — never from any request field.
- **FR-005**: Device provisioning MUST generate a 32-byte random token, return it once in plaintext, and persist only `bcrypt(token, cost=12)` in the `devices.token_hash` column.
- **FR-006**: Hub registration MUST generate a 32-byte random API key, return it once in plaintext, and persist only `bcrypt(api_key, cost=12)` in `hubs.api_key_hash`.
- **FR-007**: `POST /internal/hub/ingest` MUST write events to the TimescaleDB `telemetry` hypertable using a bulk insert (single transaction per batch).
- **FR-008**: `GET /hub/ws` MUST maintain a persistent WebSocket connection per hub. On `POST /api/v1/devices/{id}/command`, the command MUST be delivered to the connected hub within 1 second.
- **FR-009**: `DELETE /api/v1/devices/{id}` MUST push a `{"type":"revoke","device_id":…}` message to the hub's WebSocket channel immediately, and set `devices.status = 'revoked'` in the DB atomically.
- **FR-010**: `GET /internal/hub/cache-sync` MUST return only devices and rules scoped to the requesting hub's tenant set. Cross-tenant data MUST never leak.
- **FR-011**: `POST /api/v1/tenants` MUST call the Grafana HTTP API to create a Grafana Organization for the new tenant.
- **FR-012**: All endpoints MUST return `Content-Type: application/json`. Error responses MUST use `{"error":"<message>"}` format.
- **FR-013**: `GET /health` MUST return `{"status":"ok","db":"up"}` when PostgreSQL is reachable, and `{"status":"degraded","db":"down"}` (with HTTP 503) when it is not.
- **FR-014**: The server MUST bind to the port specified in the `PORT` env var (default: 9090).

### Key Entities

- **Tenant**: Multi-tenant namespace. Has `id` (UUID), `name`, `namespace` (unique slug), `is_active`, `created_at`.
- **Hub**: A Raspberry Pi. Has `id`, `api_key_hash`, `last_ping`, `is_online`, `created_at`. Associated with one or more tenants via devices.
- **Device**: A physical sensor/actuator. Has `id`, `tenant_id`, `hub_id`, `device_name`, `protocol` (enum), `token_hash`, `status` (active/suspended/revoked), `last_seen`.
- **Telemetry**: Time-series measurements. Has `time`, `tenant_id`, `hub_id`, `device_id`, `protocol_src`, `payload` (JSONB). Stored in TimescaleDB hypertable.
- **Rule**: Automation rule in JSONLogic format. Has `id`, `tenant_id`, `rule_json`, `is_active`.
- **RefreshToken**: Stored refresh token for revocability. Has `id`, `user_id`, `token_hash`, `expires_at`, `revoked_at`.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: All provisioning endpoints (tenant → hub → device) respond in < 200 ms p95 under local test load.
- **SC-002**: `POST /internal/hub/ingest` with a 100-event batch writes all rows to TimescaleDB in < 500 ms p95.
- **SC-003**: Hub WebSocket command delivery latency < 1 second from API call to message receipt (measured in integration test).
- **SC-004**: `GET /internal/hub/cache-sync` returns correct tenant-scoped data with zero cross-tenant leakage (verified by integration test with two tenants and two hubs).
- **SC-005**: `go test -race -cover ./...` passes with ≥ 80% coverage on handler and service packages.
- **SC-006**: Docker image builds with `FROM scratch` and binary size < 20 MB.
- **SC-007**: `GET /health` returns 200 when DB is up; returns 503 within 5 seconds when DB is stopped.

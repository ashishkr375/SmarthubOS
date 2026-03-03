# Research: Cloud API (001-cloud-api)

All decisions below are derived from CONSTITUTION.md (single source of truth) and
validated against Go 1.21+ ecosystem best practices. No NEEDS CLARIFICATION items
remain from the Technical Context.

---

## Decision 1: HTTP Router — `chi` v5 over stdlib mux

**Decision**: Use `go-chi/chi` v5.

**Rationale**: The API has nested route groups with different middleware chains
(public unauthenticated, JWT-authenticated user routes, hub-API-key-authenticated
internal routes). `chi` provides middleware stacking per route group cleanly without
reflection or code generation. It is stdlib-compatible (`http.Handler`) — no lock-in.
The binary size overhead is ~200 KB, well within the 20 MB budget.

**Alternatives considered**:
- `net/http` ServeMux (Go 1.22+): Would work, but lacks per-group middleware; requires
  manual middleware chaining repetition on every handler registration.
- `gin`: Larger dependency tree; uses its own Context type, making handler testing
  harder. Unnecessary for this scope.

---

## Decision 2: WebSocket Library — `gorilla/websocket`

**Decision**: Use `gorilla/websocket` v1.5.

**Rationale**: The spec requires a persistent per-hub WebSocket channel at `/hub/ws`.
`gorilla/websocket` is the de-facto standard, handles ping/pong keepalives, partial
message reads, and concurrent write locking out of the box. The hub manager pattern
(a `sync.Map` of `hub_id → *websocket.Conn`) is well-documented and straightforward.

**Alternatives considered**:
- `nhooyr.io/websocket`: Newer API, context-aware. Good choice but slightly less
  documentation around concurrent write patterns. Either would work; `gorilla` chosen
  for team familiarity.
- stdlib `golang.org/x/net/websocket`: Deprecated effectively; not recommended.

---

## Decision 3: JWT — `golang-jwt/jwt` v5

**Decision**: Use `golang-jwt/jwt` v5, HS256 algorithm only.

**Rationale**: CONSTITUTION.md §13 mandates HS256 signed with `JWT_SECRET`. v5 has
breaking-change protections against algorithm confusion attacks (explicit `WithValidMethods`
option). Claims struct: `sub` (user UUID), `tenant_id`, `role` (admin|user), `exp`.

**Security note**: `JWT_SECRET` must be ≥ 256 bits (32 bytes), generated with
`openssl rand -hex 32`. Stored only in the VPS `.env` file, never committed.

**Alternatives considered**:
- RS256 with key pair: More secure (private key never leaves server), but adds key
  management complexity for an MVP with a single server. HS256 is sufficient when the
  secret is strong and the server is not distributed.

---

## Decision 4: PostgreSQL Driver — `jackc/pgx` v5 (pgxpool)

**Decision**: Use `pgx/v5` with `pgxpool.Pool`.

**Rationale**: `pgx` is significantly faster than `database/sql` + `lib/pq` for
PostgreSQL, supports COPY protocol for bulk inserts (useful for telemetry batch
ingest), and has first-class JSONB support. `pgxpool` handles connection pooling
natively. The `batch` API allows the 100-event ingest to be a single round-trip.

**Alternatives considered**:
- `database/sql` + `lib/pq`: Standard but slower; no COPY support; JSONB requires
  manual marshalling workarounds.
- GORM: Adds 500 KB+ to binary; reflection-based; hides query behaviour. The queries
  here are simple and well-defined — a thin query layer in `db/queries/` is sufficient.

---

## Decision 5: Telemetry Batch Ingest — pgx `SendBatch`

**Decision**: Use `pgx/v5 Batch` (pipeline mode) for bulk inserting up to 100 events.

**Rationale**: A single `pgx.Batch` sends all 100 INSERT statements in one TCP
round-trip and commits them as a single transaction. This meets the SC-002 target
(< 500 ms p95 for 100 events) without needing COPY (which requires a different wire
protocol and more complex error handling).

**Alternatives considered**:
- Individual INSERTs in a loop: Too slow; 100 × network round-trips.
- `COPY FROM STDIN`: Fastest possible, but binary protocol is complex to implement
  correctly and harder to debug. Batch pipeline is sufficient for MVP.
- Single INSERT with `unnest()` array parameters: Also viable; pgx Batch is simpler.

---

## Decision 6: Hub WebSocket Manager — in-process `sync.Map`

**Decision**: Store live hub WebSocket connections in an in-process `sync.Map[string, *HubConn]`
keyed by `hub_id`.

**Rationale**: For MVP (single VPS instance), in-process state is sufficient and has
zero latency for command fan-out. When `POST /api/v1/devices/{id}/command` is called,
the handler looks up the hub's connection in the map and writes directly.

**Resiliency**: If the hub WebSocket is disconnected, the command is dropped (caller
gets `202 Accepted, hub_offline`). CONSTITUTION.md §10.1C specifies that VPS buffers
commands for up to 5 minutes on its side — this is implemented as a small in-memory
pending-commands queue per hub with TTL.

**Alternatives considered**:
- Redis pub/sub: Required for multi-instance VPS scaling. Deferred to a future phase
  when horizontal scaling is needed. Adds Redis as a dependency unnecessarily for MVP.

---

## Decision 7: Migrations — `golang-migrate` SQL files

**Decision**: Use `golang-migrate/migrate` v4 with plain `.sql` files in `cloud/migrations/`.

**Rationale**: SQL files are readable, versionable, and runnable independently of the
Go binary (useful in CI and for manual VPS recovery). Migrations run at startup if
`AUTO_MIGRATE=true` env var is set, or can be run manually via the `migrate` CLI.

**Migration order**:
1. `001_initial_schema` — tenants, hubs, devices, rules, refresh_tokens tables
2. `002_timescale_hypertable` — telemetry table + `create_hypertable()` call + retention policy + continuous aggregate

---

## Decision 8: Grafana Org Provisioning — HTTP API on tenant create

**Decision**: On `POST /api/v1/tenants`, call Grafana's HTTP API synchronously using
`http.DefaultClient` to create a Grafana Organization named after the tenant namespace.

**Rationale**: CONSTITUTION.md §16 Phase 3 and FR-011 require this. Synchronous call
keeps the tenant creation atomic from the caller's perspective — if Grafana org creation
fails, the tenant creation returns a 500 and the DB row is rolled back.

**Grafana API calls** (internal, via `http://grafana:3000`):
1. `POST /api/orgs` — create org named `{tenant_namespace}`
2. `POST /api/orgs/{orgId}/users` — add admin user to the org

**Error handling**: If `GRAFANA_URL` is empty (local dev without Grafana), skip the
Grafana calls and log a warning. This allows running Cloud API without Grafana in unit
tests.

---

## Decision 9: Dockerfile — Multi-stage, FROM scratch

**Decision**:
```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o cloud-api ./...

FROM scratch
COPY --from=builder /build/cloud-api /cloud-api
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["/cloud-api"]
```

**Rationale**: CONSTITUTION.md §23 rule 9 mandates `FROM scratch`. The API makes
outbound HTTPS calls to Grafana and Let's Encrypt (for health) — these require
`ca-certificates.crt` to be present. Binary compiled with `-s -w` strips debug
symbols; expected size ~15–18 MB.

**Alternatives considered**:
- `gcr.io/distroless/static`: Also valid; adds ~2 MB but includes timezone data.
  `FROM scratch` + manual cert copy is sufficient since we don't need timezone DB.

---

## No NEEDS CLARIFICATION items remain.

All unknowns from Technical Context have been resolved above.

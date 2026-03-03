# Data Model: Cloud API (001-cloud-api)

Source: CONSTITUTION.md §6 (canonical SQL), §7 (Standard Event Schema)

---

## Entity Relationship Diagram

```
tenants 1─────┐
              │ has many
              ▼
            devices ──────────── hubs (N devices per hub, 1 hub per device)
              │                    │
              │ has many           │ hub produces
              ▼                    ▼
            rules             telemetry (hypertable)

users 1──────────────────── refresh_tokens (1 user : many refresh tokens)
```

Detailed relationships:

```
tenants 1──< devices >──1 hubs
tenants 1──< rules
tenants 1──< telemetry (foreign key, no FK constraint on hypertable — tenant_id is indexed)
users 1──< refresh_tokens
```

---

## Entity: tenants

**Purpose**: Multi-tenant namespace. Each student lab / customer environment is one tenant.

| Column | Type | Constraint | Notes |
|---|---|---|---|
| `id` | `UUID` | PK, DEFAULT `uuid_generate_v4()` | |
| `name` | `VARCHAR(255)` | NOT NULL | Human-readable name, e.g. "Student Lab 01" |
| `namespace` | `VARCHAR(64)` | NOT NULL, UNIQUE | URL-safe slug, e.g. `lab01`. Derived from name on create. |
| `created_at` | `TIMESTAMPTZ` | NOT NULL, DEFAULT `NOW()` | |
| `is_active` | `BOOLEAN` | NOT NULL, DEFAULT `TRUE` | Soft-delete / disable flag |

**Indexes**: `idx_tenants_namespace ON tenants(namespace)`

**Constraints**: `namespace` must be unique across all tenants. Validated as URL-safe slug in application layer.

---

## Entity: hubs

**Purpose**: Represents one Raspberry Pi. One hub can serve multiple tenants' devices.

| Column | Type | Constraint | Notes |
|---|---|---|---|
| `id` | `UUID` | PK, DEFAULT `uuid_generate_v4()` | |
| `name` | `VARCHAR(255)` | NOT NULL | Human label, e.g. "pi-01" |
| `api_key_hash` | `VARCHAR(72)` | NOT NULL | `bcrypt(hub_api_key, cost=12)`. Raw key returned once, never re-accessed. |
| `last_ping` | `TIMESTAMPTZ` | NULL | Set by `/internal/hub/heartbeat` |
| `is_online` | `BOOLEAN` | NOT NULL, DEFAULT `FALSE` | Set `TRUE` by heartbeat; a background job sets `FALSE` if no ping > 2 min |
| `created_at` | `TIMESTAMPTZ` | NOT NULL, DEFAULT `NOW()` | |

**Security invariant**: `api_key_hash` is NEVER returned by any endpoint after initial registration. The raw key is shown exactly once in the `POST /api/v1/hubs/register` response.

---

## Entity: devices

**Purpose**: Represents a physical sensor/actuator registered under a tenant and assigned to a hub.

| Column | Type | Constraint | Notes |
|---|---|---|---|
| `id` | `UUID` | PK, DEFAULT `uuid_generate_v4()` | |
| `tenant_id` | `UUID` | NOT NULL, FK → `tenants(id)` ON DELETE CASCADE | |
| `hub_id` | `UUID` | NOT NULL, FK → `hubs(id)` | Hub that manages this device |
| `device_name` | `VARCHAR(255)` | NOT NULL | e.g. "sensor-1" |
| `protocol` | `VARCHAR(10)` | NOT NULL, CHECK IN ('mqtt','http','coap','ws') | Protocol adapter this device uses |
| `token_hash` | `VARCHAR(72)` | NOT NULL | `bcrypt(token, cost=12)`. Raw token returned once, never re-accessed. |
| `status` | `VARCHAR(16)` | NOT NULL, DEFAULT `'active'`, CHECK IN ('active','suspended','revoked') | |
| `last_seen` | `TIMESTAMPTZ` | NULL | Updated by protocol adapters on activity |
| `created_at` | `TIMESTAMPTZ` | NOT NULL, DEFAULT `NOW()` | |

**Indexes**: `idx_devices_tenant_id ON devices(tenant_id)`, `idx_devices_hub_id ON devices(hub_id)`

**State transitions**:
```
active ──────→ suspended ──────→ active  (admin action)
       └──────→ revoked          (permanent, triggers WebSocket revoke push)
```

**Security invariant**: `token_hash` is NEVER returned by any endpoint. `GET /api/v1/devices` returns `token_hash` to the hub's cache-sync only — NOT to the public API.

---

## Entity: rules

**Purpose**: JSONLogic automation rule, scoped to a tenant.

| Column | Type | Constraint | Notes |
|---|---|---|---|
| `id` | `UUID` | PK, DEFAULT `uuid_generate_v4()` | |
| `tenant_id` | `UUID` | NOT NULL, FK → `tenants(id)` ON DELETE CASCADE | |
| `rule_json` | `JSONB` | NOT NULL | JSONLogic rule object (see §12 of CONSTITUTION.md) |
| `is_active` | `BOOLEAN` | NOT NULL, DEFAULT `TRUE` | Inactive rules are skipped by rule engine |
| `created_at` | `TIMESTAMPTZ` | NOT NULL, DEFAULT `NOW()` | |

**Indexes**: `idx_rules_tenant_id ON rules(tenant_id)`

---

## Entity: telemetry (TimescaleDB hypertable)

**Purpose**: Immutable time-series sensor readings. Partitioned by `time` (1-week chunks by default).

| Column | Type | Constraint | Notes |
|---|---|---|---|
| `time` | `TIMESTAMPTZ` | NOT NULL | Partition key for hypertable |
| `tenant_id` | `UUID` | NOT NULL | No FK constraint on hypertable for performance |
| `hub_id` | `UUID` | NOT NULL | |
| `device_id` | `UUID` | NOT NULL | |
| `protocol_src` | `VARCHAR(10)` | NOT NULL | e.g. 'mqtt', 'http', 'coap', 'ws' |
| `payload` | `JSONB` | NOT NULL | Normalized Standard Event Schema payload field |

**TimescaleDB configuration**:
```sql
SELECT create_hypertable('telemetry', 'time');
SELECT add_retention_policy('telemetry', INTERVAL '90 days');
```

**Indexes**: `idx_telemetry_tenant ON telemetry(tenant_id, time DESC)`, `idx_telemetry_device ON telemetry(device_id, time DESC)`

**Continuous aggregate** (hourly rollup, kept indefinitely for charts):
```sql
CREATE MATERIALIZED VIEW telemetry_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time) AS bucket,
    tenant_id,
    device_id,
    avg((payload->>'temperature')::float) AS avg_temp,
    avg((payload->>'humidity')::float)    AS avg_humidity
FROM telemetry
GROUP BY bucket, tenant_id, device_id;
```

---

## Entity: refresh_tokens

**Purpose**: Server-side storage of JWT refresh tokens for revocability. One user may have multiple active refresh tokens (multiple devices/sessions).

| Column | Type | Constraint | Notes |
|---|---|---|---|
| `id` | `UUID` | PK, DEFAULT `uuid_generate_v4()` | |
| `user_id` | `UUID` | NOT NULL | References application user (users table, added in future auth feature) |
| `token_hash` | `VARCHAR(72)` | NOT NULL | `bcrypt(refresh_token, cost=12)` |
| `expires_at` | `TIMESTAMPTZ` | NOT NULL | 7 days from creation |
| `revoked_at` | `TIMESTAMPTZ` | NULL | Set on explicit revoke or when rotated |
| `created_at` | `TIMESTAMPTZ` | NOT NULL, DEFAULT `NOW()` | |

**Usage**: On `POST /api/v1/auth/refresh`, server bcrypt-compares the presented token against all non-revoked, non-expired rows for the user. On match, the row is revoked (`revoked_at = NOW()`) and a new refresh token is issued (new row inserted, old one revoked — rotation).

---

## Entity: users (minimal, for MVP)

Exists in DB to anchor `refresh_tokens.user_id`. For MVP, only a single hardcoded admin is supported (credentials from env vars `ADMIN_EMAIL` + `ADMIN_PASSWORD_HASH`). A full users table is scoped to a future feature.

| Column | Type | Notes |
|---|---|---|
| `id` | `UUID` | PK |
| `email` | `VARCHAR(255)` | UNIQUE |
| `password_hash` | `VARCHAR(72)` | bcrypt(cost=12) |
| `role` | `VARCHAR(20)` | 'admin' or 'user' |
| `created_at` | `TIMESTAMPTZ` | |

---

## Standard Event Schema (CONSTITUTION.md §7)

Objects ingested via `POST /internal/hub/ingest` must conform to this schema:

```json
{
  "event_id":        "550e8400-e29b-41d4-a716-446655440000",
  "timestamp":       1709500000,
  "hub_id":          "hub-uuid",
  "tenant_id":       "stu_01",
  "device_id":       "sensor-uuid",
  "protocol_source": "mqtt",
  "event_type":      "telemetry",
  "payload": {
    "temperature": 24.5,
    "humidity":    60
  }
}
```

**Fields**:
| Field | Type | Source | Validated by |
|---|---|---|---|
| `event_id` | UUID string | Protocol adapter | Not null check |
| `timestamp` | int64 (Unix seconds) | Protocol adapter | Must be > 0 |
| `hub_id` | UUID string | Resolved from hub API key credential | MUST match authenticated hub's ID |
| `tenant_id` | UUID string | Resolved from device token | MUST match device's tenant in DB |
| `device_id` | UUID string | Protocol adapter | MUST exist in devices table |
| `protocol_source` | string | Protocol adapter | Must be in allowed enum |
| `event_type` | string | Protocol adapter | `telemetry\|command\|shadow_update\|alert\|heartbeat` |
| `payload` | object | Device | Arbitrary JSON, stored as JSONB |

**Validation**: The API rejects the entire batch if any event fails validation (no partial writes). FR-007.

---

## Migration Files

| File | Creates |
|---|---|
| `001_initial_schema.up.sql` | `tenants`, `hubs`, `devices`, `rules`, `users`, `refresh_tokens` tables + indexes |
| `001_initial_schema.down.sql` | DROP all tables in reverse dependency order |
| `002_timescale_hypertable.up.sql` | `telemetry` table, `create_hypertable`, indexes, retention policy, continuous aggregate |
| `002_timescale_hypertable.down.sql` | DROP hypertable + continuous aggregate |

Migration path: `cloud/migrations/` — applied at startup (when `AUTO_MIGRATE=true`) or via `migrate` CLI.

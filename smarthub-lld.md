# SmartHubOS: Low-Level Design (LLD)

---

## Architecture Principle: Pi is the Edge, Cloud is the Brain

The Raspberry Pi runs **zero persistent databases**. It holds only:
1. A local SQLite file (`edge_cache.db`) that caches auth/ACL data synced from the cloud.
2. NATS JetStream on-disk storage for buffering events during cloud outages.

All canonical data (tenants, devices, telemetry history, rules) lives in the **Cloud Database**.

---

## 1. Database Schema

### 1.1 Cloud Database (PostgreSQL — hosted on cloud, NOT on Pi)

#### Table: `tenants`
| Column       | Type        | Description                                      |
|--------------|-------------|--------------------------------------------------|
| id           | UUID        | Primary Key                                      |
| name         | Varchar     | Student or Project Name                          |
| namespace    | Varchar     | Unique slug (e.g., `stu_1`). Indexed.            |
| created_at   | Timestamptz | Creation time                                    |
| is_active    | Boolean     | Soft-disable a tenant without deleting           |

#### Table: `devices`
| Column       | Type        | Description                                      |
|--------------|-------------|--------------------------------------------------|
| id           | UUID        | Primary Key                                      |
| tenant_id    | UUID        | FK → tenants.id. Indexed.                        |
| device_name  | Varchar     | Human-readable name                              |
| protocol     | Varchar     | ENUM: `mqtt`, `http`, `coap`, `ws`               |
| token_hash   | Varchar     | Bcrypt hash. Never stored in plaintext.          |
| status       | Varchar     | `active`, `suspended`, `revoked`                 |
| last_seen    | Timestamptz | Updated by Cloud API when telemetry arrives      |
| hub_id       | UUID        | FK → hubs.id. Which Pi this device belongs to   |

#### Table: `hubs` (one row per Raspberry Pi)
| Column       | Type        | Description                                      |
|--------------|-------------|--------------------------------------------------|
| id           | UUID        | Primary Key (used as hub_id in devices table)    |
| name         | Varchar     | Human name (e.g., "Lab Room 3 Hub")              |
| api_key_hash | Varchar     | Bcrypt hash of the hub's outbound API key        |
| last_ping    | Timestamptz | Last time the Cloud Bridge checked in            |
| is_online    | Boolean     | Set by Cloud API based on heartbeat              |

#### Table: `rules`
| Column       | Type        | Description                                      |
|--------------|-------------|--------------------------------------------------|
| id           | UUID        | Primary Key                                      |
| tenant_id    | UUID        | FK → tenants.id                                  |
| rule_json    | JSONB       | The rule definition (see Rule Engine section)    |
| is_active    | Boolean     | Toggle rules without deleting                    |

> **Important Indexes on Cloud DB:**
> - `devices(tenant_id)` — fast ACL lookup
> - `devices(token_hash)` — fast auth validation (first 8 chars as prefix index)
> - `tenants(namespace)` — fast namespace resolution
> - Enable **Row-Level Security (RLS)** on `devices` and `rules` tables.

### 1.2 Cloud Time-Series (TimescaleDB extension on PostgreSQL, or InfluxDB Cloud)

#### Hypertable: `telemetry`
| Column       | Type        | Description                      |
|--------------|-------------|----------------------------------|
| time         | Timestamptz | Partition key (auto by TimescaleDB) |
| tenant_id    | UUID        | Indexed                          |
| hub_id       | UUID        | Which Pi originated this         |
| device_id    | UUID        | Indexed                          |
| payload      | JSONB       | Raw sensor values                |
| protocol_src | Varchar     | `mqtt`, `http`, `coap`           |

> Use TimescaleDB's **automatic data retention** policy: keep raw data for 90 days, downsample to hourly averages after that.

### 1.3 Edge Cache (SQLite on Pi — `/data/edge_cache.db`)

#### Table: `auth_cache`
| Column        | Type    | Description                                                |
|---------------|---------|------------------------------------------------------------|
| device_id     | TEXT    | Primary Key                                               |
| tenant_id     | TEXT    | Resolved namespace                                        |
| token_hash    | TEXT    | Bcrypt hash (copied from cloud)                           |
| allowed_topics| TEXT    | JSON array of allowed NATS subject patterns               |
| status        | TEXT    | `active` or `revoked`                                     |
| cached_at     | INTEGER | Unix timestamp. Entries older than 300s are re-validated. |

> The cache is **write-through**: when the Cloud Bridge pulls a sync, it upserts rows. When the cloud sends a `REVOKE` event, the bridge immediately deletes the row, causing the next auth check to fail instantly.

---

## 2. Standard Internal Event Schema
All Protocol Adapters MUST publish this exact schema to NATS. No exceptions.

```json
{
  "event_id": "uuid-v4",
  "timestamp": 1699999999,
  "hub_id": "hub_abc123",
  "tenant_id": "stu_01",
  "device_id": "sensor_a",
  "protocol_source": "coap",
  "event_type": "telemetry",
  "payload": {
    "temperature": 24.5,
    "humidity": 60
  }
}
```

`event_type` values: `telemetry`, `shadow_update`, `alert`, `heartbeat`

---

## 3. NATS JetStream Subject Routing Map

### Internal (Pi-only)
| Subject Pattern | Purpose |
|----------------|---------|
| `tenant.{tenant_id}.telemetry.{device_id}` | Ingress telemetry from device |
| `tenant.{tenant_id}.command.{device_id}` | Outbound command to device |
| `system.audit.{tenant_id}` | Auth failures, errors |
| `system.hub.heartbeat` | Cloud Bridge heartbeat |
| `system.cache.invalidate.{device_id}` | Immediate cache eviction |

### JetStream Streams (on Pi)
| Stream Name | Subject Filter | Max Age | Storage |
|------------|---------------|---------|---------|
| `TELEMETRY` | `tenant.*.telemetry.*` | 1 hour (buffer) | File |
| `COMMANDS` | `tenant.*.command.*` | 5 min | Memory |
| `AUDIT` | `system.audit.*` | 24 hours | File |

> `TELEMETRY` stream uses **file storage** so events survive a Pi reboot during a cloud outage. The Cloud Bridge uses a **durable consumer** with `AckExplicit` — it only advances the cursor after the cloud confirms receipt.

---

## 4. Protocol Adapter Designs

### 4.1 MQTT Adapter
- **Component:** Eclipse Mosquitto + `mosquitto-go-auth` plugin.
- **Auth Flow (all lookups hit local cache, never cloud directly):**
  1. ESP32 connects: username=`{device_id}`, password=`{token}`.
  2. `mosquitto-go-auth` calls `GET http://localhost:8080/internal/auth/mqtt?device_id={id}&token={token}`.
  3. Core API (Pi-local) checks `auth_cache` SQLite table — bcrypt verify.
  4. Returns `200 OK` with ACL or `403 Forbidden`.
  5. ACL strictly limits publish to `tenant/{tenant_id}/{device_id}/+`.
- **Bridge:** Go service, subscribes to Mosquitto `#`, normalizes to Standard Schema, publishes to NATS `tenant.{tid}.telemetry.{did}`.

### 4.2 HTTP Adapter
- **Endpoint:** `POST /api/v1/ingress/telemetry`
- **Auth:** `Authorization: Bearer {device_token}`
- **Logic:**
  1. Extract token → check `auth_cache` SQLite (LRU in-memory, backed by file).
  2. Resolve `tenant_id`.
  3. Wrap in Standard Schema.
  4. `nats.Publish("tenant."+tid+".telemetry."+did, payload)`
- **Rate limiting:** 10 requests/second per `device_id` (token bucket, in-process).

### 4.3 CoAP Adapter
- **Library:** `go-coap`
- **Port:** UDP 5683
- **Path:** `coap://<pi-ip>/telemetry`
- **Auth:** Token in CoAP Uri-Query option: `?token={device_token}`
- **Logic:** Same as HTTP adapter. Validates from `auth_cache`, normalizes, publishes to NATS.

### 4.4 WebSocket Adapter (on same HTTP server)
- **Endpoint:** `ws://<pi-ip>/ws/telemetry`
- **Auth:** First message must be `{"type":"auth","token":"{device_token}"}`. Connection dropped if not received within 5 seconds.
- **Use case:** Browser-based simulators, ESP32 with WebSocket library, low-power devices that can't use MQTT.

---

## 5. Cloud Bridge Service (NEW — Critical Component)

This is the single service that connects the Pi to the cloud. It is the **only** service on the Pi with an outbound internet connection.

### 5.1 Responsibilities
1. **Auth Cache Sync:** Every 60 seconds, call `GET https://api.yourdomain.com/internal/hub/cache-sync?hub_id={hub_id}`. Response: full list of active devices + token hashes for this hub. Upsert into `auth_cache`.
2. **Telemetry Forward:** Consume from NATS `TELEMETRY` JetStream stream (durable consumer). Batch up to 100 events or 500ms, POST to `https://api.yourdomain.com/internal/hub/ingest`. Acknowledge NATS only after cloud confirms `200 OK`.
3. **Command Receive:** Maintain a **persistent WebSocket** to `wss://api.yourdomain.com/hub/ws?hub_id={hub_id}`. When a cloud user sends a command, it arrives here and is published to NATS `tenant.{tid}.command.{did}`.
4. **Heartbeat:** Every 30 seconds, POST `{"hub_id": "...", "uptime": ..., "buffer_depth": ...}` to cloud. Cloud sets `hubs.is_online = true`.
5. **Revocation Push:** Cloud pushes `{"type":"revoke","device_id":"..."}` over the WebSocket. Bridge immediately publishes to `system.cache.invalidate.{device_id}` on NATS AND deletes `auth_cache` row.

### 5.2 Resiliency Rules
- All outbound calls use **exponential backoff** (1s, 2s, 4s, max 60s).
- If cloud is unreachable > 5 minutes, log to `system.audit.hub`. Devices continue working via cache.
- Cache TTL: 5 minutes for token validation. A device token revoked on cloud takes max 5 minutes to expire on Pi (or instantly with WebSocket push).
- NATS JetStream buffer holds up to **50,000 events** on disk (~200 MB). Older events dropped if full.

### 5.3 Cloud Bridge Configuration (`/config/bridge.yaml`)
```yaml
hub_id: "uuid-of-this-pi"
cloud_api_url: "https://api.yourdomain.com"
hub_api_key: "${HUB_API_KEY}"   # loaded from env, never hardcoded
cache_sync_interval_sec: 60
heartbeat_interval_sec: 30
telemetry_batch_size: 100
telemetry_batch_timeout_ms: 500
nats_url: "nats://localhost:4222"
sqlite_path: "/data/edge_cache.db"
tls_verify: true                 # never set false in production
```

---

## 6. Local Auth Cache Service (Pi-local API)

A tiny Go HTTP server (binds to `localhost:8080` only — not exposed outside Pi) that serves `mosquitto-go-auth` requests.

```
GET /internal/auth/mqtt?device_id=X&token=Y
  → 1. Lookup auth_cache WHERE device_id = X AND status = 'active'
  → 2. If not found OR cached_at older than 300s: return 403 (fail safe)
  → 3. bcrypt.CompareHashAndPassword(token_hash, Y)
  → 4. If match: return 200 + ACL JSON
  → 5. If mismatch: return 403, log to system.audit
```

> **Why localhost only?** This endpoint has no auth of its own (it's called by mosquitto on the same Pi). Exposing it externally would be a critical security hole.

---

## 7. Rule Engine (Pi-local, Phase 1)

- Subscribes to NATS `tenant.*.telemetry.*`.
- On startup, loads active rules from `auth_cache` (rules are also synced by Cloud Bridge).
- Evaluates JSONLogic expressions against payload.
- On match: publishes to `tenant.{tid}.command.{did}`.

```json
{
  "rule_id": "rule_001",
  "tenant_id": "stu_01",
  "trigger": { ">": [{ "var": "payload.temperature" }, 30] },
  "action": {
    "subject": "tenant.stu_01.command.fan1",
    "message": { "state": "ON" }
  }
}
```

Rules run **locally on the Pi** — sub-millisecond latency, no cloud round-trip needed for local automation.

---

## 8. API Security Checklist (enforce everywhere)

| Rule | Where enforced |
|------|----------------|
| Device `tenant_id` ALWAYS resolved from token, NEVER from payload body | All adapters |
| `hub_id` ALWAYS resolved from the hub API key credential, NEVER from request body | Cloud Bridge ingest endpoint |
| All cloud-bound calls use TLS (`tls_verify: true`) | Cloud Bridge |
| Local auth cache API binds to `127.0.0.1` only | Core API |
| Bcrypt cost factor ≥ 12 for all token hashes | Cloud API (device provisioning) |
| Rate limit per device: 10 req/s (HTTP), 5 conn/s (MQTT) | Adapters |
| JWT expiry: 15 minutes (access), 7 days (refresh) | Cloud API |
| Revocation takes effect within 5 min max (instant with WebSocket push) | Cloud Bridge |
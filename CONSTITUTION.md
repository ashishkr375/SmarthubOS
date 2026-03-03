# SmartHubOS — Final Constitution
### The single authoritative reference for building, deploying, and extending SmartHubOS.
### Cloud Target: **Option A — Self-Hosted VPS** (confirmed)

> This document supersedes all individual HLD, LLD, and guide files.
> Every architectural decision, database schema, API contract, coding rule, and deployment step is defined here.
> When in doubt, this file wins.

---

## Table of Contents

1. [Mission & Design Philosophy](#1-mission--design-philosophy)
2. [System Architecture](#2-system-architecture)
3. [The Edge-Cloud Split — What Lives Where](#3-the-edge-cloud-split--what-lives-where)
4. [VPS Cloud Stack — Exact Setup](#4-vps-cloud-stack--exact-setup)
5. [Edge (Pi) Stack — Exact Setup](#5-edge-pi-stack--exact-setup)
6. [Database Schemas](#6-database-schemas)
7. [Standard Internal Event Schema](#7-standard-internal-event-schema)
8. [NATS JetStream Routing Map](#8-nats-jetstream-routing-map)
9. [Protocol Adapters](#9-protocol-adapters)
10. [Cloud Bridge Service](#10-cloud-bridge-service)
11. [Local Auth Cache Service](#11-local-auth-cache-service)
12. [Rule Engine](#12-rule-engine)
13. [Cloud API — Endpoint Contract](#13-cloud-api--endpoint-contract)
14. [Repository Structure](#14-repository-structure)
15. [Secrets & Configuration](#15-secrets--configuration)
16. [Development Roadmap](#16-development-roadmap)
17. [Local Development Setup](#17-local-development-setup)
18. [Testing Playbook](#18-testing-playbook)
19. [Production Deployment](#19-production-deployment)
20. [Health Checks & Observability](#20-health-checks--observability)
21. [CI/CD Pipeline](#21-cicd-pipeline)
22. [Adding a Future Protocol](#22-adding-a-future-protocol)
23. [Non-Negotiable Rules](#23-non-negotiable-rules)

---

## 1. Mission & Design Philosophy

**SmartHubOS is an edge-first, cloud-connected, multi-tenant IoT operating environment.**

A Raspberry Pi (or any small ARM/x86 edge computer) acts as the **local edge server**:
- It is the only entry point for physical devices.
- It validates device credentials locally (no cloud round-trip per message).
- It buffers events on-disk during cloud outages and replays them automatically.
- It fires automation rules locally with sub-millisecond latency.

A **VPS in the cloud** is the source of truth:
- All canonical data (tenants, devices, history, rules) lives here.
- The Grafana dashboard is hosted here — accessible from any browser, anywhere.
- Users manage everything from the cloud; the Pi just executes.

### The Golden Rules (summarized — full list in §23)
| Rule | Why |
|------|-----|
| Pi never owns the main database | Telemetry on a Pi's SD card = data loss waiting to happen |
| Cloud never talks directly to devices | Devices connect to Pi only; Pi relays to cloud |
| Pi makes outbound connections only | No inbound internet ports = zero attack surface |
| Adding a new protocol = new container only | Core OS never changes |
| All edge code is Go | < 20 MB RAM per service; no JVM, no Python in hot path |

---

## 2. System Architecture

```
╔══════════════════════════════════════════════════════════════════════╗
║  VPS CLOUD LAYER  (your existing VPS — internet accessible)          ║
║                                                                        ║
║  ┌─────────────────────────────────────────────────────────────────┐ ║
║  │  Caddy  (reverse proxy, port 443, auto TLS via Let's Encrypt)   │ ║
║  └───────┬─────────────────┬──────────────────┬────────────────────┘ ║
║          │                 │                  │                       ║
║  ┌───────▼──────┐  ┌───────▼──────┐  ┌────────▼───────┐            ║
║  │  Cloud API   │  │   Grafana    │  │  React UI       │            ║
║  │  (Go, :9090) │  │  OSS (:3000) │  │  (:3001)        │            ║
║  │  REST + WSS  │  │  Dashboards  │  │  Mgmt Portal    │            ║
║  └───────┬──────┘  └───────┬──────┘  └─────────────────┘            ║
║          │                 │                                          ║
║  ┌───────▼─────────────────▼──────────────────────────────────────┐ ║
║  │  PostgreSQL 16 + TimescaleDB extension                          │ ║
║  │  ├── tenants, devices, hubs, rules  (relational)                │ ║
║  │  └── telemetry  (TimescaleDB hypertable, time-series)           │ ║
║  └─────────────────────────────────────────────────────────────────┘ ║
╚══════════════════════════════════════╦═══════════════════════════════╝
                                       ║  HTTPS POST  (telemetry batch)
                                       ║  WSS         (commands, revocations)
                                       ║  All outbound from Pi, port 443
╔══════════════════════════════════════╩═══════════════════════════════╗
║  EDGE LAYER  (Raspberry Pi — local network, always-on)               ║
║                                                                        ║
║  ┌─────────────────────────────────────────────────────────────────┐ ║
║  │  Cloud Bridge  (Go, only service with internet access)           │ ║
║  │  • Syncs auth cache from VPS every 60s                          │ ║
║  │  • Batches & forwards telemetry to VPS                          │ ║
║  │  • Holds persistent WSS to VPS for commands/revocations         │ ║
║  │  • Sends heartbeat every 30s                                    │ ║
║  └───────────────────────────┬─────────────────────────────────────┘ ║
║                              │                                        ║
║  ┌───────────────────────────▼─────────────────────────────────────┐ ║
║  │  NATS JetStream  (single binary, :4222, file storage on SD)     │ ║
║  │  Streams: TELEMETRY (file), COMMANDS (memory), AUDIT (file)     │ ║
║  └──────┬──────────────┬─────────────────┬───────────────┬─────────┘ ║
║         │              │                 │               │            ║
║  ┌──────▼────┐  ┌───────▼──────┐  ┌──────▼────┐  ┌──────▼────────┐ ║
║  │  MQTT     │  │  HTTP + WS   │  │  CoAP     │  │  Rule Engine  │ ║
║  │  Adapter  │  │  Adapter     │  │  Adapter  │  │  (local only) │ ║
║  │ Mosquitto │  │  Go, :8080   │  │  go-coap  │  │  JSONLogic    │ ║
║  │ :1883     │  │              │  │  UDP:5683 │  │               │ ║
║  └──────┬────┘  └───────┬──────┘  └──────┬────┘  └───────────────┘ ║
║         │               │                │                           ║
║  ┌──────▼───────────────▼────────────────▼───────────────────────┐  ║
║  │  Local Auth Cache  (Go service, localhost:8080 only)           │  ║
║  │  SQLite: edge_cache.db  — device_id → token_hash + ACL         │  ║
║  │  In-memory LRU over SQLite. TTL: 300s. Never hits VPS.         │  ║
║  └────────────────────────────────────────────────────────────────┘  ║
║                                                                        ║
║  ┌──────────────────────────────────────────────────────────────────┐ ║
║  │  Physical Devices  (ESP32, Arduino, sensors)                     │ ║
║  │  Connect via MQTT / HTTP / CoAP / WebSocket — local network only │ ║
║  └──────────────────────────────────────────────────────────────────┘ ║
╚═══════════════════════════════════════════════════════════════════════╝
```

---

## 3. The Edge-Cloud Split — What Lives Where

| Responsibility | Lives on Pi | Lives on VPS | Reason |
|---------------|-------------|-------------|--------|
| Accept device connections | ✓ | ✗ | Devices are local; Pi is the gateway |
| Validate device tokens | ✓ (from cache) | ✗ | Sub-ms latency, works offline |
| Store tenant/device records | ✗ | ✓ | Pi SD card is not reliable long-term storage |
| Store telemetry history | ✗ | ✓ | Telemetry volume exceeds Pi storage |
| Fire local automation rules | ✓ | ✗ | Zero latency, no internet needed |
| Dashboard / Grafana | ✗ | ✓ | Accessible from anywhere; RAM-heavy |
| User login / management | ✗ | ✓ | JWT auth lives on VPS |
| Buffer events (offline) | ✓ (NATS disk) | ✗ | Never lose data when internet drops |
| Send commands to devices | Routes via Pi | Originates from VPS | VPS → Cloud Bridge WSS → Pi → device |

### Offline Behaviour (when VPS is unreachable)
1. Auth cache on Pi is warm — devices keep connecting for up to **5 minutes** before cache TTL expires.
2. NATS JetStream `TELEMETRY` stream persists to SD card — up to **50,000 events buffered**.
3. Local rules keep firing — no cloud dependency for automation.
4. When VPS comes back: Cloud Bridge replays buffered events in FIFO order, auto-acknowledged after VPS confirms receipt.

---

## 4. VPS Cloud Stack — Exact Setup

### Hardware / Hosting
- **Recommended:** Hetzner CX21 (2 vCPU, 4 GB RAM, 40 GB SSD, €4.90/mo) or DigitalOcean Basic Droplet ($6/mo).
- OS: Ubuntu 22.04 LTS 64-bit.
- A domain name pointed at the VPS IP (e.g., `yourdomain.com`).

### VPS Services (all Docker containers via `deploy/cloud/docker-compose.yml`)

| Container | Image | Port | Purpose |
|-----------|-------|------|---------|
| `postgres` | `timescale/timescaledb:latest-pg16` | 5432 (internal) | Main DB + telemetry hypertable |
| `cloud-api` | `smarthub/cloud-api:latest` | 9090 (internal) | REST API + WebSocket hub channel |
| `grafana` | `grafana/grafana-oss:latest` | 3000 (internal) | Dashboards |
| `ui` | `smarthub/ui:latest` | 3001 (internal) | React management portal |
| `caddy` | `caddy:2-alpine` | 80, 443 (public) | Reverse proxy + auto TLS |

### Caddy Routing (`deploy/cloud/Caddyfile`)
```
api.yourdomain.com {
    reverse_proxy cloud-api:9090
}

dashboard.yourdomain.com {
    reverse_proxy grafana:3000
}

app.yourdomain.com {
    reverse_proxy ui:3001
}
```

Caddy automatically obtains and renews Let's Encrypt TLS certificates. Zero manual cert management.

### DNS Records Required
```
A    api.yourdomain.com        →  <your VPS IP>
A    dashboard.yourdomain.com  →  <your VPS IP>
A    app.yourdomain.com        →  <your VPS IP>
```

### PostgreSQL + TimescaleDB Initial Setup
```sql
-- Run once after first deployment
CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Enable Row-Level Security on sensitive tables
ALTER TABLE devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE rules ENABLE ROW LEVEL SECURITY;
```

### Grafana Configuration
- **Data source:** PostgreSQL pointing to `postgres:5432`, database `smarthub`.
- **Organizations:** Create one Grafana Organization per tenant (enforced via Cloud API on tenant creation — uses Grafana HTTP API).
- **Dashboard template:** A JSON template is auto-provisioned per tenant showing: telemetry over time, device status, last seen.
- Grafana runs at `https://dashboard.yourdomain.com`. Login from anywhere.

---

## 5. Edge (Pi) Stack — Exact Setup

### Hardware
- **Minimum:** Raspberry Pi 4 (4 GB RAM), 32 GB Class 10 SD card (or USB SSD preferred).
- **OS:** Raspberry Pi OS Lite 64-bit (no desktop).
- Docker + Docker Compose v2 installed.

### Pi Services (all via `deploy/edge/docker-compose.yml`)

| Container | Image | Port (local only) | Purpose |
|-----------|-------|-------------------|---------|
| `nats` | `nats:2-alpine` | 4222 (internal), 8222 (monitor) | JetStream event bus |
| `mosquitto` | `eclipse-mosquitto:2` | 1883 (LAN only) | MQTT broker |
| `core-api` | `smarthub/core-api:latest` | 127.0.0.1:8080 | Local auth, HTTP/WS ingress |
| `bridge-mqtt` | `smarthub/bridge-mqtt:latest` | — | Mosquitto → NATS forwarder |
| `adapter-coap` | `smarthub/adapter-coap:latest` | 5683/UDP (LAN) | CoAP adapter |
| `bridge-cloud` | `smarthub/bridge-cloud:latest` | 8081 (healthz only) | Cloud Bridge |
| `rule-engine` | `smarthub/rule-engine:latest` | — | Local rule evaluation |

### Pi RAM Budget (hard caps)
| Service | Max RAM | Max CPU (idle) |
|---------|---------|----------------|
| NATS JetStream | 30 MB | 0.5% |
| Mosquitto | 10 MB | 0.5% |
| Core API + Auth | 40 MB | 1% |
| MQTT Bridge | 15 MB | 0.5% |
| CoAP Adapter | 15 MB | 0.5% |
| Cloud Bridge | 20 MB | 1% |
| Rule Engine | 25 MB | 1% |
| Docker overhead | ~50 MB | — |
| **TOTAL HARD CAP** | **< 512 MB** | **< 20% avg** |

**No exceptions.** If a service exceeds its RAM budget, profile and fix it before merging.

---

## 6. Database Schemas

### 6.1 Cloud PostgreSQL (on VPS — source of truth)

```sql
-- TENANTS
CREATE TABLE tenants (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        VARCHAR(255) NOT NULL,
    namespace   VARCHAR(64)  NOT NULL UNIQUE,   -- e.g. "stu_weather_01"
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE
);
CREATE INDEX idx_tenants_namespace ON tenants(namespace);

-- HUBS (one row per Raspberry Pi)
CREATE TABLE hubs (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name         VARCHAR(255) NOT NULL,
    api_key_hash VARCHAR(72)  NOT NULL,          -- bcrypt(hub_api_key, cost=12)
    last_ping    TIMESTAMPTZ,
    is_online    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- DEVICES
CREATE TABLE devices (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id    UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    hub_id       UUID        NOT NULL REFERENCES hubs(id),
    device_name  VARCHAR(255) NOT NULL,
    protocol     VARCHAR(10) NOT NULL CHECK (protocol IN ('mqtt','http','coap','ws')),
    token_hash   VARCHAR(72) NOT NULL,           -- bcrypt(token, cost=12). NEVER store plaintext.
    status       VARCHAR(16) NOT NULL DEFAULT 'active'
                             CHECK (status IN ('active','suspended','revoked')),
    last_seen    TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_devices_tenant_id ON devices(tenant_id);
CREATE INDEX idx_devices_hub_id    ON devices(hub_id);

-- RULES
CREATE TABLE rules (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id   UUID    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    rule_json   JSONB   NOT NULL,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_rules_tenant_id ON rules(tenant_id);
```

### 6.2 Cloud Telemetry (TimescaleDB Hypertable — on same VPS PostgreSQL)

```sql
CREATE TABLE telemetry (
    time         TIMESTAMPTZ  NOT NULL,
    tenant_id    UUID         NOT NULL,
    hub_id       UUID         NOT NULL,
    device_id    UUID         NOT NULL,
    protocol_src VARCHAR(10)  NOT NULL,
    payload      JSONB        NOT NULL
);

-- Convert to hypertable partitioned by time
SELECT create_hypertable('telemetry', 'time');

-- Indexes for fast tenant/device queries
CREATE INDEX idx_telemetry_tenant  ON telemetry(tenant_id, time DESC);
CREATE INDEX idx_telemetry_device  ON telemetry(device_id, time DESC);

-- Retention: keep raw data 90 days, then drop
SELECT add_retention_policy('telemetry', INTERVAL '90 days');

-- Continuous aggregate: hourly averages (kept indefinitely for charts)
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

### 6.3 Edge SQLite Cache (on Pi — `/data/edge_cache.db`)

```sql
CREATE TABLE auth_cache (
    device_id      TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL,
    token_hash     TEXT NOT NULL,       -- bcrypt hash copied from cloud
    allowed_topics TEXT NOT NULL,       -- JSON array: ["tenant.stu_01.telemetry.sensor_a"]
    status         TEXT NOT NULL DEFAULT 'active',
    cached_at      INTEGER NOT NULL     -- Unix timestamp. Stale if > 300s ago.
);

CREATE TABLE rules_cache (
    rule_id    TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    rule_json  TEXT NOT NULL,           -- JSONLogic rule (see §12)
    is_active  INTEGER NOT NULL DEFAULT 1,
    cached_at  INTEGER NOT NULL
);
```

The cache is **append/upsert only** from the Cloud Bridge. The only delete is:
1. Explicit `REVOKE` push from the VPS over WebSocket.
2. Cloud Bridge full-sync finding a device no longer in the VPS response (it upserts fresh data, effectively replacing stale rows).

---

## 7. Standard Internal Event Schema

Every Protocol Adapter MUST publish this exact structure to NATS before doing anything else. No raw device payloads ever enter NATS directly.

```json
{
  "event_id":       "550e8400-e29b-41d4-a716-446655440000",
  "timestamp":      1709500000,
  "hub_id":         "hub-uuid-of-this-pi",
  "tenant_id":      "stu_01",
  "device_id":      "sensor-uuid",
  "protocol_source": "mqtt",
  "event_type":     "telemetry",
  "payload": {
    "temperature": 24.5,
    "humidity":    60
  }
}
```

**`event_type` values:**
- `telemetry` — sensor data from device to cloud
- `command` — instruction from cloud/rule engine to device
- `shadow_update` — desired state update
- `alert` — threshold breach notification
- `heartbeat` — device alive ping

**Where `tenant_id` and `hub_id` come from:**
- `tenant_id`: resolved by the adapter from the validated token via auth cache. **Never** from the raw payload body.
- `hub_id`: read from the `HUB_ID` environment variable at startup. **Never** from any request body.

---

## 8. NATS JetStream Routing Map

### Subjects
| Subject Pattern | Direction | Description |
|----------------|-----------|-------------|
| `tenant.{tenant_id}.telemetry.{device_id}` | Device → Cloud | Ingress sensor data |
| `tenant.{tenant_id}.command.{device_id}` | Cloud → Device | Outbound commands |
| `tenant.{tenant_id}.shadow.{device_id}` | Bidirectional | Device state shadow |
| `system.audit.{tenant_id}` | Internal | Auth fails, errors, bad payloads |
| `system.hub.heartbeat` | Pi → Cloud | Bridge heartbeat |
| `system.cache.invalidate.{device_id}` | Cloud → Pi | Instant cache eviction on revoke |

### JetStream Streams (configured on Pi NATS server)
| Stream | Subject Filter | Storage | Max Age | Max Messages | Purpose |
|--------|---------------|---------|---------|-------------|---------|
| `TELEMETRY` | `tenant.*.telemetry.*` | **File** | 1 hour | 50,000 | Buffer during cloud outage |
| `COMMANDS` | `tenant.*.command.*` | Memory | 5 min | 10,000 | Command delivery |
| `AUDIT` | `system.audit.*` | **File** | 24 hours | 100,000 | Error log |

> `TELEMETRY` **must** use file storage. If you use memory storage and the Pi reboots during an outage, you lose all buffered data. This is non-negotiable (see §23).

### Durable Consumers
| Consumer Name | Stream | Ack Policy | Used By |
|--------------|--------|------------|---------|
| `cloud-bridge` | `TELEMETRY` | `AckExplicit` | Cloud Bridge (forwards to VPS) |
| `rule-engine` | `TELEMETRY` | `AckExplicit` | Rule Engine (evaluates rules) |
| `mqtt-egress` | `COMMANDS` | `AckExplicit` | MQTT Bridge (sends to Mosquitto) |

All consumers are **durable** (survive service restarts). The consumer only advances its cursor after the subscriber explicitly calls `msg.Ack()`, which only happens after successful delivery downstream.

---

## 9. Protocol Adapters

### 9.1 MQTT Adapter (Mosquitto + bridge-mqtt service)

**Mosquitto configuration (`deploy/edge/mosquitto.conf`):**
```
listener 1883
allow_anonymous false
auth_plugin /mosquitto/go-auth.so
auth_opt_backends http
auth_opt_http_host 127.0.0.1
auth_opt_http_port 8080
auth_opt_http_getuser_uri /internal/auth/mqtt
auth_opt_http_aclcheck_uri /internal/acl/mqtt
```

**Auth flow:**
1. ESP32 connects: `username = {device_id}`, `password = {token}`.
2. Mosquitto calls `GET http://127.0.0.1:8080/internal/auth/mqtt?username={device_id}&password={token}`.
3. Core API looks up `auth_cache` SQLite: bcrypt hash compare.
4. Returns `200 OK` (allow) or `403 Forbidden` (deny). No cloud round-trip.
5. ACL strictly limits device to publish on `tenant/{tenant_id}/{device_id}/telemetry` and subscribe to `tenant/{tenant_id}/{device_id}/command`.

**bridge-mqtt service logic:**
1. Subscribes to Mosquitto topic `#` (all topics).
2. Parses the topic to extract `tenant_id` and `device_id`.
3. Fetches device metadata from auth cache.
4. Builds Standard Event Schema (§7).
5. Publishes to NATS `tenant.{tid}.telemetry.{did}`.
6. For NATS `tenant.*.command.*` messages: reverse path — translates to Mosquitto PUBLISH to the device's command topic.

### 9.2 HTTP Adapter (bundled in `core-api`)

- **Endpoint:** `POST /api/v1/ingress/telemetry`
- **Header:** `Authorization: Bearer {device_token}`
- **Rate limit:** 10 requests/second per `device_id` (token bucket, in-process, no Redis needed).
- **Logic:**
  1. Extract token from header. Look up auth cache.
  2. Bcrypt verify. Resolve `tenant_id` and `device_id`.
  3. Parse JSON body as `payload`.
  4. Build Standard Schema with `protocol_source: "http"`.
  5. `nats.Publish("tenant."+tid+".telemetry."+did, envelope)`.
  6. Return `204 No Content`.

### 9.3 CoAP Adapter (`adapter-coap`)

- **Library:** `go-coap` v3
- **Port:** UDP 5683
- **Resource path:** `/telemetry`
- **Auth:** Token in CoAP Uri-Query option: `?token={device_token}`
- **Logic:** Identical to HTTP adapter — validate from cache, wrap in Standard Schema, publish to NATS.
- **Response:** CoAP `2.04 Changed` on success, `4.01 Unauthorized` on bad token, `4.00 Bad Request` on malformed payload.

### 9.4 WebSocket Adapter (bundled in `core-api`)

- **Endpoint:** `ws://<pi-ip>:8080/ws/telemetry`
- **Upgrade:** Standard HTTP → WebSocket upgrade.
- **Auth handshake:** The first message from the client MUST be:
  ```json
  { "type": "auth", "token": "{device_token}" }
  ```
  If not received within **5 seconds**, the server closes the connection with code `4001`.
- **Subsequent messages:** Each message is treated as a telemetry payload. Wrapped in Standard Schema and published to NATS.
- **Egress:** Server pushes `{"type":"command","payload":{...}}` frames when NATS delivers a command for this device.

### 9.5 Zigbee / BLE (via Zigbee2MQTT — no custom code required)

1. Plug a Sonoff Zigbee USB dongle (or similar) into the Pi.
2. Run `Zigbee2MQTT` as an additional Docker container on the Pi. It translates raw Zigbee frames into MQTT messages on the local Mosquitto broker.
3. The MQTT adapter (§9.1) handles it identically to any ESP32. Zero additional code needed.

### 9.6 Adding Future Protocols (Matter, LoRaWAN, Z-Wave, Thread)
See §22 for the exact procedure. Summary: write a new Go container that validates the device token from auth cache, normalizes to Standard Schema (§7), and publishes to NATS. Everything downstream is unchanged.

---

## 10. Cloud Bridge Service

This is the **most critical service** on the Pi. It is the **only** container with internet access.

### 10.1 Responsibilities

#### A — Auth Cache Sync (every 60 seconds)
```
GET https://api.yourdomain.com/internal/hub/cache-sync
Headers: Authorization: Bearer {HUB_API_KEY}
         X-Hub-ID: {HUB_ID}

Response 200:
{
  "devices": [
    {
      "device_id": "uuid",
      "tenant_id": "stu_01",
      "token_hash": "$2a$12$...",
      "allowed_topics": ["tenant.stu_01.telemetry.sensor_a"],
      "status": "active"
    }
  ],
  "rules": [
    { "rule_id": "uuid", "tenant_id": "stu_01", "rule_json": {...}, "is_active": true }
  ]
}
```
On receipt: upsert all rows into `auth_cache` and `rules_cache` in SQLite. This is a full replacement — removed devices are handled by status field being set to `revoked` in the response, or by the revocation push.

#### B — Telemetry Forward (continuous)
- Consumes from NATS `TELEMETRY` stream, durable consumer `cloud-bridge`, batch up to **100 messages** or **500ms** timeout.
- Sends batch as HTTP POST:
  ```
  POST https://api.yourdomain.com/internal/hub/ingest
  Headers: Authorization: Bearer {HUB_API_KEY}
           Content-Type: application/json

  Body: { "events": [ {Standard Schema}, ... ] }
  ```
- **Only calls `msg.Ack()` after receiving `200 OK` from VPS.** If VPS returns `5xx` or network error, the messages remain in the stream and are retried with exponential backoff.

#### C — Command / WebSocket Channel (persistent)
- Maintains a persistent WebSocket connection to `wss://api.yourdomain.com/hub/ws`.
- Headers: `Authorization: Bearer {HUB_API_KEY}`, `X-Hub-ID: {HUB_ID}`.
- On disconnect: reconnect immediately, then back off (1s, 2s, 4s... max 60s).

**Inbound message types from VPS:**
```json
{ "type": "command",  "tenant_id": "stu_01", "device_id": "uuid", "payload": {"state":"ON"} }
{ "type": "revoke",   "device_id": "uuid" }
{ "type": "ping" }
```

On `command`: publish to NATS `tenant.{tid}.command.{did}`.
On `revoke`: DELETE from `auth_cache` WHERE `device_id = ?`, then publish to NATS `system.cache.invalidate.{device_id}`.
On `ping`: respond with `{"type":"pong"}`.

#### D — Heartbeat (every 30 seconds)
```
POST https://api.yourdomain.com/internal/hub/heartbeat
Body: { "hub_id": "uuid", "uptime_seconds": 3600, "buffer_depth": 42 }
```
VPS sets `hubs.is_online = true` and `hubs.last_ping = NOW()`. If no heartbeat for > 2 minutes, VPS sets `is_online = false` and shows hub as offline on dashboard.

### 10.2 Bridge Configuration (`/config/bridge.yaml`)
```yaml
hub_id:                   "${HUB_ID}"
cloud_api_url:            "${CLOUD_API_URL}"       # https://api.yourdomain.com
hub_api_key:              "${HUB_API_KEY}"
cache_sync_interval_sec:  60
heartbeat_interval_sec:   30
telemetry_batch_size:     100
telemetry_batch_timeout_ms: 500
nats_url:                 "nats://nats:4222"
sqlite_path:              "/data/edge_cache.db"
tls_verify:               true                     # NEVER set false in production
retry_max_interval_sec:   60
```

### 10.3 Resiliency Contract
| Scenario | Behaviour |
|----------|-----------|
| VPS unreachable | Exponential backoff. Telemetry buffered in NATS file stream. Cache stays warm from last sync. |
| Pi reboots | NATS JetStream replays from last acknowledged position. No data lost. |
| Cache TTL expired (> 5 min offline) | New device connections rejected (fail-safe). Existing connected devices depend on broker state. |
| VPS revokes a device | Immediate delete from SQLite cache; NATS invalidation event; next auth check fails. |
| VPS pushes command while Pi is temporarily offline | WSS reconnects. VPS should buffer commands for up to 5 minutes (TTL on cloud side). |

---

## 11. Local Auth Cache Service

A tiny Go HTTP server inside `core-api`. **Binds to `127.0.0.1:8080` only.** Not reachable from outside the Pi.

### Endpoints (internal only)
```
GET /internal/auth/mqtt?username={device_id}&password={token}
  1. SELECT * FROM auth_cache WHERE device_id = ? AND status = 'active'
  2. If not found: return 403
  3. If cached_at older than 300 seconds: return 403 (stale, fail-safe)
  4. bcrypt.CompareHashAndPassword(row.token_hash, token)
  5. If match: return 200 {"ok": true}
  6. If mismatch: INSERT INTO audit log; return 403

GET /internal/acl/mqtt?username={device_id}&topic={topic}&acc={access}
  → Check allowed_topics JSON array from auth_cache
  → acc=1 (read) or acc=2 (write). Return 200 or 403.

GET /internal/auth/http?token={token}
  → Same logic as above but used by HTTP/CoAP adapters

GET /healthz
  → {"status":"ok","sqlite_rows":123,"last_sync_ago_sec":45}
```

### In-Memory LRU Layer
The auth cache service keeps a hot LRU cache (max 1000 entries, TTL 60s) on top of SQLite to avoid disk reads for frequently connecting devices. SQLite is only hit on LRU miss.

---

## 12. Rule Engine

A standalone Go service subscribing to NATS. Runs entirely on the Pi — no cloud round-trip for rule evaluation.

### Rule Definition (JSONLogic format, stored in `rules_cache`)
```json
{
  "rule_id":   "rule_001",
  "tenant_id": "stu_01",
  "is_active": true,
  "trigger": {
    ">": [{ "var": "payload.temperature" }, 30]
  },
  "action": {
    "type":    "command",
    "subject": "tenant.stu_01.command.fan_uuid",
    "message": { "state": "ON" }
  }
}
```

### Evaluation Loop
1. Subscribe to NATS `tenant.*.telemetry.*` (durable consumer `rule-engine`).
2. For each event: look up all active rules for `tenant_id` in `rules_cache`.
3. Evaluate each rule's `trigger` using JSONLogic against the full event envelope.
4. If trigger is true: publish `action.message` to `action.subject` on NATS.
5. Ack the NATS message only after all matching rules have been evaluated.

Rules are reloaded from SQLite every 60 seconds (same as cache sync interval). Rule changes made on the VPS dashboard take effect on the Pi within ~60 seconds.

---

## 13. Cloud API — Endpoint Contract

All endpoints are served by the Go `cloud-api` container on the VPS. Publicly accessible via Caddy at `https://api.yourdomain.com`.

### Public API (JWT auth required for user-facing endpoints)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/v1/auth/login` | None | Login, returns access + refresh JWT |
| `POST` | `/api/v1/auth/refresh` | Refresh JWT | Rotate access token |
| `GET`  | `/api/v1/tenants` | JWT (admin) | List all tenants |
| `POST` | `/api/v1/tenants` | JWT (admin) | Create tenant. Auto-provisions Grafana org. |
| `GET`  | `/api/v1/tenants/{id}` | JWT | Get tenant details |
| `GET`  | `/api/v1/hubs` | JWT (admin) | List all hubs + online status |
| `POST` | `/api/v1/hubs/register` | JWT (admin) | Register new Pi hub. Returns hub API key (shown once). |
| `GET`  | `/api/v1/devices` | JWT | List devices for authenticated tenant |
| `POST` | `/api/v1/devices` | JWT | Provision device. Returns token (shown once). Stores bcrypt hash. |
| `DELETE`| `/api/v1/devices/{id}` | JWT | Revoke device. Pushes revoke via WebSocket to Pi. |
| `GET`  | `/api/v1/telemetry` | JWT | Query telemetry (time range, device filter) |
| `GET`  | `/api/v1/rules` | JWT | List rules for tenant |
| `POST` | `/api/v1/rules` | JWT | Create rule |
| `PUT`  | `/api/v1/rules/{id}` | JWT | Update rule |
| `DELETE`| `/api/v1/rules/{id}` | JWT | Delete rule |
| `POST` | `/api/v1/devices/{id}/command` | JWT | Send command to device via Pi |

### Internal Hub API (Hub API Key auth — Pi-to-VPS only)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/internal/hub/ingest` | Receive telemetry batch from Pi bridge |
| `GET`  | `/internal/hub/cache-sync` | Return devices + rules for this hub |
| `POST` | `/internal/hub/heartbeat` | Hub heartbeat |
| `WebSocket` | `/hub/ws` | Persistent bi-directional hub channel |

### Health
| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Returns `200 OK` with DB and service status |

### JWT Details
- **Algorithm:** HS256, signed with `JWT_SECRET` env var (256-bit random, never committed).
- **Access token expiry:** 15 minutes.
- **Refresh token expiry:** 7 days. Stored in DB, can be revoked.
- **Claims:** `{ "sub": "user_uuid", "tenant_id": "stu_01", "role": "admin|student" }`.

---

## 14. Repository Structure

```
smarthub-os/                        ← monorepo root
│
├── edge/                           ← Everything that runs ON the Raspberry Pi
│   ├── core-api/                   ← Pi-local auth + HTTP/WS ingress (Go)
│   │   ├── main.go
│   │   ├── auth/                   ← SQLite cache, LRU, bcrypt verification
│   │   ├── ingress/                ← HTTP and WebSocket adapter handlers
│   │   └── Dockerfile              ← FROM scratch, compiled Go binary only
│   │
│   ├── bridge-cloud/               ← Cloud Bridge service (Go)
│   │   ├── main.go
│   │   ├── sync/                   ← Cache sync loop
│   │   ├── telemetry/              ← NATS consumer + batch forwarder
│   │   ├── websocket/              ← Persistent WSS connection to VPS
│   │   └── Dockerfile
│   │
│   ├── adapters/
│   │   ├── bridge-mqtt/            ← Mosquitto → NATS forwarder (Go)
│   │   └── adapter-coap/           ← go-coap UDP server (Go)
│   │
│   └── rule-engine/                ← JSONLogic rule evaluator (Go)
│
├── cloud/                          ← Everything that runs on the VPS
│   ├── cloud-api/                  ← REST + WebSocket API (Go)
│   │   ├── main.go
│   │   ├── handlers/               ← HTTP handler functions
│   │   ├── hub/                    ← Internal hub channel (WebSocket)
│   │   ├── db/                     ← PostgreSQL queries
│   │   └── Dockerfile
│   │
│   ├── migrations/                 ← golang-migrate SQL files
│   │   ├── 001_initial_schema.up.sql
│   │   ├── 001_initial_schema.down.sql
│   │   └── 002_timescale_hypertable.up.sql
│   │
│   └── ui/                         ← React management portal
│       ├── src/
│       └── Dockerfile
│
├── deploy/
│   ├── edge/
│   │   ├── docker-compose.yml      ← Pi: all edge containers
│   │   ├── mosquitto.conf          ← Mosquitto + go-auth config
│   │   └── nats.conf               ← JetStream stream definitions
│   │
│   ├── cloud/
│   │   ├── docker-compose.yml      ← VPS: postgres, cloud-api, grafana, caddy, ui
│   │   ├── Caddyfile               ← Reverse proxy config
│   │   └── grafana/
│   │       └── dashboard-template.json   ← Auto-provisioned per tenant
│   │
│   └── scripts/
│       ├── pi-setup.sh             ← First-time Pi setup (Docker, UFW, env, systemd)
│       └── cloud-setup.sh          ← First-time VPS setup
│
└── docs/                           ← All design documents (including this file)
    ├── CONSTITUTION.md             ← THIS FILE — the single source of truth
    ├── smarthub-hld.md
    ├── smarthub-lld.md
    ├── smarthub-dev-guide.md
    └── Project_Detail.md
```

---

## 15. Secrets & Configuration

**Absolute rule: No credentials in code or in the repository. Ever.**

### Edge Pi Secrets (`/opt/smarthub/.env` — `chmod 600`, owned by `root`)
```env
HUB_ID=<uuid assigned when hub was registered on VPS>
HUB_API_KEY=<64-char random string from hub registration>
CLOUD_API_URL=https://api.yourdomain.com
NATS_URL=nats://nats:4222
SQLITE_PATH=/data/edge_cache.db
TLS_VERIFY=true
```

### VPS Cloud Secrets (`/opt/smarthub-cloud/.env` — `chmod 600`, owned by `root`)
```env
POSTGRES_URL=postgres://smarthub:<password>@postgres:5432/smarthub
POSTGRES_PASSWORD=<strong password>
JWT_SECRET=<256-bit random hex string>
GRAFANA_ADMIN_PASSWORD=<strong password>
GRAFANA_URL=http://grafana:3000
GRAFANA_ADMIN_USER=admin
PORT=9090
```

### `.gitignore` must include:
```
.env
.env.*
*.env
/data/
/config/bridge.yaml
```

### Generating secrets:
```bash
# Generate a 64-char random hub API key
openssl rand -hex 32

# Generate a JWT secret
openssl rand -hex 32

# Generate a strong postgres password
openssl rand -base64 24
```

---

## 16. Development Roadmap

Follow phases in order. Do not start Phase N+1 until Phase N milestone is verified.

### Phase 1 — Pi Core Infrastructure (Weeks 1-2)
- Install Raspberry Pi OS Lite 64-bit. Enable SSH. Set static local IP.
- Install Docker + Docker Compose v2.
- Deploy NATS JetStream container with `nats.conf` defining `TELEMETRY` (file storage), `COMMANDS` (memory), `AUDIT` (file) streams.
- **Milestone:** `nats pub test "hello"` and `nats sub test` work. `nats stream info TELEMETRY` shows file-backed persistence survives `docker restart nats`.

### Phase 2 — VPS Cloud Infrastructure (Weeks 2-3, parallel with Phase 1)
- Run `deploy/scripts/cloud-setup.sh` on your VPS.
- Start `deploy/cloud/docker-compose.yml`: PostgreSQL+TimescaleDB, Grafana, Caddy.
- Run DB migrations (`golang-migrate up`).
- Point DNS records (`api.`, `dashboard.`, `app.`) to VPS IP.
- **Milestone:** `https://dashboard.yourdomain.com` loads Grafana login from your phone. TLS cert is valid (green padlock).

### Phase 3 — Cloud API + Hub Registration (Weeks 4-5)
- Build `cloud/cloud-api/` in Go. Implement all endpoints from §13.
- On `POST /api/v1/tenants`: create DB row + call Grafana API to create an Organization for that tenant.
- Device provisioning: generate 32-byte random token, return once, store `bcrypt(token, 12)` hash in DB.
- **Milestone:** Via Postman: create tenant → create hub (get API key) → create device (get token). All records visible in `psql`.

### Phase 4 — Pi Cloud Bridge (Weeks 6-7)
- Build `edge/bridge-cloud/` in Go.
- Implement all 4 responsibilities (§10.1): cache sync, telemetry forward, WebSocket command channel, heartbeat.
- Run on Pi with real VPS URL.
- **Milestone:** Pi shows as `is_online = true` in `hubs` table. Manually insert a NATS message on Pi → it appears in `telemetry` table on VPS within 2 seconds.

### Phase 5 — Local Auth + MQTT Adapter (Weeks 7-8)
- Build `edge/core-api/` auth service: SQLite LRU cache, bcrypt verification, `/internal/auth/mqtt` and `/internal/acl/mqtt` endpoints (localhost only).
- Deploy Mosquitto with `mosquitto-go-auth` pointing to `127.0.0.1:8080`.
- Build `edge/adapters/bridge-mqtt/`: subscribes Mosquitto `#`, normalizes, publishes to NATS.
- **Milestone:** Flash ESP32 with device credentials from Phase 3. Full data flow: ESP32 → MQTT → NATS → Cloud Bridge → VPS TimescaleDB → Grafana dashboard live panel.

### Phase 6 — HTTP & CoAP & WebSocket Adapters (Weeks 9-10)
- Add `POST /api/v1/ingress/telemetry` to `core-api` with token bucket rate limiting.
- Build `edge/adapters/adapter-coap/` using `go-coap`. UDP 5683.
- Add WebSocket handler to `core-api` HTTP server.
- **Milestone:** `curl -X POST http://pi:8080/api/v1/ingress/telemetry -H "Authorization: Bearer {token}" -d '{"temp":25}'` → data visible in VPS Grafana within 2s.

### Phase 7 — Grafana Dashboards Per Tenant (Weeks 11-12)
- Write a Grafana dashboard JSON template (time-series panel: telemetry over time, device status).
- On `POST /api/v1/tenants` in Cloud API: call Grafana HTTP API to:
  1. Create an Organization for the tenant.
  2. Create a user in that org.
  3. Provision the dashboard template against that org, scoped by `tenant_id`.
- **Milestone:** Create a new tenant → a Grafana org and dashboard auto-appear. Tenant user can only see their own data.

### Phase 8 — Rule Engine (Weeks 13-14)
- Build `edge/rule-engine/` in Go. JSONLogic evaluator, loads rules from `rules_cache` SQLite.
- Create a rule via Cloud API `POST /api/v1/rules`.
- Wait for a cache sync (≤60s) or trigger a sync.
- **Milestone:** Physically disconnect the Pi from the internet. Create a temperature event > 30°C on NATS. Verify the rule engine publishes a `command` to the fan device — with zero cloud involvement.

### Phase 9 — React Management UI (Weeks 15+)
- Build `cloud/ui/` in React/TypeScript.
- Views: Login → Hub Overview → Tenant Devices → Add Device → Dashboards (iframe Grafana panels) → Rules.
- All API calls go to `https://api.yourdomain.com`. JWT stored in memory (not localStorage).
- **Milestone:** New student can log in, add a device, get code snippet, flash ESP32, and see live data in their Grafana panel — all via the UI, without ever touching the API directly.

---

## 17. Local Development Setup

You do NOT need a physical Pi to develop. Both stacks run on your PC.

### Prerequisites
```bash
# Windows (PowerShell)
choco install docker-desktop golang nodejs git mosquitto

# Install NATS CLI
go install github.com/nats-io/natscli/nats@latest

# Install golang-migrate
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

### Start Local Cloud Stack
```bash
cd deploy/cloud
cp .env.example .env     # fill in POSTGRES_PASSWORD, JWT_SECRET, GRAFANA_ADMIN_PASSWORD

docker-compose up -d
# Cloud API: http://localhost:9090
# Grafana:   http://localhost:3000  (admin / password from .env)

# Run migrations
migrate -source file://../../cloud/migrations \
        -database "postgres://smarthub:${POSTGRES_PASSWORD}@localhost:5432/smarthub?sslmode=disable" up
```

### Register a Test Hub + Start Local Edge Stack
```bash
# 1. Register a hub in cloud API
curl -s -X POST http://localhost:9090/api/v1/hubs/register \
  -H "Content-Type: application/json" \
  -d '{"name":"local-dev-hub"}' | tee hub.json
# Note hub_id and api_key from the output

# 2. Configure edge stack
cd deploy/edge
cp .env.example .env
# Fill in: HUB_ID=<from above>, HUB_API_KEY=<from above>, CLOUD_API_URL=http://host.docker.internal:9090

docker-compose up -d
# NATS:   nats://localhost:4222
# MQTT:   localhost:1883
# CoAP:   localhost:5683 (UDP)
# HTTP:   http://localhost:8080
```

### Create a Test Device and Send Data
```bash
# Create tenant + device
curl -s -X POST http://localhost:9090/api/v1/tenants -d '{"name":"student1"}' | tee tenant.json
curl -s -X POST http://localhost:9090/api/v1/devices \
  -d "{\"tenant_id\":\"$(jq -r .id tenant.json)\",\"hub_id\":\"<hub_id>\",\"protocol\":\"mqtt\",\"device_name\":\"sensor1\"}" \
  | tee device.json
# Note device_id and token (shown once!)

# Wait 60s for cache sync, or force it:
curl -X POST http://localhost:9090/internal/hub/cache-sync-trigger \
  -H "Authorization: Bearer <HUB_API_KEY>" -H "X-Hub-ID: <HUB_ID>"

# Send MQTT telemetry
mosquitto_pub -h localhost -p 1883 \
  -u "$(jq -r .device_id device.json)" \
  -P "$(jq -r .token device.json)" \
  -t "telemetry" -m '{"temperature":25.5,"humidity":60}'

# Verify arrival in VPS TimescaleDB
psql "postgres://smarthub:${POSTGRES_PASSWORD}@localhost:5432/smarthub" \
  -c "SELECT time, device_id, payload FROM telemetry ORDER BY time DESC LIMIT 3;"
```

---

## 18. Testing Playbook

### Happy Path (device sends telemetry → appears in Grafana)
1. Start both stacks (§17).
2. Create tenant + device + register hub.
3. Wait for cache sync.
4. `mosquitto_pub` with valid credentials.
5. Verify in TimescaleDB.
6. Open Grafana, see the data point.

### Auth Rejection (wrong token)
```bash
mosquitto_pub -h localhost -p 1883 -u "<device_id>" -P "wrongtoken" -t "telemetry" -m '{"temp":1}'
# Expected: Mosquitto rejects connection (RC 5 = Not Authorised)
# Expected: Error logged in NATS system.audit subject
```

### Cross-tenant isolation test
```bash
# Create two tenants: tenantA and tenantB with their own devices
# Try to publish from deviceA's credentials to tenantB's topic
mosquitto_pub -h localhost -p 1883 -u "<deviceA_id>" -P "<tokenA>" \
  -t "tenant/tenantB/deviceB/telemetry" -m '{"evil":"data"}'
# Expected: Mosquitto ACL rejection (RC 5)
```

### Offline resilience test
```bash
# 1. Stop the cloud stack
cd deploy/cloud && docker-compose stop

# 2. Send 10 messages — should all be accepted (cache is warm)
for i in $(seq 1 10); do
  mosquitto_pub -h localhost -p 1883 -u "<device_id>" -P "<token>" \
    -t "telemetry" -m "{\"temp\":$i}"
done

# 3. Check buffer depth
nats consumer info TELEMETRY cloud-bridge

# 4. Restart cloud
cd deploy/cloud && docker-compose start

# 5. Verify all 10 messages appear in telemetry table within ~30s
psql ... -c "SELECT COUNT(*) FROM telemetry WHERE time > NOW() - INTERVAL '5 minutes';"
```

### Rate limit test (HTTP adapter)
```bash
# Send 15 requests in quick succession from the same device
for i in $(seq 1 15); do
  curl -s -o /dev/null -w "%{http_code}\n" \
    -X POST http://localhost:8080/api/v1/ingress/telemetry \
    -H "Authorization: Bearer <token>" \
    -d '{"temp":22}'
done
# Expected: First 10 return 204, subsequent return 429 Too Many Requests
```

### Device revocation test
```bash
# 1. Connect a device
mosquitto_sub -h localhost -p 1883 -u "<device_id>" -P "<token>" -t "cmd" &

# 2. Revoke via Cloud API
curl -X DELETE http://localhost:9090/api/v1/devices/<device_id> \
  -H "Authorization: Bearer <admin_jwt>"

# 3. Check SQLite cache — row should be deleted or status=revoked
sqlite3 /data/edge_cache.db "SELECT status FROM auth_cache WHERE device_id='<device_id>';"

# 4. Try to reconnect — should be rejected immediately (no 5 min wait because
#    WebSocket push triggered instant deletion)
mosquitto_pub -h localhost -p 1883 -u "<device_id>" -P "<token>" -t "telemetry" -m '{"temp":1}'
# Expected: RC 5 — Not Authorised
```

---

## 19. Production Deployment

### VPS First-Time Setup
```bash
# SSH into VPS
ssh root@<vps-ip>

# Run cloud setup script (installs Docker, creates directories, sets up UFW)
curl -fsSL https://raw.githubusercontent.com/your-org/smarthub-os/main/deploy/scripts/cloud-setup.sh | bash

# Create /opt/smarthub-cloud/.env with production values (see §15)
nano /opt/smarthub-cloud/.env
chmod 600 /opt/smarthub-cloud/.env

# Start cloud stack
cd /opt/smarthub-cloud
docker-compose pull && docker-compose up -d

# Run DB migrations
docker-compose exec cloud-api ./migrate -source file://migrations -database "${POSTGRES_URL}" up

# Verify
curl https://api.yourdomain.com/health
```

### VPS UFW Firewall Rules
```bash
ufw default deny incoming
ufw default allow outgoing
ufw allow ssh          # port 22
ufw allow http         # port 80  (Caddy for ACME challenge)
ufw allow https        # port 443 (all traffic via Caddy)
ufw deny 9090          # Cloud API never directly exposed, only via Caddy
ufw deny 5432          # PostgreSQL never exposed externally
ufw deny 3000          # Grafana never directly exposed, only via Caddy
ufw enable
```

### Pi First-Time Setup
```bash
# On the Pi (first time):
curl -fsSL https://raw.githubusercontent.com/your-org/smarthub-os/main/deploy/scripts/pi-setup.sh | bash

# The script does:
#  1. apt update && apt install -y docker.io docker-compose-plugin
#  2. mkdir -p /opt/smarthub /data
#  3. Prompts for: HUB_ID, HUB_API_KEY, CLOUD_API_URL
#  4. Writes /opt/smarthub/.env with chmod 600, owned root
#  5. Pulls docker-compose.yml
#  6. docker-compose up -d
#  7. Configures UFW (see below)
#  8. Creates /etc/systemd/system/smarthub.service for auto-start on reboot
```

### Pi UFW Firewall Rules
```bash
ufw default deny incoming
ufw default allow outgoing
ufw allow ssh

# Local network only (192.168.x.x) — adjust to your actual LAN subnet
ufw allow from 192.168.0.0/16 to any port 1883        # MQTT
ufw allow from 192.168.0.0/16 to any port 5683/udp    # CoAP
ufw allow from 192.168.0.0/16 to any port 8080        # HTTP ingress

# Outbound to internet (outgoing is allowed by default — HTTPS/WSS to VPS)
ufw enable
```

Ports **never exposed to internet on Pi:** 1883, 5683, 8080, 4222 (NATS), 8222 (NATS monitor). The Pi is only reachable from local network devices and makes outbound connections to the VPS.

### Pi Auto-Start (systemd unit)
```ini
# /etc/systemd/system/smarthub.service
[Unit]
Description=SmartHubOS Edge Stack
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/smarthub
EnvironmentFile=/opt/smarthub/.env
ExecStart=/usr/bin/docker compose up -d
ExecStop=/usr/bin/docker compose down
TimeoutStartSec=120

[Install]
WantedBy=multi-user.target
```
```bash
systemctl enable smarthub
systemctl start smarthub
```

### Updating Edge Services (Pi)
```bash
# Pull new images and restart
cd /opt/smarthub
docker-compose pull
docker-compose up -d --no-deps --build <service-name>
# e.g.: docker-compose up -d --no-deps bridge-cloud
```

### Updating Cloud Services (VPS)
```bash
# Pull and restart without downtime
cd /opt/smarthub-cloud
docker-compose pull cloud-api
docker-compose up -d --no-deps cloud-api
# Run new migrations if schema changed
docker-compose exec cloud-api ./migrate -source file://migrations -database "${POSTGRES_URL}" up
```

---

## 20. Health Checks & Observability

### Service Health Endpoints
| Service | Endpoint | Expected Response |
|---------|----------|------------------|
| Cloud API (VPS) | `GET https://api.yourdomain.com/health` | `{"status":"ok","db":"up"}` |
| Core API (Pi) | `GET http://localhost:8080/healthz` | `{"status":"ok","sqlite_rows":N,"last_sync_ago_sec":N}` |
| Cloud Bridge (Pi) | `GET http://localhost:8081/healthz` | `{"status":"ok","ws_connected":true,"buffer_depth":N}` |
| NATS (Pi) | `GET http://localhost:8222/healthz` | `{"status":"ok"}` |
| NATS JetStream (Pi) | `GET http://localhost:8222/jsz` | JetStream stream stats |

### Uptime Monitoring
Set up UptimeRobot (free tier) to check `https://api.yourdomain.com/health` every 1 minute. Alert via email/Telegram if it returns non-200.

### Grafana Alerting
Create a Grafana alert rule:
- **Condition:** No telemetry from any device for a specific hub in the last 10 minutes.
- **Action:** Send notification to admin email.

This catches Pi outages even when the VPS is healthy.

### Checking NATS Buffer Depth (Pi)
```bash
nats consumer info TELEMETRY cloud-bridge
# Look at: Num Pending (messages waiting to be forwarded to VPS)
# If this grows continuously: Cloud Bridge is not reaching VPS
```

### PostgreSQL Backup (VPS)
```bash
# Cron job on VPS: daily backup to local file (add remote copy for real safety)
0 2 * * * pg_dump "${POSTGRES_URL}" | gzip > /backups/smarthub_$(date +%Y%m%d).sql.gz
# Keep 7 days
find /backups -name "*.sql.gz" -mtime +7 -delete
```

---

## 21. CI/CD Pipeline

### GitHub Actions — Edge Services Build (`.github/workflows/edge.yml`)
```yaml
name: Edge CI
on:
  push:
    paths: ['edge/**']
  pull_request:
    paths: ['edge/**']

jobs:
  build-and-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.21'
      - name: Vet
        run: cd edge && go vet ./...
      - name: Test
        run: cd edge && go test -race -cover ./...
      - name: Build core-api image
        run: docker build -t smarthub/core-api:${{ github.sha }} edge/core-api/
      - name: Build bridge-cloud image
        run: docker build -t smarthub/bridge-cloud:${{ github.sha }} edge/bridge-cloud/
      - name: Build bridge-mqtt image
        run: docker build -t smarthub/bridge-mqtt:${{ github.sha }} edge/adapters/bridge-mqtt/
      - name: Build adapter-coap image
        run: docker build -t smarthub/adapter-coap:${{ github.sha }} edge/adapters/adapter-coap/
      - name: Build rule-engine image
        run: docker build -t smarthub/rule-engine:${{ github.sha }} edge/rule-engine/
```

### GitHub Actions — Cloud Services Build (`.github/workflows/cloud.yml`)
```yaml
name: Cloud CI
on:
  push:
    paths: ['cloud/**']
  pull_request:
    paths: ['cloud/**']

jobs:
  build-and-test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: timescale/timescaledb:latest-pg16
        env:
          POSTGRES_PASSWORD: testpass
          POSTGRES_DB: smarthub_test
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.21'
      - name: Run migrations
        run: |
          go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest
          migrate -source file://cloud/migrations \
                  -database "postgres://postgres:testpass@localhost:5432/smarthub_test?sslmode=disable" up
      - name: Test
        run: cd cloud && go test -race -cover ./...
        env:
          POSTGRES_URL: postgres://postgres:testpass@localhost:5432/smarthub_test?sslmode=disable
      - name: Build cloud-api image
        run: docker build -t smarthub/cloud-api:${{ github.sha }} cloud/cloud-api/
```

---

## 22. Adding a Future Protocol

SmartHubOS is designed so that adding a new protocol (Matter, LoRaWAN, Z-Wave, Thread, etc.) requires **zero changes** to existing services. Only a new container is added.

**Steps to add Protocol X:**

1. **Create** `edge/adapters/adapter-x/` Go module.

2. **Implement** the following interface in your adapter:
   ```go
   // Your adapter must do exactly these three things:
   // 1. Accept connections from devices using Protocol X
   // 2. For each message:
   //    a. Extract the device token / credential
   //    b. Call authCache.Validate(deviceToken) → returns tenantID, deviceID, or error
   //    c. If valid: build Standard Event Schema (§7) with protocol_source = "x"
   //    d. nats.Publish("tenant."+tenantID+".telemetry."+deviceID, envelope)
   // 3. Subscribe to NATS tenant.*.command.{my-device-ids} and deliver to device
   ```

3. **Add** the new container to `deploy/edge/docker-compose.yml`:
   ```yaml
   adapter-x:
     image: smarthub/adapter-x:latest
     environment:
       - NATS_URL=nats://nats:4222
       - SQLITE_PATH=/data/edge_cache.db
     volumes:
       - smarthub-data:/data
   ```

4. **Add** to CI workflow in `.github/workflows/edge.yml`.

5. **Update** `devices.protocol` ENUM on VPS PostgreSQL to include the new value:
   ```sql
   ALTER TABLE devices DROP CONSTRAINT devices_protocol_check;
   ALTER TABLE devices ADD CONSTRAINT devices_protocol_check
     CHECK (protocol IN ('mqtt','http','coap','ws','x'));
   ```

6. **Deploy** the new container. Everything else (NATS routing, Cloud Bridge, Rule Engine, Grafana dashboards) works automatically.

---

## 23. Non-Negotiable Rules

These rules must never be violated. They exist to protect correctness, security, and performance.

### Architecture Rules
1. **`tenant_id` is ALWAYS resolved from the validated device token. Never from the request body or payload.** A compromised device could lie. Trust only the bcrypt-verified token.

2. **`hub_id` is ALWAYS resolved from the hub API key credential. Never from any request field.**

3. **No Grafana on the Pi.** Grafana consumes ~300 MB RAM. It lives on the VPS only.

4. **No main PostgreSQL on the Pi.** The Pi runs only `edge_cache.db` (SQLite, < 10 MB). All canonical data is on the VPS.

5. **The Pi makes outbound connections only.** No internet-facing inbound ports on the Pi. The Cloud Bridge connects out to the VPS. The devices connect in from the local network only.

6. **NATS `TELEMETRY` stream MUST use file storage.** Memory-only = data loss on reboot during cloud outage.

7. **All adapters normalize to Standard Schema (§7) before publishing to NATS.** Raw device payloads never enter NATS directly.

### Code Quality Rules
8. **All edge services are Go. No JVM. No Python in the hot path.** Maximum 20 MB RAM per Go service binary.

9. **All edge Docker images use `FROM scratch` or `FROM gcr.io/distroless/static`.** No shell, no package manager, no attack surface in the container.

10. **The local auth API (`core-api`) binds to `127.0.0.1:8080` only.** Validate this in code:
    ```go
    srv := &http.Server{Addr: "127.0.0.1:8080"}
    ```

11. **All cloud-bound TLS calls have `tls_verify: true`.** Never disable TLS verification in production. Not even temporarily.

12. **No credentials in code, git history, or Docker images.** All secrets via environment variables only.

13. **Bcrypt cost factor ≥ 12** for all token and API key hashes.

14. **NATS durable consumers with `AckExplicit` for all data-path subscriptions.** `msg.Ack()` only after successful delivery downstream.

### Operational Rules
15. **Per-device rate limit on HTTP adapter: 10 req/s.** Prevents a single misbehaving device from flooding the system.

16. **Cloud Bridge uses exponential backoff (max 60s) on all retries.** No tight retry loops that could DDoS the VPS.

17. **JWT access token expiry: 15 minutes maximum.** Refresh tokens: 7 days.

18. **Daily PostgreSQL backup** on the VPS. 7-day retention minimum.

19. **Pi RAM hard cap: 512 MB total** across all containers. Any service exceeding its budget (§5) blocks the PR.

20. **Any new protocol = new container only.** If you find yourself modifying NATS routing, the Cloud Bridge, or the Rule Engine to add a protocol, stop. You are doing it wrong. See §22.

---

*SmartHubOS Constitution — Version 1.0 — March 2026*
*Cloud Target: Option A — Self-Hosted VPS*
*This document is the single source of truth. All other documents are supplementary.*

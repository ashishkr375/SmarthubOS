# SmartHubOS: High-Level Design (HLD)

## 1. System Overview
SmartHubOS is an **edge-first, cloud-connected, multi-tenant IoT operating environment**. A Raspberry Pi (or any small ARM/x86 edge computer) acts as the **local edge server** — it accepts all device connections, performs real-time protocol translation, fires local rules, and caches auth state. The **main database and dashboard live in the cloud** (AWS / Azure / GCP / self-hosted VPS), so users can view live data and manage devices from anywhere outside the local network.

### Design Philosophy
- **The Pi is the entry point for devices.** It is stateless with respect to tenants — it does not own the source-of-truth DB.
- **The Cloud is the source of truth.** All tenant records, device registrations, historical telemetry, and dashboards live in the cloud.
- **The Pi stays alive even when cloud is unreachable.** Auth is cached locally, and messages are buffered (NATS JetStream) so no data is lost during connectivity gaps.
- **Adding a new protocol = new container only.** The Core OS never changes.

---

## 2. Architectural Layers

```
┌────────────────────────────────────────────────────────────────────┐
│  CLOUD LAYER  (AWS / Azure / GCP / VPS — outside the network)      │
│                                                                      │
│  ┌──────────────┐  ┌──────────────────┐  ┌──────────────────────┐  │
│  │  PostgreSQL  │  │  TimescaleDB /   │  │  Grafana Cloud /     │  │
│  │  (Main DB)   │  │  InfluxDB Cloud  │  │  Custom Dashboard    │  │
│  │  Tenants,    │  │  (Telemetry,     │  │  (Accessible from    │  │
│  │  Devices,    │  │   History)       │  │   anywhere)          │  │
│  │  ACLs, Rules │  │                  │  │                      │  │
│  └──────┬───────┘  └────────┬─────────┘  └──────────────────────┘  │
│         │                   │                                        │
│  ┌──────▼───────────────────▼─────────────────────────────────┐    │
│  │            Cloud API   (REST + WebSocket)                   │    │
│  │   Auth, Tenant Mgmt, Device Mgmt, Dashboard data feed      │    │
│  └──────────────────────────┬──────────────────────────────────┘    │
│                             │  HTTPS / MQTT over TLS / NATS Leaf    │
└─────────────────────────────┼──────────────────────────────────────┘
                              │  (Outbound connection from Pi)
┌─────────────────────────────┼──────────────────────────────────────┐
│  EDGE LAYER  (Raspberry Pi — on-premises, always-on)               │
│                                                                      │
│  ┌──────────────────────────▼──────────────────────────────────┐   │
│  │                   Cloud Bridge Service                        │   │
│  │  (Buffers NATS events → forwards to Cloud API / IoT Core)   │   │
│  └───────────────────────────┬─────────────────────────────────┘   │
│                              │                                       │
│  ┌───────────────────────────▼─────────────────────────────────┐   │
│  │                     NATS JetStream                            │   │
│  │        (Internal Event Bus — persists to disk on Pi)         │   │
│  └──┬─────────────────┬────────────────────┬────────────────────┘   │
│     │                 │                    │                         │
│  ┌──▼──────┐  ┌───────▼──────┐  ┌─────────▼────┐                  │
│  │ MQTT    │  │  HTTP/WS     │  │  CoAP        │  ...future        │
│  │ Adapter │  │  Adapter     │  │  Adapter     │  adapters         │
│  │(Mosquit)│  │  (Go/FastAPI)│  │ (go-coap)    │                   │
│  └──┬──────┘  └───────┬──────┘  └─────────┬────┘                  │
│     │                 │                    │                         │
│  ┌──▼─────────────────▼────────────────────▼────────────────────┐  │
│  │              Local Auth Cache  (SQLite + in-memory LRU)       │  │
│  │  Stores: device_id → tenant_id → allowed_topics (TTL: 5 min) │  │
│  │  Source of truth is Cloud DB. Cache syncs every 60 seconds.  │  │
│  └───────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  [ ESP32 / Arduino / Sensors — connect via MQTT / HTTP / CoAP ]    │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 3. Architectural Pattern
- **Event-Driven Microservices** deployed as Docker containers on the Pi.
- **NATS JetStream** as the internal event bus (persists to local disk = no message loss during cloud outage).
- **Cloud-first database** — The Pi carries only a local SQLite cache of auth/ACL data synced from cloud PostgreSQL.
- **Outbound-only cloud connection** from the Pi — no inbound ports needed (no port forwarding, no exposed Pi to internet).

---

## 4. Tech Stack Selection

### Edge (Raspberry Pi)
| Component | Technology | RAM Usage (approx) |
|-----------|------------|-------------------|
| OS | Raspberry Pi OS Lite 64-bit | — |
| Containerization | Docker + Docker Compose v2 | ~50 MB |
| Internal Event Bus | NATS JetStream (single binary) | ~15 MB |
| MQTT Broker | Eclipse Mosquitto | ~5 MB |
| Core API / Adapters | Golang binaries | ~20 MB each |
| Local Auth Cache | SQLite (file) + in-memory LRU | ~5 MB |
| Cloud Bridge | Go service (NATS → Cloud) | ~15 MB |
| **Total estimate** | | **~200 MB of 4 GB RAM** |

### Cloud (Provider-agnostic)
| Component | AWS Option | Azure Option | Self-hosted Option |
|-----------|-----------|-------------|-------------------|
| Main Relational DB | RDS PostgreSQL | Azure Database for PostgreSQL | PostgreSQL on VPS |
| Time-Series Telemetry | Timestream / InfluxDB Cloud | Azure Data Explorer | TimescaleDB on VPS |
| Cloud API | ECS / Fargate (Go) | Azure Container Apps | Docker on VPS |
| Dashboard | Managed Grafana | Azure Managed Grafana | Grafana OSS on VPS |
| IoT Message Bridge | AWS IoT Core | Azure IoT Hub | NATS Leafnode on VPS |
| Object Storage (backup) | S3 | Azure Blob | MinIO |

> **Recommendation:** Start with a **$6/mo VPS (Hetzner / DigitalOcean)** running PostgreSQL + TimescaleDB + Grafana OSS + a Go API. When you scale, migrate to AWS RDS + Timestream without touching the Pi code — only the Cloud Bridge's target URL changes.

---

## 5. Security & Multi-Tenancy Strategy

### On the Edge (Pi)
- **Local Auth Cache:** Device tokens are validated against a local SQLite cache (bcrypt hash check). No round-trip to cloud per message.
- **Cache TTL:** 5 minutes. If the Pi can't reach the cloud, existing cached devices keep working.
- **NATS Namespace Isolation:** `tenant.{tenant_id}.#` — a device from tenant A can never publish to tenant B's subject.
- **ACL Enforcement:** `mosquitto-go-auth` queries the local cache (HTTP to Local API) instead of the cloud DB directly.

### On the Cloud
- **JWT Auth** for all human users (dashboard, management API). Short-lived tokens (15 min), refresh tokens stored in DB.
- **Device tokens** are long-lived but can be revoked instantly — the cloud pushes a cache invalidation event to the Pi via the persistent bridge connection.
- **TLS everywhere:** The Cloud Bridge connects to the cloud API over HTTPS/WSS. No plain-text traffic leaves the Pi.
- **Row-Level Security (RLS)** on PostgreSQL: A query run under tenant A's DB role physically cannot return tenant B's rows.

---

## 6. Data Flow: Ingress (Device → Cloud, via Pi)

```
Device (ESP32)
    │
    │ MQTT / HTTP / CoAP  (local network, port 1883/80/5683)
    ▼
Protocol Adapter on Pi
    │ 1. Check Local Auth Cache (SQLite LRU). Hit → proceed. Miss → reject or wait for cache sync.
    │ 2. Normalize to Standard JSON envelope.
    ▼
NATS JetStream (Pi)                         ← persists to disk if bridge is down
    │
    │ subscribed by:
    ├──▶ Local Rule Engine (Pi) — fires instant local commands (e.g., turn fan ON)
    │
    └──▶ Cloud Bridge Service (Pi)
              │
              │ HTTPS POST / MQTT-TLS / NATS Leafnode  (outbound, port 443)
              ▼
         Cloud API
              │
              ├──▶ TimescaleDB / InfluxDB Cloud  (telemetry history)
              └──▶ Grafana / Dashboard  (live WebSocket push to browser)
```

**Key guarantee:** If the internet goes down, the Pi keeps accepting device data, stores it in NATS JetStream on-disk, and replays it to the cloud automatically when the connection is restored.

---

## 7. Data Flow: Egress (Cloud → Device, via Pi)

```
User opens Dashboard (browser, anywhere in the world)
    │ Sends command: "Turn Fan ON"
    ▼
Cloud API
    │ Authenticates JWT, validates ACL, publishes to Cloud NATS / IoT topic
    ▼
Cloud Bridge on Pi  (receives command over persistent WSS/NATS Leaf connection)
    │ Publishes to internal NATS: tenant.{tenant_id}.command.{device_id}
    ▼
MQTT/CoAP Adapter on Pi
    │ Translates and sends to the physical device
    ▼
Device (ESP32) receives command
```

---

## 8. Cloud Provider Integration Options

### Option A: VPS (Recommended to Start)
- One Ubuntu VPS (Hetzner CX21, $6/mo) running: PostgreSQL, TimescaleDB extension, Grafana OSS, Go Cloud API, Caddy (reverse proxy + free TLS).
- Pi connects to `api.yourdomain.com:443` via NATS Leafnode or HTTPS.
- Dashboard at `dashboard.yourdomain.com` — login from anywhere.

### Option B: AWS
- **AWS IoT Core** — accepts MQTT from the Pi's Cloud Bridge. Scales to millions of devices.
- **Amazon RDS (PostgreSQL)** — main DB.
- **Amazon Timestream** — time-series storage for telemetry.
- **Amazon Managed Grafana** — dashboard, integrates with Timestream natively.
- **AWS Secrets Manager** — stores Pi bridge credentials.
- Pi Cloud Bridge connects using the **AWS IoT SDK** (MQTT over TLS with X.509 cert).

### Option C: Azure
- **Azure IoT Hub** — device-to-cloud message ingestion.
- **Azure Database for PostgreSQL** — main DB.
- **Azure Data Explorer (ADX)** — cheap time-series at scale.
- **Azure Managed Grafana** — dashboard.
- Pi Cloud Bridge uses the **Azure IoT Device SDK**.

### Option D: Google Cloud
- **Cloud IoT Core successor (Pub/Sub)** — message ingestion.
- **Cloud SQL (PostgreSQL)** — main DB.
- **BigQuery** — telemetry storage (very cheap at scale).
- **Looker Studio / Grafana** — dashboard.

> **Architecture rule:** The Pi's Cloud Bridge is the **only component that changes** when switching cloud providers. All edge adapters and NATS internals remain identical.

---

## 9. Pi Resource Budget (Always enforce this)
The Pi is a constrained device. Every service added must justify its RAM/CPU cost.

| Service | Max RAM | Max CPU (idle) |
|---------|---------|----------------|
| NATS JetStream | 30 MB | 0.5% |
| Mosquitto | 10 MB | 0.5% |
| Core API + Auth | 40 MB | 1% |
| MQTT Bridge | 15 MB | 0.5% |
| CoAP Adapter | 15 MB | 0.5% |
| Cloud Bridge | 20 MB | 1% |
| Local Cache (SQLite) | 10 MB | 0.2% |
| Rule Engine | 25 MB | 1% |
| **Total (hard cap)** | **< 512 MB** | **< 20% avg** |

All Go binaries. No JVM. No Node.js in the hot path. Grafana does NOT run on the Pi.
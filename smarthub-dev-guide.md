# SmartHubOS: Developer Guide

Welcome to the SmartHubOS development team. This guide covers local setup, cloud setup, repo structure, coding rules, and testing workflow.

---

## 0. Architecture at a Glance (Before You Start)

The Pi is the **edge execution engine**. The cloud is the **source of truth**. Both must be running for the full system to work. You can develop them independently.

```
[Devices] → [Pi: Adapters → NATS → Cloud Bridge] → [Cloud: API → PostgreSQL + TimescaleDB → Grafana]
```

---

## 1. Prerequisites

### For Pi / Edge development
You do NOT need a physical Raspberry Pi to develop. Use your local machine.
- Docker & Docker Compose v2
- Golang v1.21+
- `mosquitto-clients` (`choco install mosquitto` on Windows, `brew install mosquitto` on Mac)
- NATS CLI (`go install github.com/nats-io/natscli/nats@latest`)
- `sqlite3` CLI (for inspecting local cache)

### For Cloud development
- Docker & Docker Compose v2 (for local cloud stack)
- Golang v1.21+
- Node.js v18+ (for the management UI)
- PostgreSQL 16 client (`psql`)
- A domain name (for production TLS). Use `localhost` for development.

---

## 2. Repository Structure (Monorepo)

```text
smarthub-os/
├── edge/                          # Everything that runs ON the Raspberry Pi
│   ├── core-api/                  # Pi-local auth API (localhost:8080 only), Go
│   ├── bridge-cloud/              # Cloud Bridge service, Go
│   ├── adapters/
│   │   ├── bridge-mqtt/           # Mosquitto → NATS forwarder, Go
│   │   ├── adapter-coap/          # CoAP UDP server (go-coap), Go
│   │   └── adapter-http/          # HTTP + WebSocket ingress (bundled with core-api)
│   └── rule-engine/               # Local rule evaluation, Go
│
├── cloud/                         # Everything that runs in the CLOUD
│   ├── cloud-api/                 # REST + WebSocket API (tenant mgmt, hub ingest), Go
│   ├── migrations/                # PostgreSQL schema migrations (golang-migrate)
│   └── ui/                        # React management dashboard
│
├── deploy/
│   ├── edge/
│   │   ├── docker-compose.yml     # Pi deployment: NATS, Mosquitto, all edge services
│   │   └── mosquitto.conf         # Mosquitto config with go-auth plugin
│   ├── cloud/
│   │   ├── docker-compose.yml     # Local cloud stack for dev: Postgres, TimescaleDB, Grafana
│   │   └── Caddyfile              # Reverse proxy config
│   └── scripts/
│       ├── pi-setup.sh            # First-time Pi setup script
│       └── cloud-setup.sh         # VPS first-time setup
│
└── docs/                          # HLD, LLD, this guide, API specs
```

---

## 3. Environment Setup

### 3.1 Local Cloud Stack (for development, on your PC)

```bash
cd deploy/cloud
cp .env.example .env
# Edit .env: set POSTGRES_PASSWORD, GRAFANA_ADMIN_PASSWORD, JWT_SECRET

docker-compose up -d
# Starts: PostgreSQL+TimescaleDB, Grafana, Cloud API (dev mode)
```

Run DB migrations:
```bash
cd cloud/migrations
go run github.com/golang-migrate/migrate/v4/cmd/migrate \
  -source file://. \
  -database "postgres://smarthub:${POSTGRES_PASSWORD}@localhost:5432/smarthub?sslmode=disable" up
```

Cloud API runs at `http://localhost:9090`. Grafana at `http://localhost:3000`.

### 3.2 Local Edge Stack (simulates the Pi, on your PC)

```bash
cd deploy/edge
cp .env.example .env
# Edit .env: set HUB_ID (a UUID), HUB_API_KEY, CLOUD_API_URL=http://localhost:9090

docker-compose up -d
# Starts: NATS JetStream, Mosquitto, core-api, bridge-cloud, bridge-mqtt, adapter-coap
```

NATS at `nats://localhost:4222`. MQTT at `localhost:1883`. CoAP at `localhost:5683`. HTTP ingress at `http://localhost:8080`.

---

## 4. Secrets & Configuration Management

**Rule: No credentials in code. Ever.**

All secrets are injected via environment variables. Docker Compose reads from `.env` files (which are in `.gitignore`).

### Edge `.env` (on the Pi at runtime: `/opt/smarthub/.env`)
```env
HUB_ID=your-hub-uuid-here
HUB_API_KEY=generated-long-random-string
CLOUD_API_URL=https://api.yourdomain.com
NATS_URL=nats://localhost:4222
SQLITE_PATH=/data/edge_cache.db
TLS_VERIFY=true
```

### Cloud `.env` (on the VPS)
```env
POSTGRES_URL=postgres://smarthub:password@localhost:5432/smarthub
TIMESCALE_URL=postgres://smarthub:password@localhost:5432/smarthub
JWT_SECRET=your-256-bit-random-secret
GRAFANA_ADMIN_PASSWORD=strongpassword
PORT=9090
```

### Production Secrets (when moving beyond VPS)
- **AWS:** Use AWS Secrets Manager. Reference in ECS task definition as environment variables.
- **Azure:** Use Azure Key Vault. Inject as container environment variables.
- **Pi (hardened):** Store `.env` in a file owned by `root` with `chmod 600`. The Docker Compose reads it at startup only.

> Never commit `.env` files. The repo's `.gitignore` must include `*.env`, `.env`, `.env.*`.

---

## 5. Coding Standards & Best Practices

### Rule 1: `tenant_id` comes from the token — never the payload
When an adapter receives a telemetry message, the `tenant_id` MUST be resolved from the validated device token (via auth cache lookup). If the raw JSON payload includes a `tenant_id` field, it is ignored entirely.

```go
// CORRECT
tenantID := authCache.Resolve(deviceID)  // from validated token

// WRONG — never do this
tenantID := payload["tenant_id"].(string)
```

### Rule 2: `hub_id` comes from the hub API key — never the request body
The Cloud Bridge authenticates with the cloud using a hub API key. The cloud resolves `hub_id` from that key. A request body claiming a specific `hub_id` is rejected.

### Rule 3: Graceful failure in adapters
If an ESP32 sends malformed JSON:
1. Log error to `system.audit.{tenant_id}` on NATS.
2. Return `400 Bad Request` (HTTP) or appropriate error code (CoAP/MQTT disconnect).
3. Drop the message — do NOT crash the adapter process.

### Rule 4: NATS JetStream resiliency
All services subscribe with **durable consumers** and `AckExplicit`. A service that crashes and restarts continues from where it left off. Never use ephemeral subscribers for anything in the data path.

### Rule 5: Cloud Bridge uses exponential backoff
On any cloud API failure: wait 1s, 2s, 4s, 8s… up to 60s max. Log each retry to `system.audit.hub`. Do not flood the cloud with retries.

### Rule 6: All binaries must be Go, ≤ 20 MB RAM each
No JVM. No Python in the hot path. Every edge service is a compiled Go binary in a minimal `scratch` or `distroless` Docker image.

### Rule 7: Local auth API only on localhost
The Pi's auth service (`core-api`) must bind to `127.0.0.1:8080`. It MUST NOT be accessible from outside the Pi. Validate this in code:
```go
srv := &http.Server{Addr: "127.0.0.1:8080", ...}
```

---

## 6. Testing Locally

### Testing the full edge-cloud pipeline (end-to-end)
```bash
# 1. Start the local cloud stack
cd deploy/cloud && docker-compose up -d

# 2. Register a hub
curl -X POST http://localhost:9090/api/v1/hubs/register \
  -H "Content-Type: application/json" \
  -d '{"name": "dev-hub"}'
# → returns: {"hub_id": "...", "api_key": "..."}

# 3. Copy hub_id and api_key into deploy/edge/.env, then start edge stack
cd deploy/edge && docker-compose up -d

# 4. Create a tenant + device
curl -X POST http://localhost:9090/api/v1/tenants -d '{"name": "test_student"}'
curl -X POST http://localhost:9090/api/v1/devices \
  -d '{"tenant_id": "...", "protocol": "mqtt", "device_name": "sensor1"}'
# → returns token ONCE. Copy it.

# 5. Wait ~60s for the Cloud Bridge to sync the new device into edge cache.
#    (Or force sync: POST http://localhost:9090/internal/hub/cache-sync-trigger)

# 6. Send MQTT telemetry
mosquitto_pub -h localhost -p 1883 -u "device_id" -P "token" \
  -t "telemetry" -m '{"temp": 22}'

# 7. Verify it arrived in the cloud
psql $POSTGRES_URL -c "SELECT * FROM telemetry ORDER BY time DESC LIMIT 5;"
```

### Testing NATS message flow
```bash
nats sub ">"          # subscribe to all subjects — watch messages flow
nats stream ls        # list JetStream streams
nats consumer info TELEMETRY cloud-bridge-consumer
```

### Testing auth cache
```bash
sqlite3 /data/edge_cache.db "SELECT device_id, tenant_id, status, cached_at FROM auth_cache;"
```

### Testing offline resilience
```bash
# 1. Stop the cloud stack
cd deploy/cloud && docker-compose stop

# 2. Send MQTT messages — they should be accepted by Pi (cache is warm)
mosquitto_pub -h localhost -p 1883 -u "device_id" -P "token" -t "telemetry" -m '{"temp": 25}'

# 3. Check NATS buffer depth
nats consumer info TELEMETRY cloud-bridge-consumer

# 4. Restart cloud stack
cd deploy/cloud && docker-compose start

# 5. Verify buffered messages were replayed to cloud DB within ~30s
```

---

## 7. Health Checks & Observability

Each service exposes `GET /healthz` on its management port:
- `core-api`: `http://localhost:8080/healthz`
- `bridge-cloud`: `http://localhost:8081/healthz`

NATS built-in monitoring: `http://localhost:8222/healthz` and `http://localhost:8222/jsz` (JetStream stats).

For production, set up a simple uptime check (UptimeRobot free tier or AWS CloudWatch) that pings the cloud API's `GET /health` every minute and alerts you if the hub goes offline.

---

## 8. Git Workflow & CI/CD

1. Create a feature branch: `git checkout -b feature/coap-adapter` or `fix/cloud-bridge-backoff`.
2. Write unit tests for your module. Aim for 70%+ coverage on auth and bridge logic.
3. Open a Pull Request (PR) against `main`.
4. CI (GitHub Actions) must pass:
   - `go vet ./...` — static analysis
   - `go test ./...` — unit tests
   - `docker build` — ensure each service image builds cleanly
5. Require 1 code review approval.

### GitHub Actions — Edge Service Build
```yaml
# .github/workflows/edge.yml
on: [push, pull_request]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.21' }
      - run: cd edge && go vet ./...
      - run: cd edge && go test ./...
      - run: docker build -t smarthub-edge-api edge/core-api/
      - run: docker build -t smarthub-cloud-bridge edge/bridge-cloud/
```

---

## 9. Pi Deployment (Production)

```bash
# On the Pi (first time):
curl -fsSL https://raw.githubusercontent.com/your-org/smarthub-os/main/deploy/scripts/pi-setup.sh | bash

# This script:
#  1. Installs Docker + Docker Compose
#  2. Creates /opt/smarthub/ directory
#  3. Prompts for HUB_ID, HUB_API_KEY, CLOUD_API_URL
#  4. Writes /opt/smarthub/.env (chmod 600, owned by root)
#  5. Pulls docker-compose.yml from the repo
#  6. Starts all containers: docker-compose up -d
#  7. Configures UFW firewall rules
#  8. Enables docker-compose to auto-start on reboot (systemd unit)
```

### UFW Firewall Rules (Pi)
```bash
ufw default deny incoming
ufw default allow outgoing
ufw allow from 192.168.0.0/16 to any port 1883   # MQTT — local network only
ufw allow from 192.168.0.0/16 to any port 5683/udp  # CoAP — local network only
ufw allow from 192.168.0.0/16 to any port 8080   # HTTP ingress — local network only
ufw allow ssh
ufw enable
```

> Outbound `443` (HTTPS/WSS to cloud) is allowed by `default allow outgoing`. The Pi never opens an inbound port to the internet.
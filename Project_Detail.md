# Part 1: The Multi-Protocol Architecture

The golden rule of SmartHubOS: **The Core OS only speaks ONE language.**
Externally, devices can speak MQTT, HTTP, CoAP, or Zigbee. The moment data enters the Pi, the specific "Protocol Adapter" translates it into a standard JSON payload and pushes it to the **Internal Event Bus (NATS JetStream)**.

The **Pi is an edge execution engine — not a database server.** All tenant records, device provisioning, telemetry history, and user dashboards live in the **cloud**. The Pi caches only what it needs to validate devices locally, so it stays fast and works offline.

### 1. How Protocol Adapters Work
Every protocol adapter is a separate Docker container running on the Pi.

*   **Ingress (Device → Pi → Cloud):** The device sends data via CoAP. The CoAP Adapter validates the token against the local auth cache, wraps the payload in the SmartHubOS Standard JSON schema, and pushes it to NATS JetStream. The Cloud Bridge picks it up from NATS and forwards it to the cloud, where it is stored in the time-series database and shown on the dashboard.
*   **Egress (Cloud → Pi → Device):** A user clicks "Turn on LED" in the cloud dashboard. The cloud API sends a command over a persistent WebSocket to the Pi's Cloud Bridge. The Bridge publishes to NATS. The adapter for that device's protocol (e.g., MQTT) sends the command to the physical device. Round-trip latency: typically < 200ms.

### 2. Handling Specific Protocols

*   **MQTT (The TCP/IP standard):**
    *   *How:* Eclipse Mosquitto on the Pi.
    *   *Integration:* Mosquitto uses `mosquitto-go-auth` plugin. Auth calls hit the Pi's local auth cache (SQLite + LRU), not the cloud. A Go bridge service forwards messages from Mosquitto to NATS.

*   **HTTP/REST (For simple web clients and webhooks):**
    *   *How:* Lightweight Go API server on the Pi.
    *   *Integration:* `POST /api/v1/ingress/telemetry` with `Authorization: Bearer {token}`. Token validated against local cache. Rate limited at 10 req/s per device.

*   **WebSockets (For real-time streaming):**
    *   *How:* Hosted on the same Go HTTP server.
    *   *Integration:* First message must be an auth frame. Good for ESP32 devices or browser-based simulators.

*   **CoAP (Constrained Application Protocol - UDP):**
    *   *How:* `go-coap` library on the Pi. Listens on UDP port 5683.
    *   *Integration:* Token passed in CoAP Uri-Query option. Validates against local cache. Ultra-low overhead for battery-powered sensors.

*   **Zigbee / BLE (Non-IP radio protocols):**
    *   *How:* Requires a USB radio dongle (e.g., Sonoff Zigbee USB) plugged into the Pi.
    *   *Integration:* Run **Zigbee2MQTT** container on the Pi. It translates radio signals to MQTT. SmartHubOS treats it identically to any other MQTT device. Zero custom code needed.

*   **Future protocols (Matter, Thread, LoRaWAN, Z-Wave):**
    *   Write a new Docker container that translates the protocol to the SmartHub Standard JSON, then publishes to NATS. Zero changes to any existing service.

---

# Part 2: The Edge-Cloud Split (Key Architecture Decision)

The most important design change from a purely local hub: **the Pi does not own the database.**

```
┌─────────────────────────┐         ┌──────────────────────────────┐
│     Raspberry Pi        │         │          CLOUD               │
│   (Edge Execution)      │         │  (Source of Truth)           │
│                         │         │                              │
│  ✓ Accept device conn.  │◄───────►│  ✓ Tenant & device records   │
│  ✓ Validate tokens      │  HTTPS  │  ✓ Telemetry history         │
│    (from local cache)   │  / WSS  │  ✓ Dashboard (Grafana)       │
│  ✓ Fire local rules     │         │  ✓ User management           │
│  ✓ Buffer events        │         │  ✓ Device provisioning UI    │
│  ✓ Forward to cloud     │         │  ✓ Remote commands           │
│                         │         │                              │
│  ✗ No Grafana on Pi     │         │  ✗ Cloud never talks         │
│  ✗ No main PostgreSQL   │         │     directly to devices      │
│  ✗ No user management   │         │                              │
└─────────────────────────┘         └──────────────────────────────┘
```

### Why This Split?

| Concern | Answer |
|---------|--------|
| Pi runs out of storage | Telemetry goes to cloud, not Pi disk |
| Pi crashes and data is lost | NATS JetStream buffers on disk; replays to cloud on reconnect |
| User wants to view data from home | Dashboard is on the cloud, accessible anywhere |
| Pi can't reach cloud | Local auth cache keeps devices running for up to 5 min (or until cache expires) |
| Add more Pi hubs in future | Each Pi registers with the cloud. One dashboard shows all hubs |
| Switch cloud providers | Only the Cloud Bridge config changes. Pi internals untouched |

### The Auth Cache: How the Pi Works Offline
1. The Cloud Bridge syncs a local SQLite cache every 60 seconds from the cloud DB.
2. Every device token hash and its allowed topics are stored locally.
3. When a device connects via MQTT/HTTP/CoAP, the Pi validates the token from this local cache — no cloud round-trip, sub-millisecond latency.
4. If cloud goes offline, the Pi keeps working for up to 5 minutes using cached auth. Telemetry is buffered in NATS JetStream on the Pi's SD card and auto-replayed when cloud comes back.
5. If a device is revoked in the cloud dashboard, the revocation is pushed to the Pi instantly via the persistent WebSocket connection.

---

# Part 3: Credential Management & Multi-Tenancy

Tenants (students/projects) exist only in the cloud database. The Pi knows about them only through the synced auth cache.

### The Dynamic Authentication Flow
1. **Cloud DB:** PostgreSQL (cloud-hosted) holds `tenants`, `devices`, `hubs`, and `rules` tables. This is the single source of truth.
2. **Device Provisioning (cloud):** A new device is registered via the cloud dashboard. The cloud generates a random strong token, stores its bcrypt hash in the `devices` table, and shows the token to the user once.
3. **Cache Sync (Pi):** The Cloud Bridge pulls the bcrypt hashes and ACLs for all devices registered to this hub every 60 seconds. Upserts into local `edge_cache.db`.
4. **Connection (Pi):** When an ESP32 connects to Mosquitto with `device_123` / `token`, `mosquitto-go-auth` calls the Pi's local auth API (localhost only), which checks `edge_cache.db` — not the cloud. Returns `200` + ACL or `403`.
5. **ACL Enforcement:** The ACL stored in cache limits the device to `tenant/{tenant_id}/{device_id}/+`. Cross-tenant access is impossible at the NATS subject level too: `tenant.stu_A.*` can never be published by a device authenticated as `stu_B`.

---

# Part 4: Cloud Integration Options

You can point SmartHubOS at any cloud. Only the Cloud Bridge configuration changes — nothing else on the Pi.

### Option A: Self-Hosted VPS (Recommended First Step)
Best for getting started, lowest cost (~$6-12/month).

```
Hetzner CX21 / DigitalOcean Droplet (2 vCPU, 4 GB RAM)
├── PostgreSQL 16 + TimescaleDB extension  (main DB + telemetry)
├── Grafana OSS                             (dashboard, login from internet)
├── Cloud API (Go)                          (REST + WebSocket for Pi bridge)
└── Caddy                                   (reverse proxy, auto TLS via Let's Encrypt)
```

Pi Cloud Bridge connects to `https://api.yourdomain.com` and `wss://api.yourdomain.com/hub/ws`.

### Option B: AWS (Best for scale)
```
├── Amazon RDS (PostgreSQL)      ← main DB
├── Amazon Timestream            ← telemetry time-series
├── AWS IoT Core                 ← receives MQTT from Pi Cloud Bridge
├── Amazon ECS/Fargate           ← Cloud API container
├── Amazon Managed Grafana       ← dashboard (native Timestream integration)
└── AWS Secrets Manager          ← Hub API keys and DB credentials
```
Pi Cloud Bridge uses the **AWS IoT Device SDK** (MQTT over TLS, X.509 certificate per hub).

### Option C: Azure
```
├── Azure Database for PostgreSQL ← main DB
├── Azure Data Explorer (ADX)     ← telemetry (very cheap at scale)
├── Azure IoT Hub                 ← message gateway for Pi
├── Azure Container Apps          ← Cloud API
└── Azure Managed Grafana         ← dashboard
```

### Option D: Google Cloud
```
├── Cloud SQL (PostgreSQL)   ← main DB
├── BigQuery                 ← telemetry history (cheap storage)
├── Cloud Pub/Sub            ← message ingestion from Pi
├── Cloud Run                ← Cloud API
└── Looker Studio / Grafana  ← dashboard
```

### Switching Clouds Later
Because the Cloud Bridge is the only internet-facing service, switching from Option A to Option B later means:
1. Export data from VPS PostgreSQL → import to RDS.
2. Update `bridge.yaml`: change `cloud_api_url` and add AWS IoT cert.
3. Redeploy Cloud Bridge container on Pi.
4. Everything else on the Pi stays identical.

---

# Part 5: The User Experience

### For a Student (local lab)
1. Student connects laptop to lab Wi-Fi and opens `dashboard.yourdomain.com` (cloud URL).
2. Logs in with credentials created by the professor on the cloud portal.
3. Clicks **"Add Device"** → selects protocol → gets a Device ID + Token + pre-filled code snippet.
4. Flashes ESP32, powers it on. The ESP32 connects to the Pi (local network), which is already registered with the cloud.
5. Within seconds, the cloud dashboard shows **"Status: Connected"** and live telemetry begins appearing in Grafana.
6. Student builds dashboards, sets rules — all from the cloud UI, accessible from anywhere.

### For the Professor/Admin (remote)
1. Logs into the cloud dashboard from home.
2. Sees all Pis (hubs) registered, their online/offline status, and how many devices are active.
3. Can provision new devices, revoke tokens, or download telemetry CSV exports — all from outside the lab network.
4. Can set "class rules" that fire on any hub (e.g., send an alert if any sensor reports temperature > 40°C).

---

# Part 6: Detailed Development Roadmap

Follow this sequence strictly. Do not skip phases.

### Phase 1: Pi Core Infrastructure (Weeks 1-2)
*   Install Raspberry Pi OS Lite 64-bit. Enable SSH.
*   Install Docker and Docker Compose v2.
*   Deploy **NATS JetStream** container. Configure a `TELEMETRY` stream with file storage.
*   *Milestone:* `nats pub` and `nats sub` work. JetStream stream persists after container restart.

### Phase 2: Cloud Infrastructure Setup (Weeks 2-3, parallel with Phase 1)
*   Provision a VPS (Hetzner/DigitalOcean) or AWS account.
*   Install PostgreSQL + TimescaleDB. Run initial schema migrations (tenants, devices, hubs, telemetry hypertable).
*   Install Grafana. Connect it to the database.
*   Deploy Caddy with a real domain + free TLS.
*   *Milestone:* You can log into Grafana at `https://dashboard.yourdomain.com` from your phone.

### Phase 3: Cloud API & Hub Registration (Weeks 4-5)
*   Build the Cloud API in **Go**:
    *   `POST /api/v1/tenants` — create tenant.
    *   `POST /api/v1/devices` — provision device, return token once.
    *   `POST /api/v1/hubs/register` — register a new Pi hub, return hub API key.
    *   `POST /internal/hub/ingest` — receive telemetry batch from Pi bridge.
    *   `GET /internal/hub/cache-sync` — return active devices list for a hub.
    *   `WebSocket /hub/ws` — persistent command/revocation channel.
*   *Milestone:* Postman can create a tenant, provision a device, and register a hub. Device data appears in TimescaleDB.

### Phase 4: Pi Cloud Bridge (Weeks 6-7)
*   Build the **Cloud Bridge** Go service on the Pi:
    *   Cache sync (every 60s from `GET /internal/hub/cache-sync`).
    *   Telemetry forwarding (consume NATS `TELEMETRY` stream → batch POST to cloud).
    *   Command WebSocket (receive from cloud, publish to NATS `tenant.*.command.*`).
    *   Heartbeat (every 30s).
    *   Revocation handler (on `{"type":"revoke"}` message → delete from SQLite cache).
*   *Milestone:* Pi publishes a test NATS message. It appears in the cloud TimescaleDB within 1 second.

### Phase 5: MQTT Adapter + Local Auth (Weeks 7-8)
*   Deploy Mosquitto + `mosquitto-go-auth` on Pi.
*   Build the Pi-local auth API (Go, binds to `localhost:8080` only):
    *   `GET /internal/auth/mqtt` — bcrypt check against `edge_cache.db`.
*   Build the MQTT-to-NATS bridge Go service.
*   *Milestone:* Flash an ESP32 with MQTT credentials from Step 3. Data flows: ESP32 → Mosquitto → NATS → Cloud Bridge → Cloud DB → Grafana.

### Phase 6: HTTP & CoAP Adapters (Weeks 9-10)
*   Add `POST /api/v1/ingress/telemetry` to the Pi API. Rate limit: 10 req/s per device.
*   Build `go-coap` adapter service. UDP port 5683. Token in Uri-Query.
*   Build WebSocket adapter on the same HTTP server.
*   *Milestone:* All three send data through the same NATS pipeline. Test with `curl`, a CoAP client, and a WebSocket client.

### Phase 7: Cloud Dashboard UI (Weeks 11-12)
*   Configure Grafana data source pointing to TimescaleDB.
*   Create per-tenant Grafana dashboards using Grafana's **Organizations** feature (one org per tenant = strict data isolation).
*   Auto-provision dashboards when a new tenant is created (Grafana API + dashboard JSON template).
*   *Milestone:* A new student account automatically gets a Grafana dashboard scoped to their namespace. They can see only their data.

### Phase 8: Rule Engine (Weeks 13-14)
*   Build Rule Engine Go service on Pi. Subscribes to `tenant.*.telemetry.*`.
*   Loads rules from `edge_cache.db` (synced from cloud `rules` table every 60s).
*   Evaluates JSONLogic expressions. Fires commands to NATS instantly (no cloud round-trip).
*   *Milestone:* Create a rule in the cloud dashboard: "if temp > 30 → turn fan ON". Verify the Pi fires the command without cloud connectivity.

### Phase 9: Web Management UI (Weeks 15+)
*   React/Vue.js frontend hosted on the cloud (not the Pi).
*   Views: Login → My Hubs → My Devices → Add Device → Dashboards → Rules.
*   Uses the Cloud API exclusively. Embeds Grafana panels via iframe.

---

# Part 7: Key Rules (Non-Negotiable)

1. **`tenant_id` must always come from the validated token, never from the payload body.** An evil device could lie in its JSON. The Pi resolves tenant from the bcrypt-verified device record only.

2. **No Grafana on the Pi.** Grafana is a significant RAM consumer. It lives on the cloud only.

3. **No main PostgreSQL on the Pi.** The Pi runs SQLite for its local cache only — a single file, < 10 MB.

4. **All Go, no Python in the hot path.** Every adapter and the Cloud Bridge MUST be written in Go. Sub-15 MB RAM per service. Python is acceptable only for one-off scripts or the CoAP adapter if go-coap has issues.

5. **The Cloud Bridge is the only internet-facing service on the Pi.** All adapters are local-network only. Never expose NATS or the local auth API port externally.

6. **NATS JetStream file storage for `TELEMETRY` stream.** Memory-only streams lose data on reboot. The `TELEMETRY` stream MUST use file storage.

7. **Standardize the internal JSON envelope.** Every adapter wraps raw device data into the Standard Schema (see LLD §2) before NATS publish. No exceptions.

8. **Port map (UFW rules on Pi):**
    - `1883` TCP — MQTT (Mosquitto), local network only
    - `5683` UDP — CoAP, local network only
    - `80/443` TCP — HTTP API, local network only
    - All outbound: `443` TCP — Cloud Bridge (HTTPS/WSS)
    - Block all inbound from internet. The Pi makes outbound connections only.

*   **Ingress (Device to Hub):** The device sends a payload via CoAP. The CoAP Adapter receives it, validates the credential, wraps the payload in the SmartHubOS JSON schema, and pushes it to the NATS bus.
*   **Egress (Hub to Device):** The student clicks "Turn on LED" in the dashboard. The dashboard sends a command to NATS. The CoAP Adapter listens to NATS, sees a command for its connected device, translates it back to CoAP, and sends it to the device.

### 2. Handling Specific Protocols

*   **MQTT (The TCP standard):**
    *   *How:* Use **Eclipse Mosquitto**.
    *   *Integration:* Mosquitto natively handles the TCP connections. You run a background service that bridges Mosquitto topics to the NATS Event Bus.
*   **HTTP/REST (For simple web clients/webhooks):**
    *   *How:* A lightweight API server (built in FastAPI/Python or Gin/Golang).
    *   *Integration:* Expose an endpoint: `POST /ingress/{device_id}`. The device sends JSON. The server verifies the `Authorization: Bearer <token>` header, then publishes to NATS.
*   **WebSockets (For real-time streaming):**
    *   *How:* Hosted on the same API server as HTTP.
    *   *Integration:* Good for devices that need a persistent, low-latency connection but can't use MQTT (e.g., a web browser running a simulation, or specific ESP32 setups).
*   **CoAP (Constrained Application Protocol - UDP based):**
    *   *How:* CoAP is like HTTP but over UDP for extremely low-power devices.
    *   *Integration:* You write a CoAP server using a library like `aiocoap` (Python) or `go-coap` (Go). It listens on UDP port 5683. It extracts the payload, checks the CoAP security options (DTLS or tokens), and translates it to NATS.
*   **Zigbee / BLE (Non-IP Hardware Protocols):**
    *   *How:* These require physical radio dongles (e.g., a Sonoff Zigbee USB dongle) plugged into the Pi.
    *   *Integration:* Don't write this from scratch. Run the open-source **Zigbee2MQTT** container. It translates the raw radio waves into MQTT. Your SmartHubOS simply treats it like any other MQTT device!

---

# Part 2: Credential Management & Multi-Tenancy (Under the Hood)

To isolate students, you need a central **Identity Management Service**. You cannot manually create users in Mosquitto or htpasswd files. 

### The Dynamic Authentication Flow
1.  **The Database:** You run a lightweight relational DB (like PostgreSQL or SQLite) holding tables for `Students`, `Projects/Namespaces`, and `Devices`.
2.  **The Dynamic Auth Plugin:** You configure Mosquitto (and your other adapters) to use a dynamic auth plugin (e.g., `mosquitto-go-auth`).
3.  **The Magic:** When an ESP32 tries to connect to MQTT with username `device_123` and password `abc`, Mosquitto does *not* look at a text file. It makes an internal HTTP call to your API or queries the database directly: *"Is this username/password valid, and what topics can it read/write?"*
4.  **ACL Enforcement:** The DB returns an Access Control List (ACL) saying: *"Yes, device_123 belongs to Student_A. It can only publish to the topic `classA/studentA/device_123/telemetry`."* If the device tries to publish to `studentB`, Mosquitto instantly drops the connection.

---

# Part 3: The Student Experience (UX)

Here is exactly how a student interacts with SmartHubOS in a classroom lab.

### Step 1: Login & Project Creation
*   The student connects their laptop to the classroom Wi-Fi.
*   They navigate to the local Hub URL (e.g., `http://smarthub.local`).
*   They log in (using credentials provided by the professor, or SSO).
*   They click **"Create Project"** and name it "WeatherStation". The system assigns them a namespace: `namespace: stu_weather_01`.

### Step 2: Device Provisioning
*   The student clicks **"Add Device"**.
*   They select a protocol from a dropdown: *MQTT, HTTP, or CoAP*.
*   The UI generates a unique **Device ID** and a **Secure Token/Password**.
*   *Crucially*, the UI provides the student with a **Code Snippet** (C++ for Arduino IDE, or MicroPython) pre-filled with their credentials, Wi-Fi SSID, and Hub IP address.

### Step 3: Flashing & Connecting
*   The student copies the code, flashes it to their ESP32, and powers it on.
*   The ESP32 connects to the hub. The Hub's dashboard instantly shows a green indicator: **"Status: Connected"**.

### Step 4: Visualizing Data
*   The student navigates to the "Dashboard" tab.
*   Behind the scenes, SmartHubOS has auto-provisioned an embedded **Grafana dashboard** (or a custom React UI) tightly scoped to their namespace.
*   They drag and drop a line chart and bind it to their `temperature` telemetry. They cannot see any other student's data.

---

# Part 4: Detailed Development Roadmap

If you are building this, follow this exact sequence to avoid getting overwhelmed.

### Phase 1: Core Infrastructure (Weeks 1-2)
*   **Hardware:** Raspberry Pi 4/5 installed with Raspberry Pi OS Lite (64-bit).
*   **Containerization:** Install Docker and Docker Compose.
*   **Message Bus:** Deploy the NATS server container.
*   **Database:** Deploy PostgreSQL.
*   *Milestone:* You can manually push a message to NATS and read it from another terminal.

### Phase 2: The Core API & Identity Layer (Weeks 3-5)
*   **Language Choice:** Use **Golang** or **Python (FastAPI)** for the core API. 
*   **Features to build:**
    *   User login/registration API.
    *   Device generation API (creates random strong passwords and stores bcrypt hashes in the DB).
    *   Namespace generator.
*   *Milestone:* You can hit the API via Postman, create a user, and generate a device credential.

### Phase 3: The MQTT Adapter & Dynamic Auth (Weeks 6-8)
*   Deploy **Eclipse Mosquitto**.
*   Install and configure the `mosquitto-go-auth` plugin to connect to your PostgreSQL database.
*   Write a background script (The MQTT-to-NATS Bridge) that listens to Mosquitto and forwards incoming JSON payloads to the NATS bus.
*   *Milestone:* You can flash an ESP32, connect to Mosquitto, and the payload ends up on the internal NATS bus. Access is blocked if wrong credentials are used.

### Phase 4: HTTP & CoAP Adapters (Weeks 9-10)
*   Add a `POST /telemetry` endpoint to your core API. It must validate the API key and push to NATS.
*   Write a simple Python script using `aiocoap`. It listens for UDP packets, validates the token in the CoAP options, and pushes to NATS.
*   *Milestone:* You can send data via `curl` (HTTP) or a CoAP client, and it arrives on the same NATS bus as the MQTT data.

### Phase 5: The Web UI / Student Interface (Weeks 11-13)
*   Build a frontend using **React.js** or **Vue.js**.
*   Create the views: Login -> Dashboard -> Device List -> Add Device.
*   Use standard REST API calls to your Core API to fetch data.
*   *Shortcut for charts:* Instead of coding charts from scratch, embed Grafana panels into your UI using iframes, passing the student's namespace as a URL parameter.

### Phase 6: Storage & Automation (Weeks 14+)
*   Deploy **InfluxDB** or **VictoriaMetrics** alongside PostgreSQL.
*   Write a "Storage Engine Service": It listens to NATS and saves all telemetry into InfluxDB for historical viewing.
*   Build the Rule Engine: A service that listens to NATS, evaluates simple JSON rules (e.g., `if temp > 30 then send MQTT command ON to Fan`), and executes them.

---

### Key Architectural Advice
1.  **Keep Payloads Standardized:** 
    Force all your adapters to wrap incoming data into an internal envelope. For example, even if CoAP sends just `25.4`, your adapter should turn it into:
    ```json
    {
      "hub_time": 1699999999,
      "protocol": "coap",
      "tenant_id": "stu_01",
      "device_id": "sensor_A",
      "payload": 25.4
    }
    ```
2.  **Avoid Port Conflicts:** MQTT uses `1883`, HTTP uses `80/443`, CoAP uses `5683`. Make sure your Raspberry Pi firewall (UFW or IPTables) is configured correctly to allow these specific ports.

By strictly separating the **Protocol Adapters** (which talk to devices) from the **Core System** (which uses NATS internally), you make SmartHubOS infinitely scalable. If next year you want to add support for the new "Matter" protocol, you don't touch the core system at all—you just write a new Matter Adapter container!
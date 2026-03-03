<!--
SYNC IMPACT REPORT
Version change: UNVERSIONED (blank template) → 1.0.0
Modified principles:
  - [PRINCIPLE_1_NAME] → I. Edge-First, Cloud-Authoritative
  - [PRINCIPLE_2_NAME] → II. Zero-Trust Device Authentication
  - [PRINCIPLE_3_NAME] → III. Offline-Resilient Event Pipeline
  - [PRINCIPLE_4_NAME] → IV. Protocol Plug-In Architecture
  - [PRINCIPLE_5_NAME] → V. Minimal Footprint + Security Hardening
Added sections:
  - Technology Stack (hard numeric constraints, full tech-stack declaration)
  - Development Workflow (9-phase roadmap + spec-kit workflow + Constitution Check gate list)
Removed sections: None — template structure preserved
Templates requiring updates:
  ✅ .specify/templates/plan-template.md — Constitution Check gate list populated with SmartHubOS-specific checks
  ✅ .specify/templates/spec-template.md — No structural changes required (generic user-story structure is compatible)
  ✅ .specify/templates/tasks-template.md — No structural changes required (phase structure is compatible)
Follow-up TODOs: None — all placeholders resolved.
-->

# SmartHubOS Constitution

## Core Principles

### I. Edge-First, Cloud-Authoritative

The Raspberry Pi is the device gateway; the VPS is the source of truth. All canonical
data (tenants, devices, telemetry history, rules) MUST live on VPS PostgreSQL. The Pi
MUST only hold: NATS JetStream file buffers, SQLite auth/rules cache (< 10 MB), and
compiled Go binaries. No Grafana on the Pi. No main PostgreSQL on the Pi. No inbound
internet ports on the Pi.

The cloud dashboard MUST be accessible from any browser, anywhere. The Pi makes
outbound connections only — the Cloud Bridge connects out to the VPS; devices connect
in from the local LAN only. This boundary is non-negotiable.

### II. Zero-Trust Device Authentication

`tenant_id` MUST be resolved from the validated device token — never from the request
body or payload. `hub_id` MUST be resolved from the hub API key credential — never
from any request field. A compromised device must not be able to claim another
tenant's identity.

All token hashes MUST use bcrypt cost ≥ 12. Plaintext tokens MUST never be stored,
re-returned after initial provisioning, or written to logs. The local auth cache MUST
serve bcrypt comparisons without internet round-trips (cache TTL = 300 s, fail-safe:
expired → deny). On revocation the VPS MUST push an instant WebSocket delete to the
Pi; the Pi MUST delete the row from SQLite immediately on receipt.

### III. Offline-Resilient Event Pipeline

The NATS `TELEMETRY` stream MUST use **file** storage. Using memory storage is a
breaking violation — a Pi reboot during a cloud outage loses all buffered data.

NATS durable consumers MUST use `AckExplicit`. `msg.Ack()` MUST be called only after
confirmed downstream delivery. The Cloud Bridge MUST apply exponential backoff (max
60 s) on all retries — no tight retry loops. The system MUST keep accepting device
connections and executing local automation rules for up to 5 minutes while the VPS
is unreachable (bounded by the auth cache TTL).

### IV. Protocol Plug-In Architecture

Adding a new device protocol MUST require only a new container — zero changes to
NATS routing, Cloud Bridge, Rule Engine, or Cloud API. All protocol adapters MUST
normalize inbound device data to the Standard Event Schema (see `CONSTITUTION.md` §7)
before publishing to NATS. Raw device payloads MUST never enter NATS directly.

New protocol checklist: (1) validate token from auth cache, (2) build Standard Event
Schema with `protocol_source` set to the new protocol name, (3) publish to
`tenant.{tid}.telemetry.{did}`, (4) subscribe to `tenant.{tid}.command.{did}` for
egress, (5) add as a new service in `deploy/edge/docker-compose.yml` only.

### V. Minimal Footprint + Security Hardening

All edge services MUST be written in Go with a hard RAM cap of 20 MB per binary.
Total edge RAM across all containers MUST remain below 512 MB. All edge Docker images
MUST use `FROM scratch` or `FROM gcr.io/distroless/static` — no shell, no package
manager, minimal attack surface.

The Pi local auth API MUST bind to `127.0.0.1:8080` only (enforced in code, not
just configuration). TLS verification MUST be `true` in all cloud-bound connections —
never disabled in any environment. No secrets in code, git history, or Docker images;
all credentials via environment variables only.

## Technology Stack

**Edge (Raspberry Pi):**
Go 1.21+, NATS JetStream 2 (file storage), Eclipse Mosquitto 2 + mosquitto-go-auth,
SQLite (auth & rules cache only), Docker Compose v2.
All images: `FROM scratch` / `gcr.io/distroless/static`.

**Cloud (VPS — Option A confirmed: Hetzner CX21 / DigitalOcean Basic Droplet):**
Go 1.21+ (cloud-api), PostgreSQL 16 + TimescaleDB extension, Grafana OSS,
React 18 + TypeScript (UI), Caddy 2 (reverse proxy, auto TLS via Let's Encrypt).
Infrastructure: Ubuntu 22.04 LTS, Docker Compose v2.

**Hard constraints (any PR that breaks these is blocked):**
- Total edge RAM: MUST be < 512 MB across all containers.
- Per edge Go binary: MUST be ≤ 20 MB RAM.
- HTTP ingress rate limit: 10 req/s per `device_id`, token-bucket, in-process (no Redis).
- JWT access token: MUST expire in ≤ 15 minutes.
- JWT refresh token: MUST expire in ≤ 7 days.
- Telemetry retention: 90 days raw (then dropped); continuous hourly aggregates kept indefinitely.
- NATS `TELEMETRY` stream `max_msgs: 50 000` — hard ceiling on the offline buffer.

**Full reference:** `CONSTITUTION.md` in the project root contains all SQL schemas,
API contracts, NATS routing map, Docker Compose files, secrets layout, and deployment
procedures. It is the single source of truth for implementation detail.

## Development Workflow

Phases execute in strict dependency order. Phase N+1 MUST NOT begin until the Phase N
milestone is verified and committed.

| Phase | Scope | Key Milestone |
|-------|-------|---------------|
| 1 | Pi Core Infra | NATS `TELEMETRY` stream survives `docker restart nats` with data intact |
| 2 | VPS Cloud Infra | `https://dashboard.yourdomain.com` loads Grafana with valid TLS cert |
| 3 | Cloud API | Tenant → Hub → Device provisioning roundtrip verified in Postman |
| 4 | Cloud Bridge | Pi shows `is_online=true` in DB; telemetry flows Pi → VPS in < 2 s |
| 5 | MQTT + Auth Cache | ESP32 → MQTT → TimescaleDB → Grafana live panel |
| 6 | HTTP / CoAP / WS | All three adapters authenticate and publish to NATS |
| 7 | Grafana per-tenant | New tenant automatically provisions Grafana org + dashboard |
| 8 | Rule Engine | Rule fires locally with Pi fully disconnected from VPS |
| 9 | React UI | Student adds device + sees live data — entirely via the UI |

**Spec-kit workflow (one git branch per feature):**

```text
git checkout -b feature/<name>
/speckit.specify  — draft user stories and requirements
/speckit.plan     — technical design, data model, contracts
/speckit.tasks    — ordered implementation task list
/speckit.implement — build, test, PR to main
```

**Constitution Check (required gate on every `plan.md` before Phase 0 research):**

- [ ] No main PostgreSQL or Grafana proposed for the Pi
- [ ] `tenant_id` resolved from validated credential, never from request body or payload
- [ ] `hub_id` resolved from hub API key credential, never from request field
- [ ] NATS `TELEMETRY` stream configured with file storage (not memory)
- [ ] All adapters normalize to Standard Event Schema before publishing to NATS
- [ ] New protocol = new container only; no changes to existing services
- [ ] Edge images use `FROM scratch` or distroless; no shell in container
- [ ] Pi auth API bound to `127.0.0.1:8080` only (enforced in code)
- [ ] No credentials in code or committed `.env` files
- [ ] bcrypt cost ≥ 12 for all token and API-key hashes
- [ ] NATS consumers use `AckExplicit`; `msg.Ack()` called only after confirmed delivery
- [ ] Cloud Bridge uses exponential backoff (max 60 s) on all retries

## Governance

This constitution supersedes all supplementary documents (`smarthub-hld.md`,
`smarthub-lld.md`, `smarthub-dev-guide.md`, `Project_Detail.md`). For full technical
schemas, API contracts, and deployment procedures, refer to `CONSTITUTION.md` in the
project root — that document is the single source of truth for implementation detail.

When this file conflicts with `CONSTITUTION.md` on technical specifics,
`CONSTITUTION.md` wins. When it conflicts on process and governance, this file wins.

**Amendment procedure:**
1. Open a PR with a `docs:` prefix; describe the principle or constraint being changed.
2. Classify the version bump: MAJOR (breaking — principle removal or redefinition),
   MINOR (new principle or section added), PATCH (clarification or wording fix).
3. Update the `Last Amended` date and version line before merging.
4. Update `plan-template.md` Constitution Check checklist if gates are added or removed.
5. Update `CONSTITUTION.md` if the amendment affects technical implementation detail.

**Compliance review:** Every PR touching edge or cloud services MUST self-certify
against the Constitution Check checklist in `Development Workflow` above. PRs that
violate any principle in this document are blocked until the violation is resolved.

**Versioning policy:** Semantic versioning — MAJOR.MINOR.PATCH.
MAJOR: backward-incompatible governance or principle change.
MINOR: new principle or section added.
PATCH: clarification, wording, or non-semantic refinement.

**Version**: 1.0.0 | **Ratified**: 2026-03-03 | **Last Amended**: 2026-03-03

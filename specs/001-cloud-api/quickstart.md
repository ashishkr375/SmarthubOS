# Cloud API — Developer Quickstart

Get the Cloud API running locally in under 10 minutes.

**Prerequisites**: Go 1.21+, Docker + Docker Compose v2, `psql` (optional, for inspection).

---

## 1. Clone and navigate

```sh
git clone https://github.com/ashishkr375/SmarthubOS.git
cd SmarthubOS
git checkout 001-cloud-api
```

---

## 2. Configure environment

```sh
cd deploy/cloud
cp .env.example .env
```

Open `.env` and fill in the required values:

```env
# PostgreSQL (TimescaleDB)
POSTGRES_USER=smarthub
POSTGRES_PASSWORD=changeme_strong_password
POSTGRES_DB=smarthubos
DATABASE_URL=postgres://smarthub:changeme_strong_password@postgres:5432/smarthubos

# Cloud API
JWT_SECRET=<run: openssl rand -hex 32>
ADMIN_EMAIL=admin@smarthubos.online
ADMIN_PASSWORD=<bcrypt hash or plaintext for dev>
PORT=9090
AUTO_MIGRATE=true

# Grafana (leave blank to skip Grafana provisioning in local dev)
GRAFANA_URL=http://grafana:3000
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASS=admin
```

> **Security**: Never commit `.env`. It is gitignored. Generate `JWT_SECRET` with:
> ```sh
> openssl rand -hex 32
> ```

---

## 3. Start the stack

```sh
docker compose up -d
```

This starts:
- `postgres` — TimescaleDB on port `5432` (localhost only)
- `grafana` — Grafana OSS on port `3000` (localhost only)

> The `cloud-api` service is not in `docker-compose.yml` yet — it will be added during
> `/speckit.implement`. For now, run it directly with `go run`.

---

## 4. Apply migrations (first time only, if AUTO_MIGRATE is false)

```sh
# Install golang-migrate CLI (once)
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Apply migrations
migrate -path cloud/migrations -database "$DATABASE_URL" up
```

If `AUTO_MIGRATE=true` in `.env`, migrations run automatically when `cloud-api` starts.

---

## 5. Run the Cloud API

```sh
cd cloud/cloud-api
go mod tidy
go run ./...
```

The server starts on `http://localhost:9090`. You should see:

```
{"level":"info","ts":"...","msg":"cloud-api started","port":"9090"}
```

---

## 6. Smoke test with curl

### Health check
```sh
curl http://localhost:9090/health
# → {"status":"ok","db":"up"}
```

### Login (admin)
```sh
curl -s -X POST http://localhost:9090/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@smarthubos.online","password":"<your_admin_password>"}' | jq
# → {"access_token":"eyJ...","refresh_token":"a9f4e2b81c..."}
```

Save the token:
```sh
TOKEN=$(curl -s -X POST http://localhost:9090/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@smarthubos.online","password":"<your_admin_password>"}' \
  | jq -r .access_token)
```

### Create a tenant
```sh
curl -s -X POST http://localhost:9090/api/v1/tenants \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Student Lab 01","namespace":"lab01"}' | jq
# → {"id":"...","name":"Student Lab 01","namespace":"lab01","is_active":true,...}
```

### Register a hub
```sh
curl -s -X POST http://localhost:9090/api/v1/hubs/register \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"pi-01"}' | jq
# → {"hub_id":"...","name":"pi-01","api_key":"a9f4e2b81c..."}
# Save api_key — it is shown only once.
```

### Provision a device
```sh
curl -s -X POST http://localhost:9090/api/v1/devices \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"hub_id":"<hub_id>","device_name":"sensor-1","protocol":"mqtt"}' | jq
# → {"device_id":"...","token":"..."}
# Save token — shown only once.
```

### Hub: cache sync
```sh
curl -s http://localhost:9090/internal/hub/cache-sync \
  -H "Authorization: Bearer <hub_api_key>" | jq
# → {"devices":[...],"rules":[...],"synced_at":"..."}
```

### Hub: ingest telemetry
```sh
curl -s -X POST http://localhost:9090/internal/hub/ingest \
  -H "Authorization: Bearer <hub_api_key>" \
  -H "Content-Type: application/json" \
  -d '{
    "events": [{
      "event_id": "550e8400-e29b-41d4-a716-446655440000",
      "timestamp": 1709500000,
      "hub_id": "<hub_id>",
      "tenant_id": "<tenant_id>",
      "device_id": "<device_id>",
      "protocol_source": "mqtt",
      "event_type": "telemetry",
      "payload": {"temperature": 24.5, "humidity": 60}
    }]
  }'
# → HTTP 204 No Content
```

Verify rows in TimescaleDB:
```sh
docker exec -it smarthubos-postgres-1 psql -U smarthub -d smarthubos \
  -c "SELECT time, device_id, payload FROM telemetry ORDER BY time DESC LIMIT 5;"
```

---

## 7. Run tests

```sh
cd cloud/cloud-api
go test -race -cover ./...
```

Integration tests use `testcontainers-go` and spin up a real PostgreSQL container.
They require Docker to be running.

---

## 8. Build Docker image (optional)

```sh
cd cloud/cloud-api
docker build -t cloud-api:dev .
```

Verify the image is under 20 MB (SC-006):
```sh
docker image ls cloud-api:dev
```

---

## Environment Variable Reference

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | YES | — | Full Postgres DSN (pgx format) |
| `JWT_SECRET` | YES | — | HS256 signing secret (≥ 32 bytes hex) |
| `PORT` | No | `9090` | HTTP listen port |
| `AUTO_MIGRATE` | No | `false` | Run DB migrations on startup |
| `GRAFANA_URL` | No | `""` | Base URL of Grafana. If blank, provisioning is skipped |
| `GRAFANA_ADMIN_USER` | No | `admin` | Grafana HTTP API admin user |
| `GRAFANA_ADMIN_PASS` | No | `admin` | Grafana HTTP API admin password |
| `ADMIN_EMAIL` | YES | — | Bootstrapped admin user email |
| `ADMIN_PASSWORD` | YES | — | Bootstrapped admin user password (plaintext, hashed on first start) |

---

## Troubleshooting

**`FATAL: password authentication failed`**
- Check `POSTGRES_PASSWORD` matches `DATABASE_URL`.

**`migrate: no change`**
- Migrations already applied. Run `migrate … version` to see current version.

**`dial tcp …:5432: connection refused`**
- Postgres container not started, or `DATABASE_URL` points to wrong host.
  Use `postgres` (container service name) inside Docker; `localhost` for `go run` on host.

**TimescaleDB extension not found**
- The image must be `timescale/timescaledb:latest-pg16`, not stock `postgres:16`.
  Check `docker-compose.yml`.

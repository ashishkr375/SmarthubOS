# SmarthubOS Development Guidelines

Auto-generated from all feature plans. Last updated: 2026-03-03

## Active Technologies

- Go 1.21+ (001-cloud-api)
- PostgreSQL 16 + TimescaleDB (001-cloud-api)

## Key Dependencies (001-cloud-api)

| Package | Version | Purpose |
|---|---|---|
| `go-chi/chi` | v5 | HTTP router (route groups + middleware) |
| `gorilla/websocket` | v1.5 | WebSocket hub manager (`/hub/ws`) |
| `golang-jwt/jwt` | v5 | JWT HS256 auth (`WithValidMethods` hardened) |
| `jackc/pgx` | v5 (pgxpool) | PostgreSQL driver + batch ingest |
| `golang-migrate/migrate` | v4 | SQL migration runner |
| `uber-go/zap` | latest | Structured logging |
| `testcontainers/testcontainers-go` | latest | Integration tests (real PG in Docker) |
| `golang.org/x/time/rate` | latest | Token bucket rate limiting (in-process) |

## Project Structure

```text
cloud/
  cloud-api/           ← Go service (this feature)
    main.go
    config/            ← env var loading
    db/queries/        ← pgx query functions
    auth/              ← bcrypt.go, jwt.go
    handlers/          ← chi HTTP handlers
    hub/               ← WebSocket hub manager (sync.Map)
    internal/          ← middleware (JWT, HubAPIKey, RateLimit)
    grafana/           ← Grafana org provisioning client
    Dockerfile
  migrations/          ← golang-migrate SQL files

deploy/
  cloud/
    docker-compose.yml ← postgres + grafana (+ future cloud-api)
    .env.example

specs/
  001-cloud-api/
    spec.md
    plan.md
    research.md
    data-model.md
    contracts/openapi.yaml
    quickstart.md
```

## Commands

```sh
# Run Cloud API locally
cd cloud/cloud-api && go run ./...

# Run tests (requires Docker for testcontainers)
cd cloud/cloud-api && go test -race -cover ./...

# Apply DB migrations
migrate -path cloud/migrations -database "$DATABASE_URL" up

# Start cloud infra (TimescaleDB + Grafana)
cd deploy/cloud && docker compose up -d
```

## Code Style

### Go
- `tenant_id` is ALWAYS resolved from JWT claims — never from request body.
- `hub_id` is ALWAYS resolved from hub API key credential — never from any request field.
- Secrets: bcrypt cost = 12 hardcoded in `auth/bcrypt.go`. Never pass cost as a parameter.
- TLS: never disabled, not even in tests.
- Error responses: always `{"error":"<message>"}` format. No stack traces in responses.
- Logging: structured zap, no `fmt.Println`.
- Context: pass `context.Context` as first argument in all DB and HTTP calls.
- Rate limiting: `golang.org/x/time/rate` token bucket per `device_id`, in-process `sync.Map`. No Redis.

## Security Rules (non-negotiable, from CONSTITUTION.md §23)

1. `token_hash` and `api_key_hash` are NEVER returned by any public API endpoint.
2. Tokens returned once only (on creation). Not stored in redis, not logged.
3. JWT algorithm is HS256 only — validate with `jwt.WithValidMethods([]string{"HS256"})`.
4. All Docker images: `FROM scratch` + copy ca-certificates only.
5. All cloud-bound TLS connections: `InsecureSkipVerify = false`.

## Recent Changes

- 001-cloud-api: Added Go 1.21+, PostgreSQL 16 + TimescaleDB, full cloud API layer

<!-- MANUAL ADDITIONS START -->
<!-- MANUAL ADDITIONS END -->

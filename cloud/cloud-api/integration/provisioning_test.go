//go:build integration

// Package integration contains end-to-end tests that require Docker.
// Run with: go test -tags integration -v ./integration/...
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ashishkr375/smarthubos/cloud/cloud-api/auth"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/config"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/db"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/grafana"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/handlers"
	hubpkg "github.com/ashishkr375/smarthubos/cloud/cloud-api/hub"
	"github.com/go-chi/chi/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
)

// setupTestServer spins up a TimescaleDB container and returns a running test HTTP server.
func setupTestServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "timescale/timescaledb:latest-pg16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "testdb",
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
		},
		WaitingFor: wait.ForListeningPort("5432/tcp"),
	}

	pgContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	host, _ := pgContainer.Host(ctx)
	port, _ := pgContainer.MappedPort(ctx, "5432")
	dsn := fmt.Sprintf("postgres://test:test@%s:%s/testdb?sslmode=disable", host, port.Port())

	cfg := &config.Config{
		DatabaseURL:      dsn,
		JWTSecret:        "integration-test-secret-key-32bytes!!",
		Port:             "0",
		AutoMigrate:      true,
		AdminEmail:       "admin@test.local",
		AdminPassword:    "TestPass123!",
		GrafanaURL:       "",
		GrafanaAdminUser: "admin",
		GrafanaAdminPass: "admin",
	}
	auth.SetJWTSecret(cfg.JWTSecret)

	log, _ := zap.NewDevelopment()
	pool, err := db.Connect(ctx, cfg, log)
	if err != nil {
		pgContainer.Terminate(ctx) //nolint:errcheck
		t.Fatalf("db connect: %v", err)
	}

	// Bootstrap admin user for tests.
	if err := bootstrapTestAdmin(ctx, pool, cfg); err != nil {
		pool.Close()
		pgContainer.Terminate(ctx) //nolint:errcheck
		t.Fatalf("bootstrap admin: %v", err)
	}

	h := &handlers.Handlers{
		DB:      pool,
		HubMgr:  hubpkg.NewManager(log),
		Grafana: grafana.NewClient("", "admin", "admin"),
		Config:  cfg,
		Log:     log,
	}

	r := chi.NewRouter()
	r.Post("/api/v1/auth/login", h.Login)
	r.Post("/api/v1/auth/refresh", h.RefreshToken)
	r.Group(func(r chi.Router) {
		r.Use(auth.JWTMiddleware)
		r.Get("/api/v1/tenants", h.ListTenants)
		r.Post("/api/v1/tenants", h.CreateTenant)
		r.Get("/api/v1/hubs", h.ListHubs)
		r.Post("/api/v1/hubs/register", h.RegisterHub)
		r.Get("/api/v1/devices", h.ListDevices)
		r.Post("/api/v1/devices", h.CreateDevice)
	})
	r.Group(func(r chi.Router) {
		r.Use(auth.HubAPIKeyMiddleware(pool))
		r.Post("/internal/hub/ingest", h.Ingest)
		r.Post("/internal/hub/heartbeat", h.Heartbeat)
		r.Get("/internal/hub/cache-sync", h.CacheSync)
	})

	ts := httptest.NewServer(r)
	cleanup := func() {
		ts.Close()
		pool.Close()
		pgContainer.Terminate(ctx) //nolint:errcheck
	}
	return ts, cleanup
}

// doJSON is a test helper that sends a JSON request and decodes the response.
func doJSON(t *testing.T, method, url string, body any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body) //nolint:errcheck
	}
	req, _ := http.NewRequest(method, url, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result) //nolint:errcheck
	return resp.StatusCode, result
}

// TestProvisioningFlow covers US1: login → create tenant → register hub → provision device.
func TestProvisioningFlow(t *testing.T) {	ts, cleanup := setupTestServer(t)
	defer cleanup()
	base := ts.URL

	// 1. Login as admin.
	code, body := doJSON(t, "POST", base+"/api/v1/auth/login", map[string]string{
		"email":    "admin@test.local",
		"password": "TestPass123!",
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("login: expected 200 got %d — %v", code, body)
	}
	token := body["access_token"].(string)
	authHdr := map[string]string{"Authorization": "Bearer " + token}

	// 2. Create tenant.
	code, body = doJSON(t, "POST", base+"/api/v1/tenants", map[string]string{
		"name": "Lab 01", "namespace": "lab01",
	}, authHdr)
	if code != http.StatusCreated {
		t.Fatalf("create tenant: expected 201 got %d — %v", code, body)
	}
	tenantID := body["id"].(string)

	// 3. Register hub.
	code, body = doJSON(t, "POST", base+"/api/v1/hubs/register", map[string]string{
		"name": "Pi-01",
	}, authHdr)
	if code != http.StatusCreated {
		t.Fatalf("register hub: expected 201 got %d — %v", code, body)
	}
	hubID := body["id"].(string)
	hubAPIKey := body["api_key"].(string)
	if hubAPIKey == "" {
		t.Fatal("expected non-empty api_key")
	}

	// 4. Provision device under that hub and tenant.
	code, body = doJSON(t, "POST", base+"/api/v1/devices", map[string]string{
		"tenant_id":   tenantID,
		"hub_id":      hubID,
		"device_name": "sensor-01",
		"protocol":    "mqtt",
	}, authHdr)
	if code != http.StatusCreated {
		t.Fatalf("create device: expected 201 got %d — %v", code, body)
	}
	deviceToken := body["device_token"].(string)
	if deviceToken == "" {
		t.Fatal("expected non-empty device_token")
	}

	// 5. List devices — should return HTTP 200.
	req, _ := http.NewRequest("GET", base+"/api/v1/devices?tenant_id="+tenantID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("list devices: expected 200, got %v (err=%v)", resp.StatusCode, err)
	}
	resp.Body.Close()
}

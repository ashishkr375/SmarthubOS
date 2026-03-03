//go:build integration

package integration

import (
	"net/http"
	"testing"
)

// TestCacheSync covers US3:
// Hub authenticates → GET /internal/hub/cache-sync returns correct devices and rules.
func TestCacheSync(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()
	base := ts.URL

	// Setup: login, create tenant, register hub, create device and rule.
	code, body := doJSON(t, "POST", base+"/api/v1/auth/login", map[string]string{
		"email": "admin@test.local", "password": "TestPass123!",
	}, nil)
	if code != 200 {
		t.Fatalf("login %d: %v", code, body)
	}
	jwtToken := body["access_token"].(string)
	authHdr := map[string]string{"Authorization": "Bearer " + jwtToken}

	code, body = doJSON(t, "POST", base+"/api/v1/tenants",
		map[string]string{"name": "Sync Lab", "namespace": "synclab"}, authHdr)
	if code != 201 {
		t.Fatalf("create tenant %d: %v", code, body)
	}
	tenantID := body["id"].(string)

	code, body = doJSON(t, "POST", base+"/api/v1/hubs/register",
		map[string]string{"name": "Sync-Hub"}, authHdr)
	if code != 201 {
		t.Fatalf("register hub %d: %v", code, body)
	}
	hubAPIKey := body["api_key"].(string)
	hubID := body["id"].(string)

	// Create device.
	code, body = doJSON(t, "POST", base+"/api/v1/devices", map[string]string{
		"tenant_id": tenantID, "hub_id": hubID,
		"device_name": "door-sensor", "protocol": "mqtt",
	}, authHdr)
	if code != 201 {
		t.Fatalf("create device %d: %v", code, body)
	}
	devMap := body["device"].(map[string]any)
	deviceID := devMap["id"].(string)

	// GET /internal/hub/cache-sync.
	req, _ := http.NewRequest("GET", base+"/internal/hub/cache-sync", nil)
	req.Header.Set("Authorization", "Bearer "+hubAPIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		t.Fatalf("cache-sync: expected 200, got %d (err=%v)", statusCode, err)
	}
	defer resp.Body.Close()

	var syncResp struct {
		Devices []struct {
			DeviceID      string   `json:"device_id"`
			TokenHash     string   `json:"token_hash"`
			AllowedTopics []string `json:"allowed_topics"`
			Status        string   `json:"status"`
		} `json:"devices"`
		Rules []any `json:"rules"`
	}

	if err := decodeJSON(resp.Body, &syncResp); err != nil {
		t.Fatalf("decode cache-sync response: %v", err)
	}

	// Verify device appears in response.
	found := false
	for _, d := range syncResp.Devices {
		if d.DeviceID == deviceID {
			found = true
			if d.TokenHash == "" {
				t.Error("token_hash should be non-empty")
			}
			if len(d.AllowedTopics) == 0 {
				t.Error("allowed_topics should be non-empty")
			}
			// Verify topic format includes tenant namespace.
			topicsOK := false
			for _, topic := range d.AllowedTopics {
				if len(topic) > 0 && topic[:7] == "tenant." {
					topicsOK = true
					break
				}
			}
			if !topicsOK {
				t.Errorf("allowed_topics should start with 'tenant.', got %v", d.AllowedTopics)
			}
			break
		}
	}
	if !found {
		t.Errorf("device %s not found in cache-sync response", deviceID)
	}
}

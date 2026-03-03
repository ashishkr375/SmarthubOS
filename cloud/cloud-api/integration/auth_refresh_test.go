//go:build integration

package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestAuthRefresh covers US4: login → refresh token → old token rejected, new pair works.
func TestAuthRefresh(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()
	base := ts.URL

	// 1. Login.
	code, body := doJSON(t, "POST", base+"/api/v1/auth/login", map[string]string{
		"email": "admin@test.local", "password": "TestPass123!",
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("login: %d %v", code, body)
	}
	firstRefresh := body["refresh_token"].(string)

	// 2. Use the refresh token to obtain a new pair.
	code, body = doJSON(t, "POST", base+"/api/v1/auth/refresh", map[string]string{
		"refresh_token": firstRefresh,
	}, nil)
	if code != http.StatusOK {
		t.Fatalf("refresh: expected 200, got %d %v", code, body)
	}
	newAccessToken := body["access_token"].(string)
	newRefreshToken := body["refresh_token"].(string)

	if newAccessToken == "" {
		t.Fatal("expected non-empty access_token after refresh")
	}
	if newRefreshToken == firstRefresh {
		t.Fatal("refresh token should have rotated — old and new tokens must differ")
	}

	// 3. Attempt to reuse the old refresh token — must be rejected (revoked).
	code, body = doJSON(t, "POST", base+"/api/v1/auth/refresh", map[string]string{
		"refresh_token": firstRefresh,
	}, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("reuse of old refresh token: expected 401, got %d %v", code, body)
	}

	// 4. The new access token must be usable.
	req, _ := http.NewRequest("GET", base+"/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer "+newAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}
		t.Fatalf("use new access token: expected 200, got %d (err=%v)", statusCode, err)
	}
	resp.Body.Close()
}

// decodeJSON decodes JSON from an io.Reader into v.
func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

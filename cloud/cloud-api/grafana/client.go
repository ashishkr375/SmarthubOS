// Package grafana provides a minimal Grafana HTTP API client for tenant provisioning.
package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client interacts with the Grafana HTTP API for automated tenant provisioning.
type Client struct {
	baseURL   string
	adminUser string
	adminPass string
	http      *http.Client
}

// NewClient returns a Grafana API client.
// If baseURL is empty, all provisioning calls become no-ops.
func NewClient(baseURL, adminUser, adminPass string) *Client {
	return &Client{
		baseURL:   baseURL,
		adminUser: adminUser,
		adminPass: adminPass,
		http:      &http.Client{Timeout: 10 * time.Second},
	}
}

// ProvisionTenant creates a Grafana org and TimescaleDB datasource for a new tenant.
// Silently skips provisioning when baseURL is empty (local dev without Grafana).
func (c *Client) ProvisionTenant(ctx context.Context, tenantName, namespace string) error {
	if c.baseURL == "" {
		return nil
	}
	orgID, err := c.createOrg(ctx, tenantName)
	if err != nil {
		return fmt.Errorf("grafana create org: %w", err)
	}
	if err := c.createDatasource(ctx, orgID, namespace); err != nil {
		return fmt.Errorf("grafana create datasource: %w", err)
	}
	return nil
}

// createOrg posts to /api/orgs and returns the new org ID.
func (c *Client) createOrg(ctx context.Context, name string) (int64, error) {
	body, _ := json.Marshal(map[string]string{"name": name})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/orgs", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.adminUser, c.adminPass)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OrgID   int64  `json:"orgId"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return 0, fmt.Errorf("grafana returned %d: %s", resp.StatusCode, result.Message)
	}
	return result.OrgID, nil
}

// createDatasource provisions a TimescaleDB datasource for the given org.
func (c *Client) createDatasource(ctx context.Context, orgID int64, namespace string) error {
	body, _ := json.Marshal(map[string]any{
		"name":      "SmartHubOS-" + namespace,
		"type":      "postgres",
		"access":    "proxy",
		"isDefault": true,
		"jsonData": map[string]any{
			"database":        "smarthubos",
			"sslmode":         "disable",
			"timescaledb":     true,
			"postgresVersion": 1400,
		},
	})
	url := fmt.Sprintf("%s/api/orgs/%d/datasources", c.baseURL, orgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.adminUser, c.adminPass)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("datasource creation failed with status %d", resp.StatusCode)
	}
	return nil
}

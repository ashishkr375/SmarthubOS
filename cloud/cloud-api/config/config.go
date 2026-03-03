// Package config loads and validates all configuration from environment variables.
// Missing required variables cause an immediate fatal error — no silent defaults.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration for the cloud-api service.
type Config struct {
	DatabaseURL      string
	JWTSecret        string
	Port             string
	AutoMigrate      bool
	AdminEmail       string
	AdminPassword    string
	GrafanaURL       string
	GrafanaAdminUser string
	GrafanaAdminPass string
}

// Load reads all env vars and validates required fields.
// Returns an error if any required variable is missing or malformed.
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		JWTSecret:        os.Getenv("JWT_SECRET"),
		Port:             getEnvOrDefault("PORT", "9090"),
		AdminEmail:       os.Getenv("ADMIN_EMAIL"),
		AdminPassword:    os.Getenv("ADMIN_PASSWORD"),
		GrafanaURL:       os.Getenv("GRAFANA_URL"),
		GrafanaAdminUser: getEnvOrDefault("GRAFANA_ADMIN_USER", "admin"),
		GrafanaAdminPass: getEnvOrDefault("GRAFANA_ADMIN_PASS", "admin"),
	}

	autoMigrateStr := getEnvOrDefault("AUTO_MIGRATE", "false")
	autoMigrate, err := strconv.ParseBool(autoMigrateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid AUTO_MIGRATE value %q: %w", autoMigrateStr, err)
	}
	cfg.AutoMigrate = autoMigrate

	// Validate required vars
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 bytes (got %d)", len(cfg.JWTSecret))
	}
	if cfg.AdminEmail == "" {
		return nil, fmt.Errorf("ADMIN_EMAIL is required")
	}
	if cfg.AdminPassword == "" {
		return nil, fmt.Errorf("ADMIN_PASSWORD is required")
	}

	return cfg, nil
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

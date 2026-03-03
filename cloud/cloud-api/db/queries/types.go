// Package db/queries defines all shared entity types used across query files
// and returned to handlers.
package queries

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ─── TENANTS ────────────────────────────────────────────────────────────────

// Tenant represents a row in the tenants table.
type Tenant struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// ─── HUBS ───────────────────────────────────────────────────────────────────

// Hub represents a row in the hubs table.
// APIKeyHash is never serialized to JSON (security invariant).
type Hub struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	APIKeyHash string     `json:"-"` // never exposed via API
	LastPing   *time.Time `json:"last_ping"`
	IsOnline   bool       `json:"is_online"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ─── DEVICES ────────────────────────────────────────────────────────────────

// Device is the full device row including TokenHash.
// TokenHash MUST NOT be included in any public API response.
type Device struct {
	ID         uuid.UUID  `json:"-"`
	TenantID   uuid.UUID  `json:"-"`
	HubID      uuid.UUID  `json:"-"`
	DeviceName string     `json:"-"`
	Protocol   string     `json:"-"`
	TokenHash  string     `json:"-"` // never exposed via public API
	Status     string     `json:"-"`
	LastSeen   *time.Time `json:"-"`
	CreatedAt  time.Time  `json:"-"`
}

// DevicePublic is the safe response shape returned by the public API.
// It omits TokenHash.
type DevicePublic struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	HubID      uuid.UUID  `json:"hub_id"`
	DeviceName string     `json:"device_name"`
	Protocol   string     `json:"protocol"`
	Status     string     `json:"status"`
	LastSeen   *time.Time `json:"last_seen"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ToPublic converts a Device to DevicePublic, stripping TokenHash.
func (d *Device) ToPublic() DevicePublic {
	return DevicePublic{
		ID:         d.ID,
		TenantID:   d.TenantID,
		HubID:      d.HubID,
		DeviceName: d.DeviceName,
		Protocol:   d.Protocol,
		Status:     d.Status,
		LastSeen:   d.LastSeen,
		CreatedAt:  d.CreatedAt,
	}
}

// CacheSyncDevice is the device shape returned by GET /internal/hub/cache-sync.
// Includes TokenHash (the Pi needs it for offline bcrypt auth).
type CacheSyncDevice struct {
	DeviceID        uuid.UUID `json:"device_id"`
	TenantID        uuid.UUID `json:"tenant_id"`
	TokenHash       string    `json:"token_hash"`
	AllowedTopics   []string  `json:"allowed_topics"`
	Status          string    `json:"status"`
}

// ─── RULES ──────────────────────────────────────────────────────────────────

// Rule represents a row in the rules table.
type Rule struct {
	ID        uuid.UUID       `json:"id"`
	TenantID  uuid.UUID       `json:"tenant_id"`
	RuleJSON  json.RawMessage `json:"rule_json"`
	IsActive  bool            `json:"is_active"`
	CreatedAt time.Time       `json:"created_at"`
}

// CacheSyncRule is the rule shape returned by GET /internal/hub/cache-sync.
type CacheSyncRule struct {
	RuleID   uuid.UUID       `json:"rule_id"`
	TenantID uuid.UUID       `json:"tenant_id"`
	RuleJSON json.RawMessage `json:"rule_json"`
	IsActive bool            `json:"is_active"`
}

// ─── USERS ──────────────────────────────────────────────────────────────────

// User represents a row in the users table.
type User struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"` // never exposed
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

// ─── REFRESH TOKENS ─────────────────────────────────────────────────────────

// RefreshToken represents a row in the refresh_tokens table.
type RefreshToken struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	TokenHash string     `json:"-"` // never exposed
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	CreatedAt time.Time  `json:"created_at"`
}

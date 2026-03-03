package queries

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InsertDevice creates a device record.
// tokenHash is bcrypt(plaintext_token). The plaintext is returned once to the caller.
// Returns the full Device row (with TokenHash) so the handler can confirm storage.
func InsertDevice(ctx context.Context, pool *pgxpool.Pool, tenantID, hubID uuid.UUID, deviceName, protocol, tokenHash string) (*Device, error) {
	var d Device
	err := pool.QueryRow(ctx,
		`INSERT INTO devices (tenant_id, hub_id, device_name, protocol, token_hash)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, tenant_id, hub_id, device_name, protocol,
		           token_hash, status, last_seen, created_at`,
		tenantID, hubID, deviceName, protocol, tokenHash,
	).Scan(&d.ID, &d.TenantID, &d.HubID, &d.DeviceName, &d.Protocol,
		&d.TokenHash, &d.Status, &d.LastSeen, &d.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert device: %w", err)
	}
	return &d, nil
}

// ListDevicesByTenantID returns the public view of all devices for a tenant.
// TokenHash is never included.
func ListDevicesByTenantID(ctx context.Context, pool *pgxpool.Pool, tenantID uuid.UUID) ([]DevicePublic, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, tenant_id, hub_id, device_name, protocol, status, last_seen, created_at
		 FROM devices WHERE tenant_id = $1 ORDER BY created_at DESC`,
		tenantID)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	var devices []DevicePublic
	for rows.Next() {
		var d DevicePublic
		if err := rows.Scan(&d.ID, &d.TenantID, &d.HubID, &d.DeviceName,
			&d.Protocol, &d.Status, &d.LastSeen, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// GetDeviceByID returns the public view of a single device.
// Returns pgx.ErrNoRows if not found.
func GetDeviceByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*DevicePublic, error) {
	var d DevicePublic
	err := pool.QueryRow(ctx,
		`SELECT id, tenant_id, hub_id, device_name, protocol, status, last_seen, created_at
		 FROM devices WHERE id = $1`,
		id,
	).Scan(&d.ID, &d.TenantID, &d.HubID, &d.DeviceName,
		&d.Protocol, &d.Status, &d.LastSeen, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// RevokeDevice sets device status to 'revoked'. Scoped to tenantID for isolation.
func RevokeDevice(ctx context.Context, pool *pgxpool.Pool, id, tenantID uuid.UUID) error {
	tag, err := pool.Exec(ctx,
		`UPDATE devices SET status = 'revoked'
		 WHERE id = $1 AND tenant_id = $2 AND status != 'revoked'`,
		id, tenantID)
	if err != nil {
		return fmt.Errorf("revoke device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// UpdateDeviceLastSeen refreshes last_seen to NOW().
func UpdateDeviceLastSeen(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	_, err := pool.Exec(ctx,
		`UPDATE devices SET last_seen = NOW() WHERE id = $1`, id)
	return err
}

// GetActiveDevicesForCacheSync returns all active devices for the given hub, including
// TokenHash (for Pi offline auth) and computed AllowedTopics from the tenant namespace.
func GetActiveDevicesForCacheSync(ctx context.Context, pool *pgxpool.Pool, hubID uuid.UUID) ([]CacheSyncDevice, error) {
	rows, err := pool.Query(ctx,
		`SELECT d.id, d.tenant_id, t.namespace, d.token_hash, d.status
		 FROM devices d
		 JOIN tenants t ON t.id = d.tenant_id
		 WHERE d.hub_id = $1 AND d.status = 'active'`,
		hubID)
	if err != nil {
		return nil, fmt.Errorf("query cache sync devices: %w", err)
	}
	defer rows.Close()

	var devices []CacheSyncDevice
	for rows.Next() {
		var d CacheSyncDevice
		var namespace string
		if err := rows.Scan(&d.DeviceID, &d.TenantID, &namespace, &d.TokenHash, &d.Status); err != nil {
			return nil, fmt.Errorf("scan cache sync device: %w", err)
		}
		// Build MQTT topic patterns: tenant.<namespace>.<type>.<device_id>
		devStr := d.DeviceID.String()
		d.AllowedTopics = []string{
			"tenant." + namespace + ".telemetry." + devStr,
			"tenant." + namespace + ".command." + devStr,
			"tenant." + namespace + ".shadow." + devStr,
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

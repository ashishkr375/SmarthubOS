package queries

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InsertTenant creates a new tenant and returns the persisted row.
func InsertTenant(ctx context.Context, pool *pgxpool.Pool, name, namespace string) (*Tenant, error) {
	var t Tenant
	err := pool.QueryRow(ctx,
		`INSERT INTO tenants (name, namespace)
		 VALUES ($1, $2)
		 RETURNING id, name, namespace, is_active, created_at`,
		name, namespace,
	).Scan(&t.ID, &t.Name, &t.Namespace, &t.IsActive, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert tenant: %w", err)
	}
	return &t, nil
}

// GetTenantByID fetches a tenant by UUID. Returns pgx.ErrNoRows when not found.
func GetTenantByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*Tenant, error) {
	var t Tenant
	err := pool.QueryRow(ctx,
		`SELECT id, name, namespace, is_active, created_at FROM tenants WHERE id = $1`,
		id,
	).Scan(&t.ID, &t.Name, &t.Namespace, &t.IsActive, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTenants returns all tenants ordered by creation time descending.
func ListTenants(ctx context.Context, pool *pgxpool.Pool) ([]Tenant, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, name, namespace, is_active, created_at FROM tenants ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Namespace, &t.IsActive, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		tenants = append(tenants, t)
	}
	return tenants, rows.Err()
}

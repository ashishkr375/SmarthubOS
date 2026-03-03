package queries

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InsertHub persists a new hub. id is pre-generated so the API key can embed it.
func InsertHub(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, name, apiKeyHash string) (*Hub, error) {
	var h Hub
	err := pool.QueryRow(ctx,
		`INSERT INTO hubs (id, name, api_key_hash)
		 VALUES ($1, $2, $3)
		 RETURNING id, name, api_key_hash, last_ping, is_online, created_at`,
		id, name, apiKeyHash,
	).Scan(&h.ID, &h.Name, &h.APIKeyHash, &h.LastPing, &h.IsOnline, &h.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert hub: %w", err)
	}
	return &h, nil
}

// GetHubByID fetches a hub row including APIKeyHash for auth comparisons.
// Returns pgx.ErrNoRows when not found.
func GetHubByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*Hub, error) {
	var h Hub
	err := pool.QueryRow(ctx,
		`SELECT id, name, api_key_hash, last_ping, is_online, created_at
		 FROM hubs WHERE id = $1`,
		id,
	).Scan(&h.ID, &h.Name, &h.APIKeyHash, &h.LastPing, &h.IsOnline, &h.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// ListHubs returns all hubs ordered by creation time descending.
func ListHubs(ctx context.Context, pool *pgxpool.Pool) ([]Hub, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, name, api_key_hash, last_ping, is_online, created_at
		 FROM hubs ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list hubs: %w", err)
	}
	defer rows.Close()

	var hubs []Hub
	for rows.Next() {
		var h Hub
		if err := rows.Scan(&h.ID, &h.Name, &h.APIKeyHash, &h.LastPing, &h.IsOnline, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan hub: %w", err)
		}
		hubs = append(hubs, h)
	}
	return hubs, rows.Err()
}

// UpdateHubPing sets last_ping = NOW() and is_online = TRUE for the given hub.
func UpdateHubPing(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	_, err := pool.Exec(ctx,
		`UPDATE hubs SET last_ping = NOW(), is_online = TRUE WHERE id = $1`, id)
	return err
}

// MarkOfflineHubs bulk-sets is_online = FALSE for hubs silent for ≥ 2 minutes.
// Called by the background checker goroutine every 30 seconds.
func MarkOfflineHubs(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx,
		`UPDATE hubs SET is_online = FALSE
		 WHERE is_online = TRUE
		   AND (last_ping IS NULL OR last_ping < NOW() - INTERVAL '2 minutes')`)
	return err
}

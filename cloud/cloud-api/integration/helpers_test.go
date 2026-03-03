//go:build integration

// Package integration — shared test helpers (not a test itself).
package integration

import (
	"context"
	"errors"

	"github.com/ashishkr375/smarthubos/cloud/cloud-api/auth"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/config"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/db/queries"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// bootstrapTestAdmin seeds the admin user from cfg if the account doesn't exist yet.
func bootstrapTestAdmin(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) error {
	_, err := queries.GetUserByEmail(ctx, pool, cfg.AdminEmail)
	if err == nil {
		return nil // already exists
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	hash, err := auth.Hash(cfg.AdminPassword)
	if err != nil {
		return err
	}
	return queries.InsertUser(ctx, pool, cfg.AdminEmail, hash, "admin")
}

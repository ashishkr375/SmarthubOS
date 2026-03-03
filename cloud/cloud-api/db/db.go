// Package db provides database connectivity and migration utilities.
package db

import (
	"context"
	"fmt"

	"github.com/ashishkr375/smarthubos/cloud/cloud-api/config"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/migrations"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// Connect opens a pgxpool connection, pings the DB, and optionally runs migrations.
func Connect(ctx context.Context, cfg *config.Config, log *zap.Logger) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pgxpool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	log.Info("database connected")

	if cfg.AutoMigrate {
		if err := RunMigrations(cfg.DatabaseURL, log); err != nil {
			pool.Close()
			return nil, err
		}
	}
	return pool, nil
}

// RunMigrations applies all pending SQL migrations from the embedded FS.
// Exported so that CI tooling can call it directly.
func RunMigrations(databaseURL string, log *zap.Logger) error {
	d, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("init iofs source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", d, databaseURL)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}
	log.Info("database migrations applied")
	return nil
}

package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ashishkr375/smarthubos/cloud/cloud-api/auth"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/config"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/db"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/db/queries"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/grafana"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/handlers"
	hubpkg "github.com/ashishkr375/smarthubos/cloud/cloud-api/hub"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

func main() {
	log := zap.Must(zap.NewProduction())
	defer log.Sync() //nolint:errcheck

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("load config", zap.Error(err))
	}

	// Initialise JWT signing key before any handler can issue/validate tokens.
	auth.SetJWTSecret(cfg.JWTSecret)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg, log)
	if err != nil {
		log.Fatal("connect database", zap.Error(err))
	}
	defer pool.Close()

	if err := bootstrapAdmin(ctx, pool, cfg, log); err != nil {
		log.Fatal("bootstrap admin user", zap.Error(err))
	}

	grafanaClient := grafana.NewClient(cfg.GrafanaURL, cfg.GrafanaAdminUser, cfg.GrafanaAdminPass)
	hubManager := hubpkg.NewManager(log)

	h := &handlers.Handlers{
		DB:      pool,
		HubMgr:  hubManager,
		Grafana: grafanaClient,
		Config:  cfg,
		Log:     log,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(handlers.RequestLogger(log))

	// Liveness probe — no auth required.
	r.Get("/health", h.Health)

	// Public auth endpoints.
	r.Post("/api/v1/auth/login", h.Login)
	r.Post("/api/v1/auth/refresh", h.RefreshToken)

	// JWT-protected management API.
	r.Group(func(r chi.Router) {
		r.Use(auth.JWTMiddleware)

		r.Get("/api/v1/tenants", h.ListTenants)
		r.Post("/api/v1/tenants", h.CreateTenant)
		r.Get("/api/v1/tenants/{id}", h.GetTenant)

		r.Get("/api/v1/hubs", h.ListHubs)
		r.Post("/api/v1/hubs/register", h.RegisterHub)
		r.Get("/api/v1/hubs/{id}", h.GetHubByID)

		r.Get("/api/v1/devices", h.ListDevices)
		r.Post("/api/v1/devices", h.CreateDevice)
		r.Delete("/api/v1/devices/{id}", h.RevokeDevice)
		r.Post("/api/v1/devices/{id}/command", h.SendCommand)

		r.Get("/api/v1/telemetry", h.QueryTelemetry)

		r.Get("/api/v1/rules", h.ListRules)
		r.Post("/api/v1/rules", h.CreateRule)
		r.Put("/api/v1/rules/{id}", h.UpdateRule)
		r.Delete("/api/v1/rules/{id}", h.DeleteRule)
	})

	// Hub-API-key-protected internal endpoints.
	r.Group(func(r chi.Router) {
		r.Use(auth.HubAPIKeyMiddleware(pool))

		r.With(handlers.IngestRateLimiter).Post("/internal/hub/ingest", h.Ingest)
		r.Post("/internal/hub/heartbeat", h.Heartbeat)
		r.Get("/internal/hub/cache-sync", h.CacheSync)
		r.Get("/hub/ws", h.HubWS)
	})

	// Background goroutine: mark hubs offline when no heartbeat for 2 min.
	go runHubOnlineChecker(ctx, pool, log)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Info("cloud-api listening", zap.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("server error", zap.Error(err))
		}
	}()

	<-ctx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown error", zap.Error(err))
	}
	log.Info("shutdown complete")
}

// bootstrapAdmin creates the admin user from env vars if it doesn't already exist.
func bootstrapAdmin(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, log *zap.Logger) error {
	_, err := queries.GetUserByEmail(ctx, pool, cfg.AdminEmail)
	if err == nil {
		return nil // user already exists
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	hash, err := auth.Hash(cfg.AdminPassword)
	if err != nil {
		return err
	}
	if err := queries.InsertUser(ctx, pool, cfg.AdminEmail, hash, "admin"); err != nil {
		return err
	}
	log.Info("admin user bootstrapped", zap.String("email", cfg.AdminEmail))
	return nil
}

// runHubOnlineChecker ticks every 30 seconds and marks hubs offline
// when their last_ping is older than 2 minutes.
func runHubOnlineChecker(ctx context.Context, pool *pgxpool.Pool, log *zap.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := queries.MarkOfflineHubs(ctx, pool); err != nil {
				log.Error("mark offline hubs", zap.Error(err))
			}
		case <-ctx.Done():
			return
		}
	}
}

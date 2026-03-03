// Package handlers contains all HTTP handler methods for the cloud-api service.
package handlers

import (
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/config"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/grafana"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/hub"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// Handlers holds shared dependencies injected at startup.
// All HTTP handler methods are defined on this struct.
type Handlers struct {
	DB      *pgxpool.Pool
	HubMgr  *hub.Manager
	Grafana *grafana.Client
	Config  *config.Config
	Log     *zap.Logger
}

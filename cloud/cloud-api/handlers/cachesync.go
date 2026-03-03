package handlers

import (
	"net/http"

	"github.com/ashishkr375/smarthubos/cloud/cloud-api/auth"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/db/queries"
	"go.uber.org/zap"
)

// cacheSyncResponse is the payload returned by GET /internal/hub/cache-sync.
type cacheSyncResponse struct {
	Devices []queries.CacheSyncDevice `json:"devices"`
	Rules   []queries.CacheSyncRule   `json:"rules"`
}

// CacheSync handles GET /internal/hub/cache-sync.
// Returns all active devices (with token hashes) and active rules for this hub.
// Called by the Pi every 60 seconds to refresh its local auth cache.
func (h *Handlers) CacheSync(w http.ResponseWriter, r *http.Request) {
	hub := auth.GetHub(r.Context())
	if hub == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	devices, err := queries.GetActiveDevicesForCacheSync(r.Context(), h.DB, hub.ID)
	if err != nil {
		h.Log.Error("cache sync devices", zap.String("hub_id", hub.ID.String()), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	rules, err := queries.GetActiveRulesForCacheSync(r.Context(), h.DB, hub.ID)
	if err != nil {
		h.Log.Error("cache sync rules", zap.String("hub_id", hub.ID.String()), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	if devices == nil {
		devices = []queries.CacheSyncDevice{}
	}
	if rules == nil {
		rules = []queries.CacheSyncRule{}
	}

	writeJSON(w, http.StatusOK, cacheSyncResponse{
		Devices: devices,
		Rules:   rules,
	})
}

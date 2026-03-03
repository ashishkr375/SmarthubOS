package handlers

import (
	"net/http"

	"github.com/ashishkr375/smarthubos/cloud/cloud-api/auth"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/db/queries"
	"go.uber.org/zap"
)

// Heartbeat handles POST /internal/hub/heartbeat.
// Updates last_ping = NOW() and is_online = TRUE for the authenticated hub.
func (h *Handlers) Heartbeat(w http.ResponseWriter, r *http.Request) {
	hub := auth.GetHub(r.Context())
	if hub == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := queries.UpdateHubPing(r.Context(), h.DB, hub.ID); err != nil {
		h.Log.Error("heartbeat update fail", zap.String("hub_id", hub.ID.String()), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

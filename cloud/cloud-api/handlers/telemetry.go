package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ashishkr375/smarthubos/cloud/cloud-api/db/queries"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// QueryTelemetry handles GET /api/v1/telemetry.
//
// Query params:
//
//	device_id  — UUID (required)
//	metric     — string (required)
//	from       — RFC3339 timestamp (default: 24 hours ago)
//	to         — RFC3339 timestamp (default: now)
//	limit      — int, max 10 000 (default: 1 000)
func (h *Handlers) QueryTelemetry(w http.ResponseWriter, r *http.Request) {
	deviceIDStr := r.URL.Query().Get("device_id")
	metric := r.URL.Query().Get("metric")
	if deviceIDStr == "" || metric == "" {
		writeError(w, http.StatusBadRequest, "device_id_and_metric_required")
		return
	}

	deviceID, err := uuid.Parse(deviceIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_device_id")
		return
	}

	now := time.Now()
	from := now.Add(-24 * time.Hour)
	to := now

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = t
		}
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = t
		}
	}

	limit := 1000
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	rows, err := queries.QueryTelemetry(r.Context(), h.DB, deviceID, metric, from, to, limit)
	if err != nil {
		h.Log.Error("query telemetry", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if rows == nil {
		rows = []queries.TelemetryRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device_id": deviceID,
		"metric":    metric,
		"from":      from.Format(time.RFC3339),
		"to":        to.Format(time.RFC3339),
		"count":     len(rows),
		"rows":      rows,
	})
}

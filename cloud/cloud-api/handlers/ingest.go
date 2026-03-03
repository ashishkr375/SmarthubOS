package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ashishkr375/smarthubos/cloud/cloud-api/auth"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/db/queries"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ingestPayload is the JSON body expected from a hub at POST /internal/hub/ingest.
type ingestPayload struct {
	DeviceID string          `json:"device_id"`
	TenantID string          `json:"tenant_id"`
	Metric   string          `json:"metric"`
	Value    float64         `json:"value"`
	Time     *time.Time      `json:"time"`        // optional; defaults to now
	Tags     json.RawMessage `json:"tags"`        // optional JSONB object
}

// Ingest handles POST /internal/hub/ingest.
// Accepts a JSON array of telemetry readings and persists them in a batch.
// Authenticated via hub API key.
func (h *Handlers) Ingest(w http.ResponseWriter, r *http.Request) {
	hub := auth.GetHub(r.Context())
	if hub == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var payloads []ingestPayload
	if err := json.NewDecoder(r.Body).Decode(&payloads); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if len(payloads) == 0 {
		writeError(w, http.StatusBadRequest, "empty_payload")
		return
	}
	if len(payloads) > 1000 {
		writeError(w, http.StatusRequestEntityTooLarge, "too_many_rows")
		return
	}

	now := time.Now()
	rows := make([]queries.TelemetryRow, 0, len(payloads))
	for _, p := range payloads {
		deviceID, err := uuid.Parse(p.DeviceID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_device_id")
			return
		}
		tenantID, err := uuid.Parse(p.TenantID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_tenant_id")
			return
		}
		ts := now
		if p.Time != nil {
			ts = *p.Time
		}
		rows = append(rows, queries.TelemetryRow{
			Time:     ts,
			DeviceID: deviceID,
			TenantID: tenantID,
			Metric:   p.Metric,
			Value:    p.Value,
			Tags:     p.Tags,
		})
		// Best-effort: update last_seen in background (avoids request-ctx cancellation).
		go func(id uuid.UUID) {
			queries.UpdateDeviceLastSeen(context.Background(), h.DB, id) //nolint:errcheck
		}(deviceID)
	}

	if err := queries.InsertTelemetryBatch(r.Context(), h.DB, rows); err != nil {
		h.Log.Error("telemetry batch insert", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"accepted": len(rows)})
}

package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ashishkr375/smarthubos/cloud/cloud-api/auth"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/db/queries"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

// ListDevices handles GET /api/v1/devices?tenant_id=<uuid>.
func (h *Handlers) ListDevices(w http.ResponseWriter, r *http.Request) {
	tenantIDStr := r.URL.Query().Get("tenant_id")
	if tenantIDStr == "" {
		writeError(w, http.StatusBadRequest, "tenant_id_required")
		return
	}
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_tenant_id")
		return
	}

	devices, err := queries.ListDevicesByTenantID(r.Context(), h.DB, tenantID)
	if err != nil {
		h.Log.Error("list devices", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if devices == nil {
		devices = []queries.DevicePublic{}
	}
	writeJSON(w, http.StatusOK, devices)
}

// CreateDevice handles POST /api/v1/devices.
// Generates a device token, hashes it, and returns the plaintext token ONCE.
func (h *Handlers) CreateDevice(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		TenantID   string `json:"tenant_id"`
		HubID      string `json:"hub_id"`
		DeviceName string `json:"device_name"`
		Protocol   string `json:"protocol"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_tenant_id")
		return
	}
	hubID, err := uuid.Parse(req.HubID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_hub_id")
		return
	}
	if req.DeviceName == "" || req.Protocol == "" {
		writeError(w, http.StatusBadRequest, "device_name_and_protocol_required")
		return
	}

	// Generate a 32-byte random device token.
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	rawTokenHex := hex.EncodeToString(rawToken)

	tokenHash, err := auth.Hash(rawTokenHex)
	if err != nil {
		h.Log.Error("hash device token", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	device, err := queries.InsertDevice(r.Context(), h.DB, tenantID, hubID,
		req.DeviceName, req.Protocol, tokenHash)
	if err != nil {
		h.Log.Error("insert device", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"device":       device.ToPublic(),
		"device_token": rawTokenHex, // shown ONCE — store on device firmware
	})
}

// RevokeDevice handles DELETE /api/v1/devices/{id}.
// Requires admin role; scoped to the tenant in the query parameter.
func (h *Handlers) RevokeDevice(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if !requireRole(claims, "admin") {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	deviceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	tenantIDStr := r.URL.Query().Get("tenant_id")
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_tenant_id")
		return
	}

	if err := queries.RevokeDevice(r.Context(), h.DB, deviceID, tenantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		h.Log.Error("revoke device", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SendCommand handles POST /api/v1/devices/{id}/command.
// Delivers a command payload to the hub managing this device (via WebSocket or buffer).
func (h *Handlers) SendCommand(w http.ResponseWriter, r *http.Request) {
	deviceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}

	device, err := queries.GetDeviceByID(r.Context(), h.DB, deviceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	payload["device_id"] = deviceID.String()

	cmdBytes, err := encodeJSON(payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	h.HubMgr.SendCommand(device.HubID.String(), cmdBytes)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ashishkr375/smarthubos/cloud/cloud-api/auth"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/db/queries"
	hubpkg "github.com/ashishkr375/smarthubos/cloud/cloud-api/hub"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// CheckOrigin allows all origins; hub connections originate from trusted Pis.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ListHubs handles GET /api/v1/hubs.
func (h *Handlers) ListHubs(w http.ResponseWriter, r *http.Request) {
	hubs, err := queries.ListHubs(r.Context(), h.DB)
	if err != nil {
		h.Log.Error("list hubs", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if hubs == nil {
		hubs = []queries.Hub{}
	}
	writeJSON(w, http.StatusOK, hubs)
}

// RegisterHub handles POST /api/v1/hubs/register.
// Generates a hub API key of the form "{hub_uuid}:{random_hex_64}".
// The plaintext key is returned once — it is NOT recoverable after this response.
//
// Security: the key is stored as bcrypt(sha256(key)) in the database.
func (h *Handlers) RegisterHub(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if !requireRole(claims, "admin") {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	hubID := uuid.New()

	// Build API key: "{hub_uuid}:{random_32_bytes_hex}"
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		h.Log.Error("rand read for hub key", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	rawKey := hubID.String() + ":" + hex.EncodeToString(random)

	// Hash using bcrypt(sha256(rawKey)) — bcrypt alone would truncate at 72 bytes.
	keyHash, err := auth.Hash(rawKey)
	if err != nil {
		h.Log.Error("hash hub api key", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	hub, err := queries.InsertHub(r.Context(), h.DB, hubID, req.Name, keyHash)
	if err != nil {
		h.Log.Error("insert hub", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         hub.ID,
		"name":       hub.Name,
		"is_online":  hub.IsOnline,
		"created_at": hub.CreatedAt,
		"api_key":    rawKey, // shown ONCE — store securely on the Pi
	})
}

// HubWS handles GET /hub/ws — upgrades to a WebSocket for cloud→hub push commands.
// Requires hub API key auth via HubAPIKeyMiddleware.
func (h *Handlers) HubWS(w http.ResponseWriter, r *http.Request) {
	hubInfo := auth.GetHub(r.Context())
	if hubInfo == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.Log.Warn("ws upgrade failed", zap.Error(err))
		return
	}

	c, pending := h.HubMgr.Register(hubInfo.ID.String(), conn)

	// Drain buffered commands immediately after reconnect.
	go func() {
		for _, cmd := range pending {
			select {
			case c.Send <- cmd.Payload:
			default:
			}
		}
	}()

	go hubpkg.WritePump(c, h.Log)
	hubpkg.ReadPump(c, h.HubMgr, h.Log) // blocks until disconnect
}

// GetHubByID handles GET /api/v1/hubs/{id}.
func (h *Handlers) GetHubByID(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	hub, err := queries.GetHubByID(r.Context(), h.DB, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, hub)
}

	hubs, err := queries.ListHubs(r.Context(), h.DB)
	if err != nil {
		h.Log.Error("list hubs", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if hubs == nil {
		hubs = []queries.Hub{}
	}
	writeJSON(w, http.StatusOK, hubs)
}

// RegisterHub handles POST /api/v1/hubs/register.
// Generates a hub API key of the form "{hub_uuid}:{random_hex_64}".
// The plaintext key is returned once — it is NOT recoverable after this response.
//
// Security: the key is stored as bcrypt(sha256(key)) in the database.
func (h *Handlers) RegisterHub(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if !requireRole(claims, "admin") {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	hubID := uuid.New()

	// Build API key: "{hub_uuid}:{random_32_bytes_hex}"
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		h.Log.Error("rand read for hub key", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	rawKey := hubID.String() + ":" + hex.EncodeToString(random)

	// Hash using bcrypt(sha256(rawKey)) — bcrypt alone would truncate at 72 bytes.
	keyHash, err := auth.Hash(rawKey)
	if err != nil {
		h.Log.Error("hash hub api key", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	hub, err := queries.InsertHub(r.Context(), h.DB, hubID, req.Name, keyHash)
	if err != nil {
		h.Log.Error("insert hub", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         hub.ID,
		"name":       hub.Name,
		"is_online":  hub.IsOnline,
		"created_at": hub.CreatedAt,
		"api_key":    rawKey, // shown ONCE — store securely on the Pi
	})
}

// HubWS handles GET /hub/ws — upgrades to a WebSocket for cloud→hub commands.
// Requires hub API key auth (via HubAPIKeyMiddleware).
func (h *Handlers) HubWS(w http.ResponseWriter, r *http.Request) {
	hubInfo := auth.GetHub(r.Context())
	if hubInfo == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	upgrader := newUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.Log.Warn("ws upgrade failed", zap.Error(err))
		return
	}

	c, pending := h.HubMgr.Register(hubInfo.ID.String(), conn)

	// Drain any buffered commands immediately after connect.
	go func() {
		for _, cmd := range pending {
			select {
			case c.Send <- cmd.Payload:
			default:
			}
		}
	}()

	go writePump(c, h.Log)
	readPump(c, h.HubMgr, h.Log)
}

// GetHubByID handles GET /api/v1/hubs/{id}.
func (h *Handlers) GetHubByID(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		// chi param fallback
		idStr = r.URL.Query().Get("id")
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	hub, err := queries.GetHubByID(r.Context(), h.DB, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, hub)
}

package handlers

import (
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

// ListTenants handles GET /api/v1/tenants.
// Requires admin role.
func (h *Handlers) ListTenants(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if !requireRole(claims, "admin") {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	tenants, err := queries.ListTenants(r.Context(), h.DB)
	if err != nil {
		h.Log.Error("list tenants", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if tenants == nil {
		tenants = []queries.Tenant{}
	}
	writeJSON(w, http.StatusOK, tenants)
}

// CreateTenant handles POST /api/v1/tenants.
// Requires admin role. Triggers Grafana provisioning after tenant creation.
func (h *Handlers) CreateTenant(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if !requireRole(claims, "admin") {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Namespace == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	tenant, err := queries.InsertTenant(r.Context(), h.DB, req.Name, req.Namespace)
	if err != nil {
		h.Log.Error("insert tenant", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Best-effort Grafana provisioning — failure is logged but does not 500.
	if err := h.Grafana.ProvisionTenant(r.Context(), req.Name, req.Namespace); err != nil {
		h.Log.Warn("grafana provisioning failed", zap.Error(err), zap.String("tenant", req.Name))
	}

	writeJSON(w, http.StatusCreated, tenant)
}

// GetTenant handles GET /api/v1/tenants/{id}.
func (h *Handlers) GetTenant(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	tenant, err := queries.GetTenantByID(r.Context(), h.DB, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, tenant)
}

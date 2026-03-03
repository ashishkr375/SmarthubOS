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

// ListRules handles GET /api/v1/rules?tenant_id=<uuid>.
func (h *Handlers) ListRules(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(r.URL.Query().Get("tenant_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_tenant_id")
		return
	}
	rules, err := queries.ListRulesByTenantID(r.Context(), h.DB, tenantID)
	if err != nil {
		h.Log.Error("list rules", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if rules == nil {
		rules = []queries.Rule{}
	}
	writeJSON(w, http.StatusOK, rules)
}

// CreateRule handles POST /api/v1/rules.
func (h *Handlers) CreateRule(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if !requireRole(claims, "admin") {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		TenantID string          `json:"tenant_id"`
		RuleJSON json.RawMessage `json:"rule_json"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TenantID == "" || len(req.RuleJSON) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_tenant_id")
		return
	}

	rule, err := queries.InsertRule(r.Context(), h.DB, tenantID, req.RuleJSON)
	if err != nil {
		h.Log.Error("insert rule", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

// UpdateRule handles PUT /api/v1/rules/{id}.
func (h *Handlers) UpdateRule(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if !requireRole(claims, "admin") {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	ruleID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}

	var req struct {
		TenantID string          `json:"tenant_id"`
		RuleJSON json.RawMessage `json:"rule_json"`
		IsActive bool            `json:"is_active"`
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

	rule, err := queries.UpdateRule(r.Context(), h.DB, ruleID, tenantID, req.RuleJSON, req.IsActive)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

// DeleteRule handles DELETE /api/v1/rules/{id}.
func (h *Handlers) DeleteRule(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if !requireRole(claims, "admin") {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	ruleID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id")
		return
	}
	tenantID, err := uuid.Parse(r.URL.Query().Get("tenant_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_tenant_id")
		return
	}

	if err := queries.DeleteRule(r.Context(), h.DB, ruleID, tenantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		h.Log.Error("delete rule", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

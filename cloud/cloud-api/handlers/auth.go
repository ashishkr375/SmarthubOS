package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ashishkr375/smarthubos/cloud/cloud-api/auth"
	"github.com/ashishkr375/smarthubos/cloud/cloud-api/db/queries"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

const refreshTokenTTL = 7 * 24 * time.Hour

// Login handles POST /api/v1/auth/login.
// Returns a 15-minute access token and a 7-day refresh token on success.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	user, err := queries.GetUserByEmail(r.Context(), h.DB, req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials")
			return
		}
		h.Log.Error("login db", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	if !auth.Compare(user.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	accessToken, err := auth.SignAccessToken(user.ID.String(), "", user.Role)
	if err != nil {
		h.Log.Error("sign access token", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	rawRefresh, refreshHash, err := generateRefreshToken(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	expiresAt := time.Now().Add(refreshTokenTTL)
	if err := queries.InsertRefreshToken(r.Context(), h.DB, user.ID, refreshHash, expiresAt); err != nil {
		h.Log.Error("insert refresh token", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": rawRefresh,
		"expires_in":    int(15 * 60),
		"token_type":    "Bearer",
	})
}

// RefreshToken handles POST /api/v1/auth/refresh.
// Token rotation: old refresh token is revoked, new access+refresh pair is returned.
//
// The refresh token is prefixed with the user's UUID so we can do an O(1) DB lookup:
//
//	format: "{user_uuid}:{random_hex_64}"
func (h *Handlers) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	// Validate format: UUID (36) + ":" + hex payload.
	if len(req.RefreshToken) < 38 || req.RefreshToken[36] != ':' {
		writeError(w, http.StatusUnauthorized, "invalid_token")
		return
	}
	userID, err := uuid.Parse(req.RefreshToken[:36])
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token")
		return
	}

	user, err := queries.GetUserByID(r.Context(), h.DB, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid_token")
			return
		}
		h.Log.Error("get user for refresh", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Fetch all active (non-revoked, non-expired) tokens for this user.
	tokens, err := queries.GetNonRevokedRefreshTokensByUserID(r.Context(), h.DB, userID)
	if err != nil {
		h.Log.Error("get refresh tokens", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Find the matching token via bcrypt comparison.
	var matched *queries.RefreshToken
	for i := range tokens {
		if auth.Compare(tokens[i].TokenHash, req.RefreshToken) {
			matched = &tokens[i]
			break
		}
	}
	if matched == nil {
		writeError(w, http.StatusUnauthorized, "invalid_token")
		return
	}

	// Rotate: revoke old token first.
	if err := queries.RevokeRefreshToken(r.Context(), h.DB, matched.ID); err != nil {
		h.Log.Error("revoke old refresh token", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Issue new access token.
	accessToken, err := auth.SignAccessToken(user.ID.String(), "", user.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Issue new refresh token.
	rawRefresh, refreshHash, err := generateRefreshToken(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	expiresAt := time.Now().Add(refreshTokenTTL)
	if err := queries.InsertRefreshToken(r.Context(), h.DB, user.ID, refreshHash, expiresAt); err != nil {
		h.Log.Error("insert new refresh token", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": rawRefresh,
		"expires_in":    int(15 * 60),
		"token_type":    "Bearer",
	})
}

// generateRefreshToken creates a "{user_uuid}:{random_hex_64}" token.
// Returns (rawToken, bcryptHash, error).
func generateRefreshToken(userID uuid.UUID) (string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw := userID.String() + ":" + hex.EncodeToString(buf)
	hash, err := auth.Hash(raw)
	if err != nil {
		return "", "", err
	}
	return raw, hash, nil
}

// requireRole checks that the authenticated user has the expected role.
func requireRole(claims *auth.Claims, role string) bool {
	return claims != nil && strings.EqualFold(claims.Role, role)
}

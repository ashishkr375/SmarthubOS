package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/ashishkr375/smarthubos/cloud/cloud-api/db/queries"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// contextKey is an unexported type for context keys in this package.
type contextKey string

const (
	claimsContextKey contextKey = "claims"
	hubContextKey    contextKey = "hub"
)

// JWTMiddleware validates the Bearer token, injects *Claims into request context.
// Returns 401 with "token_expired" if the token is expired, otherwise "unauthorized".
func JWTMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeUnauthorized(w)
			return
		}
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := ParseAccessToken(tokenStr)
		if err != nil {
			if IsExpiredError(err) {
				writeErrorJSON(w, http.StatusUnauthorized, "token_expired")
				return
			}
			writeUnauthorized(w)
			return
		}
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// HubAPIKeyMiddleware authenticates hub requests using hub API keys.
//
// Expected header: Authorization: Bearer {hub_uuid}:{random_hex_32}
//
// The UUID prefix enables an O(1) DB lookup: parse hub_id → GetHubByID → Compare.
// This avoids scanning all hubs on every request.
func HubAPIKeyMiddleware(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				writeUnauthorized(w)
				return
			}
			rawKey := strings.TrimPrefix(authHeader, "Bearer ")

			// The key format is "{hub_uuid}:{hex}" — UUID is always 36 chars.
			if len(rawKey) < 38 || rawKey[36] != ':' {
				writeUnauthorized(w)
				return
			}
			hubID, err := uuid.Parse(rawKey[:36])
			if err != nil {
				writeUnauthorized(w)
				return
			}

			hub, err := queries.GetHubByID(r.Context(), pool, hubID)
			if err != nil {
				// Do not distinguish not-found from DB error to avoid oracle attacks.
				writeUnauthorized(w)
				return
			}

			if !Compare(hub.APIKeyHash, rawKey) {
				writeUnauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), hubContextKey, hub)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetClaims extracts JWT claims from the request context.
// Returns nil if the middleware was not applied or authentication failed.
func GetClaims(ctx context.Context) *Claims {
	v, _ := ctx.Value(claimsContextKey).(*Claims)
	return v
}

// GetHub extracts the authenticated Hub from the request context.
// Returns nil if HubAPIKeyMiddleware was not applied or authentication failed.
func GetHub(ctx context.Context) *queries.Hub {
	v, _ := ctx.Value(hubContextKey).(*queries.Hub)
	return v
}

func writeUnauthorized(w http.ResponseWriter) {
	writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
}

func writeErrorJSON(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":"` + msg + `"}`)) //nolint:errcheck
}

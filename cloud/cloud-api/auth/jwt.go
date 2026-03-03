// Package auth handles JWT signing, parsing, and claim extraction.
// Algorithm is hardcoded to HS256; RS256 or any other algorithm is rejected.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the payload embedded in each access token.
type Claims struct {
	Sub      string `json:"sub"`
	TenantID string `json:"tenant_id"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// jwtSecret holds the HS256 signing key. Set once at startup via SetJWTSecret.
var jwtSecret []byte

// SetJWTSecret stores the signing key. Must be called before any token operations.
func SetJWTSecret(secret string) {
	jwtSecret = []byte(secret)
}

// SignAccessToken creates a signed HS256 JWT with a 15-minute expiry.
func SignAccessToken(sub, tenantID, role string) (string, error) {
	claims := Claims{
		Sub:      sub,
		TenantID: tenantID,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sub,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(jwtSecret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return signed, nil
}

// ParseAccessToken validates a token string and returns the claims.
// Only HS256 tokens are accepted. Returns an error for any invalid input.
func ParseAccessToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(
		tokenStr,
		&Claims{},
		func(t *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		},
		jwt.WithValidMethods([]string{"HS256"}),
	)
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}

// IsExpiredError returns true if err is a token-expired error.
func IsExpiredError(err error) bool {
	return errors.Is(err, jwt.ErrTokenExpired)
}

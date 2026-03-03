package queries

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GetUserByID fetches a user by their UUID. Returns pgx.ErrNoRows if not found.
func GetUserByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*User, error) {
	row := pool.QueryRow(ctx,
		`SELECT id, email, password_hash, role, created_at
		 FROM users WHERE id = $1`, id)
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByEmail returns the user with the given email address.
// Returns pgx.ErrNoRows if not found.
func GetUserByEmail(ctx context.Context, pool *pgxpool.Pool, email string) (*User, error) {
	row := pool.QueryRow(ctx,
		`SELECT id, email, password_hash, role, created_at
		 FROM users WHERE email = $1`, email)
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// InsertUser creates a new user record. Used during admin bootstrap.
func InsertUser(ctx context.Context, pool *pgxpool.Pool, email, passwordHash, role string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO users (email, password_hash, role) VALUES ($1, $2, $3)`,
		email, passwordHash, role)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

// InsertRefreshToken persists a new refresh token record.
func InsertRefreshToken(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		 VALUES ($1, $2, $3)`,
		userID, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

// GetNonRevokedRefreshTokensByUserID returns all non-revoked, non-expired
// refresh tokens for a user. Used to find a match during token refresh.
func GetNonRevokedRefreshTokensByUserID(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) ([]RefreshToken, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		 FROM refresh_tokens
		 WHERE user_id = $1
		   AND revoked_at IS NULL
		   AND expires_at > NOW()
		 ORDER BY created_at DESC`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("query refresh tokens: %w", err)
	}
	defer rows.Close()

	var tokens []RefreshToken
	for rows.Next() {
		var t RefreshToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan refresh token: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// RevokeRefreshToken marks a refresh token as revoked by setting revoked_at to now.
func RevokeRefreshToken(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	tag, err := pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

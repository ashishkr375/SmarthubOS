// Package auth provides bcrypt hashing at a hardcoded cost of 12.
// The cost cannot be overridden by callers — this is intentional
// to prevent accidental weakening of stored credentials.
//
// Security note: bcrypt silently truncates input at 72 bytes.  All callers
// pass the raw value through SHA-256 first so that long inputs (e.g. hub API
// keys of the form "{uuid}:{hex32}" = 101 bytes) are reduced to a fixed 32-byte
// digest before bcrypt is applied.  The effective security chain is
// bcrypt(SHA-256(secret), cost=12).
package auth

import (
	"crypto/sha256"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the work factor used for all bcrypt operations.
// Hardcoded to 12 per CONSTITUTION.md §23 rule 1.
const bcryptCost = 12

// Hash returns bcrypt(SHA-256(secret), cost=12).
// Using SHA-256 as a pre-hash avoids bcrypt's 72-byte input truncation.
func Hash(secret string) (string, error) {
	digest := sha256.Sum256([]byte(secret))
	hash, err := bcrypt.GenerateFromPassword(digest[:], bcryptCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt hash: %w", err)
	}
	return string(hash), nil
}

// Compare returns true if secret hashes to match the stored bcrypt hash.
func Compare(hash, secret string) bool {
	digest := sha256.Sum256([]byte(secret))
	return bcrypt.CompareHashAndPassword([]byte(hash), digest[:]) == nil
}

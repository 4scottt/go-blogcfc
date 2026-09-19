// Package auth holds the password primitives. Sessions and role checks
// arrive with a later package and live in their own files.
package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is deliberately modest: the admin login is not a hot path,
// and a 128 MiB container should not spend a second on it.
const bcryptCost = 10

// HashPassword returns a bcrypt hash of pw.
func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(h), nil
}

// CheckPassword reports whether pw matches the stored hash.
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

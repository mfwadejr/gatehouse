// Package auth implements accounts, passwords, TOTP, recovery codes and sessions.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const pbkdf2Iterations = 600_000

// HashPassword returns "pbkdf2-sha256$iterations$salt$hash".
func HashPassword(pw string) (string, error) {
	return hashWith(pw, pbkdf2Iterations)
}

func hashWith(pw string, iter int) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, iter, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iter,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks pw against a stored hash in constant time.
func VerifyPassword(pw, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1000 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// CheckPasswordPolicy enforces the minimum standard for new passwords.
func CheckPasswordPolicy(pw, username string) error {
	if len(pw) < 12 {
		return errors.New("password must be at least 12 characters")
	}
	if len(pw) > 256 {
		return errors.New("password is too long")
	}
	if strings.EqualFold(pw, username) {
		return errors.New("password must not match the username")
	}
	return nil
}

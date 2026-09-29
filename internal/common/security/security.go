package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const sessionTokenByteCount = 32

var comparisonOnlyHash, _ = bcrypt.GenerateFromPassword([]byte("balce-timing-equaliser"), bcrypt.DefaultCost)

func NewSessionToken() (string, string, error) {
	randomBytes := make([]byte, sessionTokenByteCount)
	_, readRandomError := rand.Read(randomBytes)
	if readRandomError != nil {
		return "", "", fmt.Errorf("failed to read random bytes: %w", readRandomError)
	}

	sessionToken := base64.RawURLEncoding.EncodeToString(randomBytes)
	tokenHash := HashSessionToken(sessionToken)

	return sessionToken, tokenHash, nil
}

func HashSessionToken(sessionToken string) string {
	tokenDigest := sha256.Sum256([]byte(sessionToken))
	return hex.EncodeToString(tokenDigest[:])
}

func HashPassword(plainPassword string) (string, error) {
	hashedBytes, hashError := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.DefaultCost)
	if hashError != nil {
		return "", fmt.Errorf("failed to hash password: %w", hashError)
	}
	return string(hashedBytes), nil
}

func PasswordMatches(storedHash string, plainPassword string) bool {
	compareError := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(plainPassword))
	return compareError == nil
}

func SpendComparisonTime(plainPassword string) {
	bcrypt.CompareHashAndPassword(comparisonOnlyHash, []byte(plainPassword))
}

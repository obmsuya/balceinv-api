package admin

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	totpStepSeconds = 30
	totpDigits      = 6
	totpSecretBytes = 20
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func NewTotpSecret() (string, error) {
	secretBytes := make([]byte, totpSecretBytes)
	_, readError := rand.Read(secretBytes)
	if readError != nil {
		return "", fmt.Errorf("failed to create the authenticator secret: %w", readError)
	}
	return totpEncoding.EncodeToString(secretBytes), nil
}

func TotpCode(secret string, at time.Time) (string, error) {
	secretBytes, decodeError := totpEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if decodeError != nil {
		return "", fmt.Errorf("the authenticator secret is not valid: %w", decodeError)
	}
	counterBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(counterBytes, uint64(at.Unix()/totpStepSeconds))

	codeHash := hmac.New(sha1.New, secretBytes)
	codeHash.Write(counterBytes)
	hashSum := codeHash.Sum(nil)

	offset := hashSum[len(hashSum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(hashSum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", truncated%1000000), nil
}

func TotpMatches(secret string, typedCode string, now time.Time) bool {
	cleanCode := strings.ReplaceAll(strings.TrimSpace(typedCode), " ", "")
	if len(cleanCode) != totpDigits {
		return false
	}
	for _, stepOffset := range []int{-1, 0, 1} {
		expectedCode, codeError := TotpCode(secret, now.Add(time.Duration(stepOffset*totpStepSeconds)*time.Second))
		if codeError != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(expectedCode), []byte(cleanCode)) == 1 {
			return true
		}
	}
	return false
}

func TotpSetupLink(secret string, email string) string {
	label := url.PathEscape("Balce Admin:" + email)
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", "Balce Admin")
	return "otpauth://totp/" + label + "?" + query.Encode()
}

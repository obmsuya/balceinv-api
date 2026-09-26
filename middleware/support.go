package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

const SupportPasscodeHeader = "X-Support-Passcode"

const (
	supportPasscodeAttemptsBeforeLockout = 5
	supportPasscodeLockoutDuration       = time.Minute
)

type supportPasscodeGuard struct {
	mutex          sync.Mutex
	failedAttempts int
	lockedUntil    time.Time
}

func SupportPasscodeMatches(passcode string, expectedHashHex string) bool {
	expectedHash, decodeError := hex.DecodeString(strings.TrimSpace(expectedHashHex))
	if decodeError != nil || len(expectedHash) != sha256.Size {
		return false
	}
	passcodeHash := sha256.Sum256([]byte(passcode))
	return subtle.ConstantTimeCompare(passcodeHash[:], expectedHash) == 1
}

func RequireSupportPasscode(expectedHashHex string) fiber.Handler {
	guard := &supportPasscodeGuard{}
	return func(context *fiber.Ctx) error {
		if strings.TrimSpace(expectedHashHex) == "" {
			return utils.Error(context, fiber.StatusServiceUnavailable, "Team tools are not set up in this build")
		}

		passcodeAccepted, rejectionStatus, rejectionMessage := guard.check(context.Get(SupportPasscodeHeader), expectedHashHex, time.Now())
		if !passcodeAccepted {
			return utils.Error(context, rejectionStatus, rejectionMessage)
		}
		return context.Next()
	}
}

func (guard *supportPasscodeGuard) check(passcode string, expectedHashHex string, now time.Time) (bool, int, string) {
	guard.mutex.Lock()
	defer guard.mutex.Unlock()

	if now.Before(guard.lockedUntil) {
		return false, fiber.StatusTooManyRequests, "Too many wrong passcodes, wait a minute and try again"
	}
	if !SupportPasscodeMatches(passcode, expectedHashHex) {
		guard.failedAttempts++
		if guard.failedAttempts >= supportPasscodeAttemptsBeforeLockout {
			guard.failedAttempts = 0
			guard.lockedUntil = now.Add(supportPasscodeLockoutDuration)
		}
		return false, fiber.StatusForbidden, "Wrong team passcode"
	}
	guard.failedAttempts = 0
	return true, 0, ""
}

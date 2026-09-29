package httpx

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/gofiber/fiber/v2"
)

const SupportPasscodeHeader = "X-Support-Passcode"

const (
	supportAttemptsBeforeLockout = 5
	supportLockoutDuration       = time.Minute
)

type supportPasscodeGuard struct {
	mutex          sync.Mutex
	expectedHash   []byte
	failedAttempts int
	lockedUntil    time.Time
}

func RequireSupportPasscode(expectedHashHex string) fiber.Handler {
	expectedHash, decodeError := hex.DecodeString(expectedHashHex)
	isConfigured := decodeError == nil && len(expectedHash) == sha256.Size

	guard := &supportPasscodeGuard{
		expectedHash: expectedHash,
	}

	return func(c *fiber.Ctx) error {
		if !isConfigured {
			return response.Error(c, fiber.StatusServiceUnavailable, "team_tools_unavailable", "Team tools are not set up on this installation")
		}

		isAccepted, rejectionStatus, rejectionCode, rejectionMessage := guard.check(c.Get(SupportPasscodeHeader), time.Now())
		if !isAccepted {
			return response.Error(c, rejectionStatus, rejectionCode, rejectionMessage)
		}
		return c.Next()
	}
}

func (guard *supportPasscodeGuard) check(passcode string, now time.Time) (bool, int, string, string) {
	guard.mutex.Lock()
	defer guard.mutex.Unlock()

	isLockedOut := now.Before(guard.lockedUntil)
	if isLockedOut {
		return false, fiber.StatusTooManyRequests, "rate_limited", "Too many wrong passcodes. Wait a minute and try again"
	}

	passcodeHash := sha256.Sum256([]byte(passcode))
	isMatch := subtle.ConstantTimeCompare(passcodeHash[:], guard.expectedHash) == 1
	if !isMatch {
		guard.failedAttempts++
		shouldLockOut := guard.failedAttempts >= supportAttemptsBeforeLockout
		if shouldLockOut {
			guard.failedAttempts = 0
			guard.lockedUntil = now.Add(supportLockoutDuration)
		}
		return false, fiber.StatusForbidden, "wrong_passcode", "Wrong team passcode"
	}

	guard.failedAttempts = 0
	return true, 0, "", ""
}

package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func TestSupportPasscodeGuardLocksAfterRepeatedWrongPasscodes(t *testing.T) {
	passcodeHash := sha256.Sum256([]byte("team-passphrase"))
	expectedHashHex := hex.EncodeToString(passcodeHash[:])
	guard := &supportPasscodeGuard{}
	startTime := time.Now()

	if accepted, _, _ := guard.check("team-passphrase", expectedHashHex, startTime); !accepted {
		t.Fatal("the right passcode should be accepted")
	}
	for attempt := 1; attempt < supportPasscodeAttemptsBeforeLockout; attempt++ {
		if accepted, status, _ := guard.check("guess", expectedHashHex, startTime); accepted || status != 403 {
			t.Fatalf("wrong passcode attempt %d: accepted=%v status=%d", attempt, accepted, status)
		}
	}
	guard.check("guess", expectedHashHex, startTime)
	if accepted, status, _ := guard.check("team-passphrase", expectedHashHex, startTime.Add(time.Second)); accepted || status != 429 {
		t.Fatalf("locked guard: accepted=%v status=%d", accepted, status)
	}
	if accepted, _, _ := guard.check("team-passphrase", expectedHashHex, startTime.Add(supportPasscodeLockoutDuration+time.Second)); !accepted {
		t.Fatal("the right passcode should work after the lockout ends")
	}
	if SupportPasscodeMatches("team-passphrase", "not-hex") || SupportPasscodeMatches("", "") {
		t.Fatal("a broken hash must never match")
	}
}

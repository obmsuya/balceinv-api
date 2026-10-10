package admin

import (
	"encoding/base32"
	"testing"
	"time"
)

func TestTotpMatchesTheRfcReferenceCodes(t *testing.T) {
	rfcSecret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	referenceCodes := map[int64]string{
		59:         "287082",
		1111111109: "081804",
		1111111111: "050471",
		1234567890: "005924",
		2000000000: "279037",
	}
	for unixSeconds, expectedCode := range referenceCodes {
		actualCode, codeError := TotpCode(rfcSecret, time.Unix(unixSeconds, 0))
		if codeError != nil || actualCode != expectedCode {
			t.Fatalf("code at %d = %s (%v), want %s", unixSeconds, actualCode, codeError, expectedCode)
		}
	}

	now := time.Unix(1234567890, 0)
	if !TotpMatches(rfcSecret, "005 924", now) || !TotpMatches(rfcSecret, "005924", now.Add(25*time.Second)) {
		t.Fatal("the current code was refused")
	}
	if TotpMatches(rfcSecret, "005924", now.Add(3*time.Minute)) || TotpMatches(rfcSecret, "12345", now) || TotpMatches("not base32!", "005924", now) {
		t.Fatal("a stale, short or broken code was accepted")
	}
}

package licensing

import "testing"

func TestLicensingReplyReasonNamesWhyThePaymentServerRefused(t *testing.T) {
	replyReasons := map[string]string{
		`{"success":false,"error":"Invalid phone number"}`: "Invalid phone number",
		`{"success":false,"message":"Package not found"}`:  "Package not found",
		`{"detail":"CSRF Failed"}`:                         "CSRF Failed",
		`{"phone":["Enter a valid number"],"amount":[1]}`:  "fields: amount,phone",
		`<html>Bad Gateway</html>`:                         "reply is not JSON",
	}
	for replyBody, expectedReason := range replyReasons {
		actualReason := licensingReplyReason([]byte(replyBody))
		if actualReason != expectedReason {
			t.Fatalf("reply %s gave reason %q, want %q", replyBody, actualReason, expectedReason)
		}
	}
}

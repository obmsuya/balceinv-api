package support_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"mime"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/support"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
	"github.com/wneessen/go-mail"
)

type mailbox struct {
	messages   []*mail.Msg
	nextErrors []error
}

func openMailbox(t *testing.T) *mailbox {
	capturedMail := &mailbox{}
	restoreSendMail := support.CaptureMail(func(outgoingMessage *mail.Msg) error {
		capturedMail.messages = append(capturedMail.messages, outgoingMessage)
		if len(capturedMail.nextErrors) == 0 {
			return nil
		}
		nextError := capturedMail.nextErrors[0]
		capturedMail.nextErrors = capturedMail.nextErrors[1:]
		return nextError
	})
	t.Cleanup(restoreSendMail)
	return capturedMail
}

func configureMail(harness *apptest.Harness) {
	harness.Config.SupportSmtpHost = "smtp.mail.test"
	harness.Config.SupportSmtpPort = 465
	harness.Config.SupportSmtpUsername = "robot@mail.test"
	harness.Config.SupportSmtpPassword = "app-password"
	harness.Config.SupportEmailFrom = "robot@mail.test"
	harness.Config.SupportEmailTo = "team@mail.test"
}

func sendDue(t *testing.T, harness *apptest.Harness) int {
	t.Helper()
	sentCount, sendError := support.NewService(harness.Database, harness.Config, harness.ObjectStore).SendDue(context.Background())
	if sendError != nil {
		t.Fatalf("send due: %v", sendError)
	}
	return sentCount
}

func partContents(t *testing.T, outgoingMessage *mail.Msg) (string, string) {
	t.Helper()
	plainText := ""
	htmlText := ""
	for _, messagePart := range outgoingMessage.GetParts() {
		partBytes, contentError := messagePart.GetContent()
		if contentError != nil {
			t.Fatalf("read part: %v", contentError)
		}
		if messagePart.GetContentType() == mail.TypeTextHTML {
			htmlText = string(partBytes)
		} else {
			plainText = string(partBytes)
		}
	}
	return plainText, htmlText
}

func decodedSubject(t *testing.T, outgoingMessage *mail.Msg) string {
	t.Helper()
	subjectValues := outgoingMessage.GetGenHeader(mail.HeaderSubject)
	if len(subjectValues) != 1 {
		t.Fatalf("subject headers are %v", subjectValues)
	}
	decodedValue, decodeError := new(mime.WordDecoder).DecodeHeader(subjectValues[0])
	if decodeError != nil {
		t.Fatalf("decode subject: %v", decodeError)
	}
	return decodedValue
}

func tinyPngDataUrl(t *testing.T) string {
	t.Helper()
	imageBuffer := bytes.Buffer{}
	encodeError := png.Encode(&imageBuffer, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	if encodeError != nil {
		t.Fatalf("encode png: %v", encodeError)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBuffer.Bytes())
}

func validMessage(overrides map[string]any) map[string]any {
	requestBody := map[string]any{
		"topic":           "problem",
		"message":         "The till stopped printing receipts.",
		"contact_email":   "customer@shop.test",
		"include_details": true,
	}
	for key, value := range overrides {
		requestBody[key] = value
	}
	return requestBody
}

func TestSupportMessageIsEmailedToTheTeam(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		configureMail(harness)
		capturedMail := openMailbox(t)
		company := harness.CreateCompany("Support Shop", "owner@support.test")
		cashierToken := harness.CreateStaff(company, "cashier@support.test", []string{}, []uuid.UUID{company.ShopId})

		submitted := harness.Call(http.MethodPost, "/api/support", cashierToken, validMessage(map[string]any{
			"message":          "Printer broke <script>alert('x')</script>\nSecond line & more",
			"contact_email":    "Customer@Shop.test",
			"contact_phone":    "0712 345 678",
			"app_version":      "2.4.0",
			"operating_system": "Windows 11",
			"device_id":        "HW-123",
			"last_request_id":  "req-9",
			"screenshot":       tinyPngDataUrl(t),
		}))
		if submitted.Status != http.StatusCreated || submitted.Data()["status"] != "sent" || submitted.Data()["configured"] != true {
			t.Fatalf("submit returned %d %v", submitted.Status, submitted.Body)
		}
		if len(capturedMail.messages) != 1 {
			t.Fatalf("%d emails were sent", len(capturedMail.messages))
		}

		sentMail := capturedMail.messages[0]
		fromAddresses := sentMail.GetFromString()
		if len(fromAddresses) != 1 || !strings.Contains(fromAddresses[0], "robot@mail.test") {
			t.Fatalf("from is %v", fromAddresses)
		}
		toAddresses := sentMail.GetAddrHeaderString(mail.HeaderTo)
		if len(toAddresses) != 1 || !strings.Contains(toAddresses[0], "team@mail.test") {
			t.Fatalf("to is %v", toAddresses)
		}
		replyTo := sentMail.GetReplyTo()
		if len(replyTo) != 1 || replyTo[0].Address != "customer@shop.test" {
			t.Fatalf("reply-to is %v", replyTo)
		}
		if subject := decodedSubject(t, sentMail); subject != "[Balce support] Problem — Support Shop (Main Shop)" {
			t.Fatalf("subject is %q", subject)
		}
		hasEnvelopeHeaders := len(sentMail.GetGenHeader(mail.HeaderMessageID)) == 1 && len(sentMail.GetGenHeader(mail.HeaderDate)) == 1
		if !hasEnvelopeHeaders {
			t.Fatal("the email has no Message-ID or Date")
		}
		if len(sentMail.GetAttachments()) != 1 || sentMail.GetAttachments()[0].Name != "screenshot.png" {
			t.Fatalf("attachments are %v", sentMail.GetAttachments())
		}

		plainText, htmlText := partContents(t, sentMail)
		expectedPlatform := "desktop"
		if engineCase.Engine == "postgres" {
			expectedPlatform = "cloud"
		}
		for _, expectedText := range []string{"Support Shop", "Main Shop", "Staff cashier@support.test", "customer@shop.test", "0712 345 678", "2.4.0", "Windows 11", "req-9", "English", expectedPlatform} {
			if !strings.Contains(plainText, expectedText) || !strings.Contains(htmlText, expectedText) {
				t.Fatalf("%q is missing from the email:\n%s\n%s", expectedText, plainText, htmlText)
			}
		}
		for _, expectedLink := range []string{"mailto:customer@shop.test", "tel:&#43;255712345678", "https://wa.me/255712345678", "&lt;script&gt;", "<br>Second line &amp; more"} {
			if !strings.Contains(htmlText, expectedLink) {
				t.Fatalf("%q is missing from the HTML email:\n%s", expectedLink, htmlText)
			}
		}
		if strings.Contains(htmlText, "<script>") {
			t.Fatal("the message was not escaped in the HTML email")
		}
		if !strings.Contains(plainText, "https://wa.me/255712345678") {
			t.Fatalf("the plain email has no WhatsApp link:\n%s", plainText)
		}
		hasDeviceId := strings.Contains(htmlText, "HW-123")
		if hasDeviceId != (expectedPlatform == "desktop") {
			t.Fatalf("device id shown %v on %s", hasDeviceId, expectedPlatform)
		}

		ownMessages := harness.Call(http.MethodGet, "/api/support/messages", cashierToken, nil).Body["data"].([]any)
		if len(ownMessages) != 1 || ownMessages[0].(map[string]any)["status"] != "sent" {
			t.Fatalf("own messages are %v", ownMessages)
		}
		ownerMessages := harness.Call(http.MethodGet, "/api/support/messages", company.OwnerToken, nil).Body["data"].([]any)
		if len(ownerMessages) != 0 {
			t.Fatalf("the owner sees the cashier's messages: %v", ownerMessages)
		}
		otherCompany := harness.CreateCompany("Other Shop", "owner@othersupport.test")
		otherMessages := harness.Call(http.MethodGet, "/api/support/messages", otherCompany.OwnerToken, nil).Body["data"].([]any)
		if len(otherMessages) != 0 {
			t.Fatalf("another company sees support messages: %v", otherMessages)
		}
		if harness.QueryIntForCompany(otherCompany.Id, "SELECT COUNT(*) FROM support_messages") != 0 && engineCase.Engine == "postgres" {
			t.Fatal("row level security lets another company read support messages")
		}
	})
}

func TestSupportMessageWithoutDetailsOrEmail(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		configureMail(harness)
		capturedMail := openMailbox(t)
		company := harness.CreateCompany("Quiet Shop", "owner@quiet.test")

		submitted := harness.Call(http.MethodPost, "/api/support", company.OwnerToken, map[string]any{
			"topic":           "idea",
			"message":         "Please add a dark receipt.",
			"contact_phone":   "+255 754 000 111",
			"include_details": false,
			"app_version":     "9.9.9",
		})
		if submitted.Status != http.StatusCreated || submitted.Data()["status"] != "sent" {
			t.Fatalf("submit returned %d %v", submitted.Status, submitted.Body)
		}

		sentMail := capturedMail.messages[0]
		if len(sentMail.GetReplyTo()) != 0 {
			t.Fatalf("reply-to set without an email: %v", sentMail.GetReplyTo())
		}
		if subject := decodedSubject(t, sentMail); subject != "[Balce support] Idea — Quiet Shop" {
			t.Fatalf("subject is %q", subject)
		}
		plainText, htmlText := partContents(t, sentMail)
		if strings.Contains(htmlText, "9.9.9") || strings.Contains(plainText, "9.9.9") {
			t.Fatal("technical details were sent although they were not allowed")
		}
		if !strings.Contains(htmlText, "https://wa.me/255754000111") || !strings.Contains(plainText, "0754 000 111") {
			t.Fatalf("the phone contact is missing:\n%s", htmlText)
		}
	})
}

func TestSupportMessageValidation(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		configureMail(harness)
		openMailbox(t)
		company := harness.CreateCompany("Checked Shop", "owner@checked.test")

		refusals := []struct {
			overrides    map[string]any
			expectedCode string
		}{
			{map[string]any{"topic": "complaint"}, "validation_failed"},
			{map[string]any{"message": "  hey  "}, "message_length"},
			{map[string]any{"message": strings.Repeat("a", 5001)}, "message_length"},
			{map[string]any{"contact_email": ""}, "contact_required"},
			{map[string]any{"contact_email": "not-an-email"}, "validation_failed"},
			{map[string]any{"contact_phone": "12345"}, "invalid_phone"},
			{map[string]any{"contact_phone": "0812 345 678"}, "invalid_phone"},
			{map[string]any{"screenshot": "data:image/png;base64,bm90IGFuIGltYWdl"}, "invalid_image"},
			{map[string]any{"screenshot": "https://example.com/shot.png"}, "invalid_image"},
		}
		for _, refusal := range refusals {
			refused := harness.Call(http.MethodPost, "/api/support", company.OwnerToken, validMessage(refusal.overrides))
			if refused.Status != http.StatusBadRequest || refused.Code() != refusal.expectedCode {
				t.Fatalf("%v returned %d %v, want %s", refusal.overrides, refused.Status, refused.Body, refusal.expectedCode)
			}
		}

		tooLargeScreenshot := "data:image/png;base64," + base64.StdEncoding.EncodeToString(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 2*1024*1024)...))
		tooLarge := harness.Call(http.MethodPost, "/api/support", company.OwnerToken, validMessage(map[string]any{"screenshot": tooLargeScreenshot}))
		if tooLarge.Status != http.StatusBadRequest || tooLarge.Code() != "image_too_large" {
			t.Fatalf("a large screenshot returned %d %v", tooLarge.Status, tooLarge.Body)
		}

		phoneOnly := harness.Call(http.MethodPost, "/api/support", company.OwnerToken, validMessage(map[string]any{"contact_email": "", "contact_phone": "255712345678"}))
		if phoneOnly.Status != http.StatusCreated {
			t.Fatalf("a phone number alone was refused: %d %v", phoneOnly.Status, phoneOnly.Body)
		}
		if harness.Call(http.MethodPost, "/api/support", "", validMessage(nil)).Status != http.StatusUnauthorized {
			t.Fatal("a signed-out visitor wrote to support")
		}
		if harness.QueryIntForCompany(company.Id, "SELECT COUNT(*) FROM support_messages") != 1 {
			t.Fatal("refused messages were saved")
		}
	})
}

func TestSupportMessagesAreRateLimitedPerCompany(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		configureMail(harness)
		openMailbox(t)
		company := harness.CreateCompany("Busy Shop", "owner@busy.test")
		cashierToken := harness.CreateStaff(company, "cashier@busy.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
		otherCompany := harness.CreateCompany("Calm Shop", "owner@calm.test")

		for messageNumber := 0; messageNumber < 5; messageNumber++ {
			senderToken := company.OwnerToken
			if messageNumber%2 == 1 {
				senderToken = cashierToken
			}
			accepted := harness.Call(http.MethodPost, "/api/support", senderToken, validMessage(nil))
			if accepted.Status != http.StatusCreated {
				t.Fatalf("message %d returned %d %v", messageNumber, accepted.Status, accepted.Body)
			}
		}

		limited := harness.Call(http.MethodPost, "/api/support", cashierToken, validMessage(nil))
		if limited.Status != http.StatusTooManyRequests || limited.Code() != "rate_limited" {
			t.Fatalf("the sixth message returned %d %v", limited.Status, limited.Body)
		}
		if harness.Call(http.MethodPost, "/api/support", otherCompany.OwnerToken, validMessage(nil)).Status != http.StatusCreated {
			t.Fatal("another company was rate limited")
		}

		harness.ExecForCompany(company.Id, "UPDATE support_messages SET created_at = $1", time.Now().UTC().Add(-2*time.Hour))
		if harness.Call(http.MethodPost, "/api/support", cashierToken, validMessage(nil)).Status != http.StatusCreated {
			t.Fatal("the limit did not reset after an hour")
		}
	})
}

func TestSupportMessagesAreRetried(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		configureMail(harness)
		capturedMail := openMailbox(t)
		company := harness.CreateCompany("Retry Shop", "owner@retry.test")

		capturedMail.nextErrors = []error{errors.New("550 mailbox unavailable")}
		failed := harness.Call(http.MethodPost, "/api/support", company.OwnerToken, validMessage(nil))
		if failed.Status != http.StatusCreated || failed.Data()["status"] != "failed" {
			t.Fatalf("a refused email returned %d %v", failed.Status, failed.Body)
		}
		if harness.QueryIntForCompany(company.Id, "SELECT attempts FROM support_messages") != 1 {
			t.Fatal("the failed attempt was not counted")
		}
		if sendDue(t, harness) != 0 || len(capturedMail.messages) != 1 {
			t.Fatal("a failed email was retried before its wait was over")
		}

		harness.ExecForCompany(company.Id, "UPDATE support_messages SET next_attempt_at = $1", time.Now().UTC().Add(-time.Second))
		if sendDue(t, harness) != 1 {
			t.Fatal("a failed email was not retried after its wait")
		}
		sentCount := harness.QueryIntForCompany(company.Id, "SELECT COUNT(*) FROM support_messages WHERE status = 'sent' AND sent_at IS NOT NULL AND last_error IS NULL")
		if sentCount != 1 {
			t.Fatal("the retried email was not recorded as sent")
		}

		capturedMail.nextErrors = []error{&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("no route to host")}}
		offline := harness.Call(http.MethodPost, "/api/support", company.OwnerToken, validMessage(nil))
		if offline.Status != http.StatusCreated || offline.Data()["status"] != "pending" {
			t.Fatalf("an offline email returned %d %v", offline.Status, offline.Body)
		}
		offlineAttempts := harness.QueryIntForCompany(company.Id, "SELECT attempts FROM support_messages WHERE status = 'pending'")
		if offlineAttempts != 0 {
			t.Fatalf("being offline used up %d attempts", offlineAttempts)
		}

		harness.ExecForCompany(company.Id, "UPDATE support_messages SET status = 'sending', updated_at = $1 WHERE status = 'pending'", time.Now().UTC())
		if sendDue(t, harness) != 0 {
			t.Fatal("an email being sent right now was taken again")
		}
		harness.ExecForCompany(company.Id, "UPDATE support_messages SET updated_at = $1 WHERE status = 'sending'", time.Now().UTC().Add(-5*time.Minute))
		if sendDue(t, harness) != 1 {
			t.Fatal("a stale sending email was not picked up again")
		}

		capturedMail.nextErrors = []error{errors.New("535 authentication failed")}
		harness.Call(http.MethodPost, "/api/support", company.OwnerToken, validMessage(nil))
		harness.ExecForCompany(company.Id, "UPDATE support_messages SET attempts = 8, next_attempt_at = $1 WHERE status = 'failed'", time.Now().UTC().Add(-time.Hour))
		if sendDue(t, harness) != 0 {
			t.Fatal("an email past its last attempt was sent again")
		}
	})
}

func TestSupportMessagesWaitWhenEmailIsNotSetUp(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		configureMail(harness)
		harness.Config.SupportSmtpPassword = ""
		capturedMail := openMailbox(t)
		company := harness.CreateCompany("Offline Shop", "owner@offline.test")

		supportStatus := harness.Call(http.MethodGet, "/api/support/status", company.OwnerToken, nil)
		if supportStatus.Status != http.StatusOK || supportStatus.Data()["configured"] != false {
			t.Fatalf("status returned %d %v", supportStatus.Status, supportStatus.Body)
		}
		if strings.Contains(string(supportStatus.Raw), "team@mail.test") {
			t.Fatal("the team address was shown to the app")
		}

		saved := harness.Call(http.MethodPost, "/api/support", company.OwnerToken, validMessage(nil))
		if saved.Status != http.StatusCreated || saved.Data()["status"] != "pending" || saved.Data()["configured"] != false {
			t.Fatalf("submit returned %d %v", saved.Status, saved.Body)
		}
		if sendDue(t, harness) != 0 || len(capturedMail.messages) != 0 {
			t.Fatal("an email was sent without a password")
		}

		harness.Config.SupportSmtpPassword = "app-password"
		if sendDue(t, harness) != 1 {
			t.Fatal("the waiting message was not sent once email was set up")
		}
	})
}

func TestNormalizePhone(t *testing.T) {
	phoneCases := map[string]string{
		"0712 345 678":     "0712345678",
		"+255 712 345 678": "0712345678",
		"255654321000":     "0654321000",
		"712345678":        "0712345678",
		"0812345678":       "",
		"07123":            "",
		"":                 "",
	}
	for rawPhone, expectedPhone := range phoneCases {
		normalizedPhone, isValid := support.NormalizePhone(rawPhone)
		if normalizedPhone != expectedPhone || isValid != (expectedPhone != "") {
			t.Fatalf("%q normalized to %q (%v), want %q", rawPhone, normalizedPhone, isValid, expectedPhone)
		}
	}
}

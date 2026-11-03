package passwordreset_test

import (
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/passwordreset"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/wneessen/go-mail"
)

type capturedDeliveries struct {
	mutex      sync.Mutex
	emailCodes []string
	emailTo    []string
	smsPhones  []string
	smsCodes   []string
}

func withTestMessaging(testConfig *config.Config) {
	testConfig.SupportSmtpHost = "smtp.reset.test"
	testConfig.SupportSmtpPort = 465
	testConfig.SupportSmtpUsername = "balce@reset.test"
	testConfig.SupportSmtpPassword = "test-only-password"
	testConfig.SupportEmailFrom = "balce@reset.test"
	testConfig.BalceSmsApiKey = "test-only-sms-key"
}

var sixDigits = regexp.MustCompile(`\b\d{6}\b`)

func captureDeliveries(t *testing.T) *capturedDeliveries {
	captured := &capturedDeliveries{}
	restore := passwordreset.CaptureDeliveries(
		func(outgoingMessage *mail.Msg) {
			messageText := strings.Builder{}
			outgoingMessage.WriteTo(&messageText)
			captured.mutex.Lock()
			defer captured.mutex.Unlock()
			captured.emailCodes = append(captured.emailCodes, sixDigits.FindString(messageText.String()))
			captured.emailTo = append(captured.emailTo, outgoingMessage.GetToString()...)
		},
		func(phone string, plainCode string) {
			captured.mutex.Lock()
			defer captured.mutex.Unlock()
			captured.smsPhones = append(captured.smsPhones, phone)
			captured.smsCodes = append(captured.smsCodes, plainCode)
		},
	)
	t.Cleanup(restore)
	return captured
}

func (captured *capturedDeliveries) waitForSms(t *testing.T, count int) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		captured.mutex.Lock()
		smsCount := len(captured.smsCodes)
		captured.mutex.Unlock()
		if smsCount >= count {
			captured.mutex.Lock()
			defer captured.mutex.Unlock()
			return captured.smsCodes[count-1]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waited for %d reset SMS messages", count)
	return ""
}

func TestForgottenPasswordIsResetWithACodeByEmailAndSms(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.StartWith(t, engineCase, withTestMessaging)
		captured := captureDeliveries(t)
		company := harness.CreateCompany("Reset Shop", "owner@reset.test")
		harness.ExecForCompany(company.Id, `UPDATE companies SET phone = '0713000123' WHERE id = $1`, company.Id)
		oldSessionToken := harness.MustLogin("owner@reset.test", company.OwnerPassword)

		unknown := harness.Call(http.MethodPost, "/api/auth/password-reset/code", "", map[string]any{"email": "nobody@reset.test"})
		if unknown.Status != http.StatusOK {
			t.Fatalf("an unknown email returned %d %v", unknown.Status, unknown.Body)
		}

		asked := harness.Call(http.MethodPost, "/api/auth/password-reset/code", "", map[string]any{"email": " Owner@Reset.test "})
		if asked.Status != http.StatusOK {
			t.Fatalf("asking for a code returned %d %v", asked.Status, asked.Body)
		}
		smsCode := captured.waitForSms(t, 1)
		captured.mutex.Lock()
		emailCode, emailTo, smsPhone := captured.emailCodes, captured.emailTo, captured.smsPhones[0]
		captured.mutex.Unlock()
		if len(emailCode) != 1 || emailCode[0] != smsCode || emailTo[0] != "<owner@reset.test>" && emailTo[0] != "owner@reset.test" || smsPhone != "0713000123" {
			t.Fatalf("the code went to %v by email %v and %s by SMS %s", emailTo, emailCode, smsPhone, smsCode)
		}

		wrongCode := "000000"
		if smsCode == wrongCode {
			wrongCode = "111111"
		}
		wrong := harness.Call(http.MethodPost, "/api/auth/password-reset", "", map[string]any{"email": "owner@reset.test", "code": wrongCode, "new_password": "brand-new-pass"})
		if wrong.Status != http.StatusBadRequest || wrong.Code() != "invalid_reset_code" {
			t.Fatalf("a wrong code returned %d %v", wrong.Status, wrong.Body)
		}

		reset := harness.Call(http.MethodPost, "/api/auth/password-reset", "", map[string]any{"email": "owner@reset.test", "code": smsCode, "new_password": "brand-new-pass"})
		if reset.Status != http.StatusOK {
			t.Fatalf("the right code returned %d %v", reset.Status, reset.Body)
		}
		if harness.Call(http.MethodGet, "/api/auth/me", oldSessionToken, nil).Status != http.StatusUnauthorized {
			t.Fatal("the old session still works after the reset")
		}
		harness.MustLogin("owner@reset.test", "brand-new-pass")

		reused := harness.Call(http.MethodPost, "/api/auth/password-reset", "", map[string]any{"email": "owner@reset.test", "code": smsCode, "new_password": "another-pass-1"})
		if reused.Code() != "invalid_reset_code" {
			t.Fatalf("a used code returned %d %v", reused.Status, reused.Body)
		}
	})
}

func TestResetCodesStopAfterFiveWrongTriesAndThreeAnHour(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.StartWith(t, engineCase, withTestMessaging)
		captured := captureDeliveries(t)
		company := harness.CreateCompany("Guarded Shop", "owner@guarded.test")
		harness.ExecForCompany(company.Id, `UPDATE companies SET phone = '0713000456' WHERE id = $1`, company.Id)

		harness.Call(http.MethodPost, "/api/auth/password-reset/code", "", map[string]any{"email": "owner@guarded.test"})
		rightCode := captured.waitForSms(t, 1)
		wrongCode := "000000"
		if rightCode == wrongCode {
			wrongCode = "111111"
		}
		for range 5 {
			harness.Call(http.MethodPost, "/api/auth/password-reset", "", map[string]any{"email": "owner@guarded.test", "code": wrongCode, "new_password": "brand-new-pass"})
		}
		lockedOut := harness.Call(http.MethodPost, "/api/auth/password-reset", "", map[string]any{"email": "owner@guarded.test", "code": rightCode, "new_password": "brand-new-pass"})
		if lockedOut.Code() != "invalid_reset_code" {
			t.Fatalf("the right code still worked after five wrong tries: %d %v", lockedOut.Status, lockedOut.Body)
		}

		harness.Call(http.MethodPost, "/api/auth/password-reset/code", "", map[string]any{"email": "owner@guarded.test"})
		harness.Call(http.MethodPost, "/api/auth/password-reset/code", "", map[string]any{"email": "owner@guarded.test"})
		captured.waitForSms(t, 3)
		harness.Call(http.MethodPost, "/api/auth/password-reset/code", "", map[string]any{"email": "owner@guarded.test"})
		time.Sleep(300 * time.Millisecond)
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM password_reset_codes`) != 3 {
			t.Fatal("a fourth code was made within the hour")
		}
	})
}

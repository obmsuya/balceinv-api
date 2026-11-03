package passwordreset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io"
	"net/http"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/mailer"
	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/google/uuid"
	"github.com/wneessen/go-mail"
)

const smsTimeout = 10 * time.Second

type codeContent struct {
	PersonName    string
	BusinessName  string
	Code          string
	MinutesToLive int
}

var (
	errMailNotSetUp = errors.New("this server has no email account set up")
	errSmsNotSetUp  = errors.New("this server has no SMS key set up")
)

var sendEmail = func(ctx context.Context, mailServer mailer.Server, outgoingMessage *mail.Msg) error {
	if !mailServer.IsConfigured() {
		return errMailNotSetUp
	}
	return mailer.Send(ctx, mailServer, outgoingMessage)
}

var smsClient = &http.Client{Timeout: smsTimeout}

func sendCodeEmail(ctx context.Context, mailServer mailer.Server, recipient Recipient, plainCode string) error {
	content := codeContent{
		PersonName:    recipient.PersonName,
		BusinessName:  recipient.BusinessName,
		Code:          plainCode,
		MinutesToLive: int(codeLifetime.Minutes()),
	}
	htmlBody := bytes.Buffer{}
	htmlError := codeHtmlTemplate.Execute(&htmlBody, content)
	if htmlError != nil {
		return fmt.Errorf("failed to render the reset email: %w", htmlError)
	}
	textBody := bytes.Buffer{}
	textError := codeTextTemplate.Execute(&textBody, content)
	if textError != nil {
		return fmt.Errorf("failed to render the plain reset email: %w", textError)
	}

	outgoingMessage := mail.NewMsg()
	fromError := outgoingMessage.FromFormat("Balce", mailServer.From)
	if fromError != nil {
		return fmt.Errorf("the sender address is not valid: %w", fromError)
	}
	toError := outgoingMessage.To(recipient.Email)
	if toError != nil {
		return fmt.Errorf("the person's email address is not valid: %w", toError)
	}
	outgoingMessage.Subject("Balce password reset code / Namba ya kubadili nenosiri")
	outgoingMessage.SetMessageIDWithValue(uuid.NewString() + "@reset.balce")
	outgoingMessage.SetDate()
	outgoingMessage.SetBodyString(mail.TypeTextPlain, textBody.String())
	outgoingMessage.AddAlternativeString(mail.TypeTextHTML, htmlBody.String())
	return sendEmail(ctx, mailServer, outgoingMessage)
}

var sendCodeSms = func(ctx context.Context, apiKey string, phone string, plainCode string) error {
	if apiKey == "" {
		return errSmsNotSetUp
	}
	requestBody, encodeError := json.Marshal(map[string]string{"phone": strings.TrimSpace(phone), "code": plainCode})
	if encodeError != nil {
		return fmt.Errorf("failed to encode the reset SMS: %w", encodeError)
	}
	smsRequest, requestError := http.NewRequestWithContext(ctx, http.MethodPost, license.DjangoBaseURL+"/balce/password-reset-sms/", bytes.NewReader(requestBody))
	if requestError != nil {
		return fmt.Errorf("failed to prepare the reset SMS: %w", requestError)
	}
	smsRequest.Header.Set("Content-Type", "application/json")
	smsRequest.Header.Set("X-Balce-Sms-Key", apiKey)

	smsResponse, sendError := smsClient.Do(smsRequest)
	if sendError != nil {
		return fmt.Errorf("failed to reach the SMS service: %w", sendError)
	}
	defer smsResponse.Body.Close()
	if smsResponse.StatusCode != http.StatusAccepted {
		replyBytes, _ := io.ReadAll(io.LimitReader(smsResponse.Body, 300))
		return fmt.Errorf("the SMS service replied %d: %s", smsResponse.StatusCode, strings.TrimSpace(string(replyBytes)))
	}
	return nil
}

var codeHtmlTemplate = htmltemplate.Must(htmltemplate.New("reset-html").Parse(`<!DOCTYPE html>
<html>
<body style="margin:0;padding:0;background:#f4f5f7;font-family:Arial,Helvetica,sans-serif;color:#1f2937;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f4f5f7;padding:24px 0;">
<tr><td align="center">
<table role="presentation" width="520" cellpadding="0" cellspacing="0" style="max-width:520px;width:100%;background:#ffffff;border-radius:8px;overflow:hidden;">
<tr><td style="background:#0f766e;color:#ffffff;padding:20px 24px;">
<div style="font-size:20px;font-weight:bold;">Balce</div>
<div style="font-size:14px;opacity:0.9;margin-top:4px;">{{.BusinessName}}</div>
</td></tr>
<tr><td style="padding:24px;font-size:15px;line-height:1.6;">
<p style="margin:0 0 12px;">Hello {{.PersonName}}, use this code to set a new password:</p>
<div style="font-size:32px;font-weight:bold;letter-spacing:8px;text-align:center;background:#f0fdfa;border:1px solid #99f6e4;border-radius:8px;padding:16px;margin:8px 0 16px;">{{.Code}}</div>
<p style="margin:0 0 20px;color:#6b7280;">It works for {{.MinutesToLive}} minutes. If you did not ask for it, ignore this email; your password stays the same.</p>
<p style="margin:0 0 12px;">Habari {{.PersonName}}, tumia namba hii kuweka nenosiri jipya. Inafanya kazi kwa dakika {{.MinutesToLive}}.</p>
<p style="margin:0;color:#6b7280;">Kama hukuomba, puuza barua hii; nenosiri lako halitabadilika.</p>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`))

var codeTextTemplate = texttemplate.Must(texttemplate.New("reset-text").Parse(`Balce · {{.BusinessName}}

Hello {{.PersonName}}, use this code to set a new password: {{.Code}}
It works for {{.MinutesToLive}} minutes. If you did not ask for it, ignore this email; your password stays the same.

Habari {{.PersonName}}, tumia namba hii kuweka nenosiri jipya: {{.Code}}
Inafanya kazi kwa dakika {{.MinutesToLive}}. Kama hukuomba, puuza barua hii; nenosiri lako halitabadilika.
`))

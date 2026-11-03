package passwordreset

import (
	"context"

	"github.com/chrisostomemataba/balceinv-api/internal/common/mailer"
	"github.com/wneessen/go-mail"
)

func CaptureDeliveries(onEmail func(outgoingMessage *mail.Msg), onSms func(phone string, plainCode string)) func() {
	realSendEmail := sendEmail
	realSendCodeSms := sendCodeSms
	sendEmail = func(ctx context.Context, mailServer mailer.Server, outgoingMessage *mail.Msg) error {
		onEmail(outgoingMessage)
		return nil
	}
	sendCodeSms = func(ctx context.Context, apiKey string, phone string, plainCode string) error {
		onSms(phone, plainCode)
		return nil
	}
	return func() {
		sendEmail = realSendEmail
		sendCodeSms = realSendCodeSms
	}
}

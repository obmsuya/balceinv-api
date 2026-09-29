package support

import (
	"context"

	"github.com/wneessen/go-mail"
)

func CaptureMail(deliver func(outgoingMessage *mail.Msg) error) func() {
	realSendMail := sendMail
	sendMail = func(ctx context.Context, settings mailSettings, outgoingMessage *mail.Msg) error {
		return deliver(outgoingMessage)
	}
	return func() {
		sendMail = realSendMail
	}
}

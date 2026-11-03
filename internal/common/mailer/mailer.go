package mailer

import (
	"context"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/wneessen/go-mail"
)

const SmtpTimeout = 30 * time.Second

type Server struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

func ServerFrom(loadedConfig *config.Config) Server {
	return Server{
		Host:     loadedConfig.SupportSmtpHost,
		Port:     loadedConfig.SupportSmtpPort,
		Username: loadedConfig.SupportSmtpUsername,
		Password: loadedConfig.SupportSmtpPassword,
		From:     loadedConfig.SupportEmailFrom,
	}
}

func (server Server) IsConfigured() bool {
	return server.Host != "" && server.Port > 0 && server.Username != "" && server.Password != "" && server.From != ""
}

func Send(ctx context.Context, server Server, outgoingMessage *mail.Msg) error {
	clientOptions := []mail.Option{
		mail.WithPort(server.Port),
		mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
		mail.WithUsername(server.Username),
		mail.WithPassword(server.Password),
		mail.WithTimeout(SmtpTimeout),
	}
	if server.Port == mail.DefaultPortSSL {
		clientOptions = append(clientOptions, mail.WithSSL())
	} else {
		clientOptions = append(clientOptions, mail.WithTLSPolicy(mail.TLSMandatory))
	}

	mailClient, clientError := mail.NewClient(server.Host, clientOptions...)
	if clientError != nil {
		return fmt.Errorf("failed to prepare the mail client: %w", clientError)
	}
	sendError := mailClient.DialAndSendWithContext(ctx, outgoingMessage)
	if sendError != nil {
		return fmt.Errorf("failed to send the email: %w", sendError)
	}
	return nil
}

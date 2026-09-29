package support

import (
	"bytes"
	"context"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/wneessen/go-mail"
)

const smtpTimeout = 30 * time.Second

type mailSettings struct {
	host     string
	port     int
	username string
	password string
	from     string
	to       string
}

func mailSettingsFrom(loadedConfig *config.Config) mailSettings {
	return mailSettings{
		host:     loadedConfig.SupportSmtpHost,
		port:     loadedConfig.SupportSmtpPort,
		username: loadedConfig.SupportSmtpUsername,
		password: loadedConfig.SupportSmtpPassword,
		from:     loadedConfig.SupportEmailFrom,
		to:       loadedConfig.SupportEmailTo,
	}
}

func (settings mailSettings) isConfigured() bool {
	return settings.host != "" && settings.port > 0 && settings.username != "" && settings.password != "" && settings.from != "" && settings.to != ""
}

var sendMail = func(ctx context.Context, settings mailSettings, outgoingMessage *mail.Msg) error {
	clientOptions := []mail.Option{
		mail.WithPort(settings.port),
		mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
		mail.WithUsername(settings.username),
		mail.WithPassword(settings.password),
		mail.WithTimeout(smtpTimeout),
	}
	if settings.port == mail.DefaultPortSSL {
		clientOptions = append(clientOptions, mail.WithSSL())
	} else {
		clientOptions = append(clientOptions, mail.WithTLSPolicy(mail.TLSMandatory))
	}

	mailClient, clientError := mail.NewClient(settings.host, clientOptions...)
	if clientError != nil {
		return fmt.Errorf("failed to prepare the mail client: %w", clientError)
	}
	sendError := mailClient.DialAndSendWithContext(ctx, outgoingMessage)
	if sendError != nil {
		return fmt.Errorf("failed to send the support email: %w", sendError)
	}
	return nil
}

type emailContent struct {
	Topic        string
	MessageLines []string
	Details      Details
	Email        string
	EmailLink    htmltemplate.URL
	Phone        string
	PhoneLink    htmltemplate.URL
	WhatsAppLink htmltemplate.URL
	HasShot      bool
	DetailRows   []detailRow
}

type detailRow struct {
	Label string
	Value string
}

func buildEmail(settings mailSettings, supportMessage Message, screenshot *attachment) (*mail.Msg, error) {
	content := emailContentFor(supportMessage, screenshot != nil)

	htmlBody := bytes.Buffer{}
	htmlError := htmlEmailTemplate.Execute(&htmlBody, content)
	if htmlError != nil {
		return nil, fmt.Errorf("failed to render the support email: %w", htmlError)
	}
	textBody := bytes.Buffer{}
	textError := textEmailTemplate.Execute(&textBody, content)
	if textError != nil {
		return nil, fmt.Errorf("failed to render the plain support email: %w", textError)
	}

	outgoingMessage := mail.NewMsg()
	fromError := outgoingMessage.FromFormat("Balce support", settings.from)
	if fromError != nil {
		return nil, fmt.Errorf("the support sender address is not valid: %w", fromError)
	}
	toError := outgoingMessage.To(settings.to)
	if toError != nil {
		return nil, fmt.Errorf("the support team address is not valid: %w", toError)
	}
	if supportMessage.ContactEmail != nil {
		replyToError := outgoingMessage.ReplyToFormat(supportMessage.Details.PersonName, *supportMessage.ContactEmail)
		if replyToError != nil {
			return nil, fmt.Errorf("the contact email is not valid: %w", replyToError)
		}
	}
	outgoingMessage.Subject(subjectFor(supportMessage))
	outgoingMessage.SetMessageIDWithValue(supportMessage.Id.String() + "@support.balce")
	outgoingMessage.SetDate()
	outgoingMessage.SetBodyString(mail.TypeTextPlain, textBody.String())
	outgoingMessage.AddAlternativeString(mail.TypeTextHTML, htmlBody.String())

	if screenshot != nil {
		attachError := outgoingMessage.AttachReader(screenshot.fileName, bytes.NewReader(screenshot.body), mail.WithFileContentType(mail.ContentType(screenshot.contentType)))
		if attachError != nil {
			return nil, fmt.Errorf("failed to attach the screenshot: %w", attachError)
		}
	}
	return outgoingMessage, nil
}

func personWithRole(messageDetails Details) string {
	if messageDetails.PersonRole == "" {
		return messageDetails.PersonName
	}
	return messageDetails.PersonName + " (" + messageDetails.PersonRole + ")"
}

type attachment struct {
	fileName    string
	contentType string
	body        []byte
}

func subjectFor(supportMessage Message) string {
	subject := "[Balce support] " + topicLabels[supportMessage.Topic] + " — " + supportMessage.Details.Business
	if supportMessage.Details.Shop != "" {
		subject += " (" + supportMessage.Details.Shop + ")"
	}
	return subject
}

func emailContentFor(supportMessage Message, hasScreenshot bool) emailContent {
	messageDetails := supportMessage.Details
	content := emailContent{
		Topic:        topicLabels[supportMessage.Topic],
		MessageLines: strings.Split(strings.ReplaceAll(supportMessage.Body, "\r\n", "\n"), "\n"),
		Details:      messageDetails,
		HasShot:      hasScreenshot,
	}
	if supportMessage.ContactEmail != nil {
		content.Email = *supportMessage.ContactEmail
		content.EmailLink = htmltemplate.URL("mailto:" + *supportMessage.ContactEmail)
	}
	if supportMessage.ContactPhone != nil {
		internationalPhone := InternationalPhone(*supportMessage.ContactPhone)
		content.Phone = SpacedPhone(*supportMessage.ContactPhone)
		content.PhoneLink = htmltemplate.URL("tel:+" + internationalPhone)
		content.WhatsAppLink = htmltemplate.URL("https://wa.me/" + internationalPhone)
	}

	candidateRows := []detailRow{
		{Label: "Business", Value: messageDetails.Business},
		{Label: "Shop", Value: messageDetails.Shop},
		{Label: "Person", Value: personWithRole(messageDetails)},
		{Label: "App version", Value: messageDetails.AppVersion},
		{Label: "Platform", Value: messageDetails.Platform},
		{Label: "Operating system", Value: messageDetails.OperatingSystem},
		{Label: "Device id", Value: messageDetails.DeviceId},
		{Label: "Language", Value: messageDetails.Language},
		{Label: "Time", Value: messageDetails.RequestTime},
		{Label: "Last error request id", Value: messageDetails.LastErrorRequestId},
	}
	for _, candidateRow := range candidateRows {
		if candidateRow.Value != "" {
			content.DetailRows = append(content.DetailRows, candidateRow)
		}
	}
	return content
}

var htmlEmailTemplate = htmltemplate.Must(htmltemplate.New("support-html").Parse(`<!DOCTYPE html>
<html>
<body style="margin:0;padding:0;background:#f4f5f7;font-family:Arial,Helvetica,sans-serif;color:#1f2937;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f4f5f7;padding:24px 0;">
<tr><td align="center">
<table role="presentation" width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%;background:#ffffff;border-radius:8px;overflow:hidden;">
<tr><td style="background:#0f766e;color:#ffffff;padding:20px 24px;">
<div style="font-size:20px;font-weight:bold;">Balce support</div>
<div style="font-size:14px;opacity:0.9;margin-top:4px;">{{.Topic}} from {{.Details.Business}}{{if .Details.Shop}} ({{.Details.Shop}}){{end}}</div>
</td></tr>
<tr><td style="padding:24px;">
<div style="font-size:12px;text-transform:uppercase;letter-spacing:0.05em;color:#6b7280;margin-bottom:8px;">Message</div>
<div style="background:#fff7ed;border-left:4px solid #f97316;padding:16px;font-size:15px;line-height:1.5;">{{range $index, $line := .MessageLines}}{{if $index}}<br>{{end}}{{$line}}{{end}}</div>
</td></tr>
<tr><td style="padding:0 24px 24px;">
<div style="font-size:12px;text-transform:uppercase;letter-spacing:0.05em;color:#6b7280;margin-bottom:8px;">Contact</div>
<div style="font-size:15px;line-height:1.8;">
<strong>{{.Details.PersonName}}</strong>{{if .Details.PersonRole}} · {{.Details.PersonRole}}{{end}}<br>
{{if .Email}}Email: <a href="{{.EmailLink}}" style="color:#0f766e;">{{.Email}}</a><br>{{end}}
{{if .Phone}}Phone: <a href="{{.PhoneLink}}" style="color:#0f766e;">{{.Phone}}</a> · <a href="{{.WhatsAppLink}}" style="color:#16a34a;">WhatsApp</a><br>{{end}}
</div>
</td></tr>
<tr><td style="padding:0 24px 24px;">
<div style="font-size:12px;text-transform:uppercase;letter-spacing:0.05em;color:#6b7280;margin-bottom:8px;">Details</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="font-size:14px;border-collapse:collapse;">
{{range .DetailRows}}<tr><td style="padding:6px 8px;border-bottom:1px solid #e5e7eb;color:#6b7280;width:40%;">{{.Label}}</td><td style="padding:6px 8px;border-bottom:1px solid #e5e7eb;">{{.Value}}</td></tr>
{{end}}</table>
{{if .HasShot}}<p style="font-size:13px;color:#6b7280;margin-top:16px;">A screenshot is attached.</p>{{end}}
</td></tr>
<tr><td style="background:#f9fafb;color:#9ca3af;font-size:12px;padding:12px 24px;">Sent from the Balce app. Reply to this email to answer the customer{{if not .Email}} (no email given — call or WhatsApp them){{end}}.</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`))

var textEmailTemplate = texttemplate.Must(texttemplate.New("support-text").Parse(`BALCE SUPPORT — {{.Topic}}

MESSAGE
{{range .MessageLines}}{{.}}
{{end}}
CONTACT
{{.Details.PersonName}}{{if .Details.PersonRole}} ({{.Details.PersonRole}}){{end}}
{{if .Email}}Email: {{.Email}}
{{end}}{{if .Phone}}Phone: {{.Phone}}
WhatsApp: {{.WhatsAppLink}}
{{end}}
DETAILS
{{range .DetailRows}}{{.Label}}: {{.Value}}
{{end}}{{if .HasShot}}
A screenshot is attached.
{{end}}`))

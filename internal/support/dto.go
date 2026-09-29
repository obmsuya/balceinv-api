package support

import (
	"time"

	"github.com/google/uuid"
)

type SubmitRequest struct {
	Topic           string `json:"topic" validate:"required,oneof=question problem billing idea"`
	Message         string `json:"message" validate:"required,max=6000"`
	ContactEmail    string `json:"contact_email" validate:"omitempty,max=254,email"`
	ContactPhone    string `json:"contact_phone" validate:"max=30"`
	IncludeDetails  bool   `json:"include_details"`
	Screenshot      string `json:"screenshot" validate:"max=3000000"`
	AppVersion      string `json:"app_version" validate:"max=40"`
	OperatingSystem string `json:"operating_system" validate:"max=160"`
	DeviceId        string `json:"device_id" validate:"max=128"`
	LastRequestId   string `json:"last_request_id" validate:"max=64"`
}

type SubmitView struct {
	Id         uuid.UUID `json:"id"`
	Status     string    `json:"status"`
	Configured bool      `json:"configured"`
}

type MessageView struct {
	Id        uuid.UUID  `json:"id"`
	Topic     string     `json:"topic"`
	Preview   string     `json:"preview"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	SentAt    *time.Time `json:"sent_at"`
}

type StatusView struct {
	Configured bool `json:"configured"`
}

func toMessageView(message Message) MessageView {
	previewRunes := []rune(message.Body)
	preview := message.Body
	if len(previewRunes) > 140 {
		preview = string(previewRunes[:140]) + "…"
	}
	return MessageView{
		Id:        message.Id,
		Topic:     message.Topic,
		Preview:   preview,
		Status:    message.Status,
		CreatedAt: message.CreatedAt,
		SentAt:    message.SentAt,
	}
}

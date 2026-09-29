package support

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	StatusPending = "pending"
	StatusSending = "sending"
	StatusSent    = "sent"
	StatusFailed  = "failed"
)

var (
	ErrMessageLength      = errors.New("write between 5 and 5000 characters")
	ErrContactRequired    = errors.New("give an email or a phone number so the team can reach you")
	ErrInvalidPhone       = errors.New("the phone number must be a Tanzanian mobile number like 0712 345 678")
	ErrInvalidScreenshot  = errors.New("the screenshot must be a PNG, JPEG or WebP image")
	ErrScreenshotTooLarge = errors.New("the screenshot must be 2 MB or smaller")
	ErrRateLimited        = errors.New("too many support messages from this business in the last hour. Try again later")
	ErrUserNotFound       = errors.New("the signed-in user could not be found")
)

var topicLabels = map[string]string{
	"question": "Question",
	"problem":  "Problem",
	"billing":  "Billing",
	"idea":     "Idea",
}

type Message struct {
	Id             uuid.UUID
	CompanyId      uuid.UUID
	ShopId         *uuid.UUID
	UserId         uuid.UUID
	Topic          string
	Body           string
	ContactEmail   *string
	ContactPhone   *string
	IncludeDetails bool
	Details        Details
	ScreenshotKey  *string
	Status         string
	Attempts       int
	LastError      *string
	NextAttemptAt  time.Time
	SentAt         *time.Time
	CreatedAt      time.Time
}

type Details struct {
	Business           string `json:"business"`
	Shop               string `json:"shop,omitempty"`
	PersonName         string `json:"person_name"`
	PersonRole         string `json:"person_role"`
	RequestTime        string `json:"request_time"`
	AppVersion         string `json:"app_version,omitempty"`
	Platform           string `json:"platform,omitempty"`
	OperatingSystem    string `json:"operating_system,omitempty"`
	DeviceId           string `json:"device_id,omitempty"`
	Language           string `json:"language,omitempty"`
	LastErrorRequestId string `json:"last_error_request_id,omitempty"`
}

type Author struct {
	CompanyName string
	Timezone    string
	UserName    string
	Language    string
	ShopName    *string
}

type dueMessage struct {
	CompanyId uuid.UUID
	MessageId uuid.UUID
}

func NormalizePhone(rawPhone string) (string, bool) {
	phoneDigits := strings.Map(func(character rune) rune {
		if character >= '0' && character <= '9' {
			return character
		}
		return -1
	}, rawPhone)

	localDigits := phoneDigits
	if strings.HasPrefix(phoneDigits, "255") {
		localDigits = "0" + phoneDigits[3:]
	} else if strings.HasPrefix(phoneDigits, "6") || strings.HasPrefix(phoneDigits, "7") {
		localDigits = "0" + phoneDigits
	}

	isMobileNumber := len(localDigits) == 10 && (strings.HasPrefix(localDigits, "06") || strings.HasPrefix(localDigits, "07"))
	if !isMobileNumber {
		return "", false
	}
	return localDigits, true
}

func InternationalPhone(localPhone string) string {
	return "255" + strings.TrimPrefix(localPhone, "0")
}

func SpacedPhone(localPhone string) string {
	if len(localPhone) != 10 {
		return localPhone
	}
	return localPhone[:4] + " " + localPhone[4:7] + " " + localPhone[7:]
}

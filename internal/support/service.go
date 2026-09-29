package support

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/google/uuid"
)

const (
	screenshotFolder       = "support"
	maximumScreenshotBytes = 2 * 1024 * 1024
	minimumMessageLength   = 5
	maximumMessageLength   = 5000
	messagesPerHour        = 5
	maximumAttempts        = 8
	longestBackoff         = 6 * time.Hour
	offlineRetryAfter      = time.Minute
	staleSendingAfter      = 2 * time.Minute
	sendingInterval        = time.Minute
	dueBatchSize           = 20
	immediateSendWait      = 8 * time.Second
	recentMessagesLimit    = 10
	lastErrorLimit         = 300
)

var extensionByContentType = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/webp": "webp",
}

var languageNames = map[string]string{
	"en": "English",
	"sw": "Kiswahili",
}

type Service struct {
	openDatabase *database.Database
	loadedConfig *config.Config
	objectStore  storage.Store
	repository   *Repository
}

func NewService(openDatabase *database.Database, loadedConfig *config.Config, objectStore storage.Store) *Service {
	return &Service{
		openDatabase: openDatabase,
		loadedConfig: loadedConfig,
		objectStore:  objectStore,
		repository:   NewRepository(),
	}
}

func (service *Service) Status() StatusView {
	return StatusView{Configured: mailSettingsFrom(service.loadedConfig).isConfigured()}
}

func (service *Service) ListMine(ctx context.Context, querier database.Querier, principal *identity.Principal) ([]MessageView, error) {
	foundMessages, listError := service.repository.ListForUser(ctx, querier, principal.CompanyId, principal.UserId, recentMessagesLimit)
	if listError != nil {
		return nil, listError
	}
	messageViews := make([]MessageView, 0, len(foundMessages))
	for _, foundMessage := range foundMessages {
		messageViews = append(messageViews, toMessageView(foundMessage))
	}
	return messageViews, nil
}

func (service *Service) Submit(ctx context.Context, principal *identity.Principal, request SubmitRequest) (SubmitView, error) {
	messageText := strings.TrimSpace(request.Message)
	messageLength := utf8.RuneCountInString(messageText)
	if messageLength < minimumMessageLength || messageLength > maximumMessageLength {
		return SubmitView{}, ErrMessageLength
	}

	contactEmail := optionalText(strings.ToLower(strings.TrimSpace(request.ContactEmail)))
	var contactPhone *string
	trimmedPhone := strings.TrimSpace(request.ContactPhone)
	if trimmedPhone != "" {
		normalizedPhone, isValidPhone := NormalizePhone(trimmedPhone)
		if !isValidPhone {
			return SubmitView{}, ErrInvalidPhone
		}
		contactPhone = &normalizedPhone
	}
	if contactEmail == nil && contactPhone == nil {
		return SubmitView{}, ErrContactRequired
	}

	screenshotKey, screenshotError := service.storeScreenshot(ctx, principal.CompanyId, request.Screenshot)
	if screenshotError != nil {
		return SubmitView{}, screenshotError
	}

	createdAt := time.Now().UTC()
	newMessage := Message{
		Id:             uuid.Must(uuid.NewV7()),
		CompanyId:      principal.CompanyId,
		ShopId:         principal.ShopId,
		UserId:         principal.UserId,
		Topic:          request.Topic,
		Body:           messageText,
		ContactEmail:   contactEmail,
		ContactPhone:   contactPhone,
		IncludeDetails: request.IncludeDetails,
		ScreenshotKey:  screenshotKey,
		Status:         StatusPending,
		CreatedAt:      createdAt,
	}

	_, saveError := inTenant(ctx, service.openDatabase, principal.CompanyId, func(tenantTransaction database.Querier) (bool, error) {
		recentCount, countError := service.repository.CountSince(ctx, tenantTransaction, principal.CompanyId, createdAt.Add(-time.Hour))
		if countError != nil {
			return false, countError
		}
		if recentCount >= messagesPerHour {
			return false, ErrRateLimited
		}

		author, authorError := service.repository.FindAuthor(ctx, tenantTransaction, principal.CompanyId, principal.UserId, principal.ShopId)
		if authorError != nil {
			return false, authorError
		}
		newMessage.Details = service.detailsFor(author, principal, request, createdAt)

		insertError := service.repository.Insert(ctx, tenantTransaction, newMessage)
		return insertError == nil, insertError
	})
	if saveError != nil {
		service.forgetScreenshot(ctx, screenshotKey)
		return SubmitView{}, saveError
	}

	sendFinished := make(chan struct{})
	go func() {
		defer close(sendFinished)
		service.sendOne(context.Background(), principal.CompanyId, newMessage.Id)
	}()
	select {
	case <-sendFinished:
	case <-time.After(immediateSendWait):
	}

	currentStatus, statusError := inTenant(ctx, service.openDatabase, principal.CompanyId, func(tenantTransaction database.Querier) (string, error) {
		savedMessage, findError := service.repository.Find(ctx, tenantTransaction, principal.CompanyId, newMessage.Id)
		if findError != nil {
			return "", findError
		}
		if savedMessage == nil {
			return StatusPending, nil
		}
		return savedMessage.Status, nil
	})
	if statusError != nil {
		return SubmitView{}, statusError
	}

	submitView := SubmitView{
		Id:         newMessage.Id,
		Status:     currentStatus,
		Configured: service.Status().Configured,
	}
	return submitView, nil
}

func (service *Service) SendDue(ctx context.Context) (int, error) {
	if !mailSettingsFrom(service.loadedConfig).isConfigured() {
		return 0, nil
	}

	dueMessages, listError := service.listDue(ctx)
	if listError != nil {
		return 0, listError
	}

	sentCount := 0
	for _, waitingMessage := range dueMessages {
		isSent := service.sendOne(ctx, waitingMessage.CompanyId, waitingMessage.MessageId)
		if isSent {
			sentCount++
		}
	}
	return sentCount, nil
}

func (service *Service) StartSending(ctx context.Context) {
	go func() {
		sendingTicker := time.NewTicker(sendingInterval)
		defer sendingTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-sendingTicker.C:
			}
			_, sendError := service.SendDue(ctx)
			if sendError != nil {
				slog.Warn("support messages could not be sent", "error", sendError)
			}
		}
	}()
}

func (service *Service) listDue(ctx context.Context) ([]dueMessage, error) {
	outboxTransaction, beginError := service.openDatabase.Reader.BeginTx(ctx, nil)
	if beginError != nil {
		return nil, fmt.Errorf("failed to begin reading the support outbox: %w", beginError)
	}
	defer outboxTransaction.Rollback()

	openError := service.repository.OpenOutbox(ctx, outboxTransaction, service.openDatabase.IsPostgres())
	if openError != nil {
		return nil, openError
	}

	now := time.Now().UTC()
	return service.repository.ListDue(ctx, outboxTransaction, now, now.Add(-staleSendingAfter), maximumAttempts, dueBatchSize)
}

func (service *Service) sendOne(ctx context.Context, companyId uuid.UUID, messageId uuid.UUID) bool {
	settings := mailSettingsFrom(service.loadedConfig)
	if !settings.isConfigured() {
		return false
	}

	claimedMessage, claimError := inTenant(ctx, service.openDatabase, companyId, func(tenantTransaction database.Querier) (*Message, error) {
		claimedAt := time.Now().UTC()
		isClaimed, claimUpdateError := service.repository.Claim(ctx, tenantTransaction, companyId, messageId, claimedAt, claimedAt.Add(-staleSendingAfter), maximumAttempts)
		if claimUpdateError != nil || !isClaimed {
			return nil, claimUpdateError
		}
		return service.repository.Find(ctx, tenantTransaction, companyId, messageId)
	})
	if claimError != nil {
		slog.Error("support message could not be claimed", "companyId", companyId, "messageId", messageId, "error", claimError)
		return false
	}
	if claimedMessage == nil {
		return false
	}

	startedAt := time.Now()
	sendError := service.deliver(ctx, settings, *claimedMessage)
	callDuration := time.Since(startedAt)
	attemptedMessage := withAttemptResult(*claimedMessage, sendError, time.Now().UTC())
	if sendError != nil {
		slog.Warn("support email not sent",
			"companyId", companyId,
			"messageId", messageId,
			"status", attemptedMessage.Status,
			"attempts", attemptedMessage.Attempts,
			"durationMs", callDuration.Milliseconds(),
			"error", sendError,
		)
	} else {
		slog.Info("support email sent", "companyId", companyId, "messageId", messageId, "durationMs", callDuration.Milliseconds())
	}

	_, recordError := inTenant(ctx, service.openDatabase, companyId, func(tenantTransaction database.Querier) (bool, error) {
		updateError := service.repository.RecordAttempt(ctx, tenantTransaction, attemptedMessage, time.Now().UTC())
		return updateError == nil, updateError
	})
	if recordError != nil {
		slog.Error("support message result could not be saved", "companyId", companyId, "messageId", messageId, "error", recordError)
		return false
	}
	return sendError == nil
}

func (service *Service) deliver(ctx context.Context, settings mailSettings, claimedMessage Message) error {
	screenshot := service.loadScreenshot(ctx, claimedMessage)
	outgoingMessage, buildError := buildEmail(settings, claimedMessage, screenshot)
	if buildError != nil {
		return buildError
	}

	sendContext, cancelSend := context.WithTimeout(ctx, 2*smtpTimeout)
	defer cancelSend()
	return sendMail(sendContext, settings, outgoingMessage)
}

func withAttemptResult(claimedMessage Message, sendError error, now time.Time) Message {
	attemptedMessage := claimedMessage
	if sendError == nil {
		attemptedMessage.Status = StatusSent
		attemptedMessage.SentAt = &now
		attemptedMessage.LastError = nil
		return attemptedMessage
	}

	var networkError net.Error
	isOffline := errors.As(sendError, &networkError)
	if isOffline {
		attemptedMessage.Status = StatusPending
		attemptedMessage.NextAttemptAt = now.Add(offlineRetryAfter)
		attemptedMessage.LastError = optionalText("Could not reach the mail server. It will be sent again.")
		return attemptedMessage
	}

	attemptedMessage.Attempts++
	attemptedMessage.Status = StatusFailed
	attemptedMessage.NextAttemptAt = now.Add(backoffAfter(attemptedMessage.Attempts))
	attemptedMessage.LastError = limitedText(sendError.Error(), lastErrorLimit)
	return attemptedMessage
}

func backoffAfter(attempts int) time.Duration {
	backoff := time.Minute << attempts
	if backoff <= 0 || backoff > longestBackoff {
		return longestBackoff
	}
	return backoff
}

func (service *Service) detailsFor(author Author, principal *identity.Principal, request SubmitRequest, createdAt time.Time) Details {
	companyLocation, locationError := time.LoadLocation(author.Timezone)
	if locationError != nil {
		companyLocation = time.UTC
	}
	localTime := createdAt.In(companyLocation)

	messageDetails := Details{
		Business:    author.CompanyName,
		PersonName:  author.UserName,
		PersonRole:  principal.RoleName,
		RequestTime: localTime.Format("2 Jan 2006 15:04 MST") + " (" + companyLocation.String() + ")",
	}
	if !request.IncludeDetails {
		return messageDetails
	}

	if author.ShopName != nil {
		messageDetails.Shop = *author.ShopName
	}
	messageDetails.AppVersion = strings.TrimSpace(request.AppVersion)
	messageDetails.Language = languageNames[author.Language]
	messageDetails.LastErrorRequestId = strings.TrimSpace(request.LastRequestId)
	messageDetails.OperatingSystem = strings.TrimSpace(request.OperatingSystem)
	messageDetails.Platform = "cloud"
	if !service.openDatabase.IsPostgres() {
		messageDetails.Platform = "desktop"
		messageDetails.DeviceId = strings.TrimSpace(request.DeviceId)
		if messageDetails.OperatingSystem == "" {
			messageDetails.OperatingSystem = runtime.GOOS + "/" + runtime.GOARCH
		}
	}
	return messageDetails
}

func (service *Service) storeScreenshot(ctx context.Context, companyId uuid.UUID, dataUrl string) (*string, error) {
	trimmedUrl := strings.TrimSpace(dataUrl)
	if trimmedUrl == "" {
		return nil, nil
	}

	header, encodedImage, hasComma := strings.Cut(trimmedUrl, ",")
	isBase64DataUrl := hasComma && strings.HasPrefix(header, "data:") && strings.HasSuffix(header, ";base64")
	if !isBase64DataUrl {
		return nil, ErrInvalidScreenshot
	}
	imageBytes, decodeError := base64.StdEncoding.DecodeString(encodedImage)
	if decodeError != nil {
		return nil, ErrInvalidScreenshot
	}

	objectKey, storeError := media.StoreImage(ctx, service.objectStore, screenshotFolder, companyId, imageBytes, maximumScreenshotBytes)
	if errors.Is(storeError, media.ErrImageTooLarge) {
		return nil, ErrScreenshotTooLarge
	}
	if errors.Is(storeError, media.ErrEmptyImage) || errors.Is(storeError, media.ErrUnsupportedImage) {
		return nil, ErrInvalidScreenshot
	}
	if storeError != nil {
		return nil, storeError
	}
	return &objectKey, nil
}

func (service *Service) forgetScreenshot(ctx context.Context, screenshotKey *string) {
	if screenshotKey == nil {
		return
	}
	deleteError := service.objectStore.Delete(ctx, *screenshotKey)
	if deleteError != nil {
		slog.Warn("unused support screenshot was not removed", "key", *screenshotKey, "error", deleteError)
	}
}

func (service *Service) loadScreenshot(ctx context.Context, claimedMessage Message) *attachment {
	if claimedMessage.ScreenshotKey == nil {
		return nil
	}
	storedObject, getError := service.objectStore.Get(ctx, *claimedMessage.ScreenshotKey)
	if getError != nil {
		slog.Warn("support screenshot is missing, sending without it", "messageId", claimedMessage.Id, "error", getError)
		return nil
	}
	detectedContentType := http.DetectContentType(storedObject.Body)
	return &attachment{
		fileName:    "screenshot." + extensionByContentType[detectedContentType],
		contentType: detectedContentType,
		body:        storedObject.Body,
	}
}

func inTenant[Result any](ctx context.Context, openDatabase *database.Database, companyId uuid.UUID, work func(tenantTransaction database.Querier) (Result, error)) (Result, error) {
	var emptyResult Result

	tenantTransaction, beginError := openDatabase.Writer.BeginTx(ctx, nil)
	if beginError != nil {
		return emptyResult, fmt.Errorf("failed to begin the support transaction: %w", beginError)
	}
	defer tenantTransaction.Rollback()

	setTenantError := database.SetTenant(ctx, tenantTransaction, openDatabase.IsPostgres(), companyId)
	if setTenantError != nil {
		return emptyResult, setTenantError
	}

	workResult, workError := work(tenantTransaction)
	if workError != nil {
		return emptyResult, workError
	}

	commitError := tenantTransaction.Commit()
	if commitError != nil {
		return emptyResult, fmt.Errorf("failed to commit the support transaction: %w", commitError)
	}
	return workResult, nil
}

func optionalText(rawText string) *string {
	if rawText == "" {
		return nil
	}
	return &rawText
}

func limitedText(rawText string, maximumLength int) *string {
	textRunes := []rune(strings.TrimSpace(rawText))
	if len(textRunes) > maximumLength {
		textRunes = textRunes[:maximumLength]
	}
	return optionalText(string(textRunes))
}

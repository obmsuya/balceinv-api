package sales

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/google/uuid"
)

const (
	fiscalRequestTimeout  = 15 * time.Second
	fiscalStaleAfter      = 2 * time.Minute
	fiscalSendWaitingSize = 20
	fiscalResponseLimit   = 64 * 1024
	fiscalErrorLimit      = 300
)

var fiscalTransport = http.DefaultTransport

var (
	ErrEfdOff          = errors.New("EFD is turned off in settings")
	ErrEfdNotReady     = errors.New("add the EFD address and key in settings first")
	ErrFiscalNotQueued = errors.New("this sale was made while EFD was off, so it is not sent")
)

type FiscalService struct {
	openDatabase       *database.Database
	salesService       *Service
	repository         *Repository
	settingsRepository *settings.Repository
	httpClient         *http.Client
}

func NewFiscalService(openDatabase *database.Database, salesService *Service, repository *Repository, settingsRepository *settings.Repository) *FiscalService {
	return &FiscalService{
		openDatabase:       openDatabase,
		salesService:       salesService,
		repository:         repository,
		settingsRepository: settingsRepository,
		httpClient:         newFiscalClient(openDatabase.IsPostgres()),
	}
}

func newFiscalClient(isCloud bool) *http.Client {
	chosenTransport := fiscalTransport
	isRealTransport := fiscalTransport == http.DefaultTransport
	if isCloud && isRealTransport {
		publicDialer := &net.Dialer{
			Timeout: fiscalRequestTimeout,
			Control: refusePrivateAddresses,
		}
		chosenTransport = &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         publicDialer.DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
		}
	}
	return &http.Client{
		Timeout:   fiscalRequestTimeout,
		Transport: chosenTransport,
		CheckRedirect: func(redirectedRequest *http.Request, previousRequests []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

var sharedAddressSpace = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func refusePrivateAddresses(network string, address string, rawConnection syscall.RawConn) error {
	hostText, _, splitError := net.SplitHostPort(address)
	if splitError != nil {
		return splitError
	}
	dialedIp := net.ParseIP(hostText)
	if !IsPublicAddress(dialedIp) {
		return fmt.Errorf("the EFD address %s is not on the public internet", hostText)
	}
	return nil
}

func IsPublicAddress(dialedIp net.IP) bool {
	if dialedIp == nil {
		return false
	}
	isReserved := dialedIp.IsLoopback() || dialedIp.IsPrivate() || dialedIp.IsLinkLocalUnicast() || dialedIp.IsLinkLocalMulticast() ||
		dialedIp.IsUnspecified() || dialedIp.IsMulticast() || dialedIp.IsInterfaceLocalMulticast() || sharedAddressSpace.Contains(dialedIp)
	return !isReserved
}

type fiscalTarget struct {
	endpoint       string
	apiKey         string
	idempotencyKey string
	payload        FiscalPayload
}

type FiscalPayload struct {
	DocumentType          string              `json:"document_type"`
	SaleId                uuid.UUID           `json:"sale_id"`
	ReceiptNumber         string              `json:"receipt_number"`
	OriginalReceiptNumber *string             `json:"original_receipt_number,omitempty"`
	OriginalVerification  *string             `json:"original_verification_code,omitempty"`
	Reason                *string             `json:"reason,omitempty"`
	IssuedAt              time.Time           `json:"issued_at"`
	LocalDate             string              `json:"local_date"`
	LocalTime             string              `json:"local_time"`
	Timezone              string              `json:"timezone"`
	Seller                FiscalSeller        `json:"seller"`
	ShopName              string              `json:"shop_name"`
	Currency              FiscalCurrency      `json:"currency"`
	Items                 []FiscalPayloadItem `json:"items"`
	Totals                FiscalPayloadTotals `json:"totals"`
	Payments              []PaymentView       `json:"payments"`
}

type FiscalSeller struct {
	Name    string  `json:"name"`
	Tin     *string `json:"tin"`
	Address *string `json:"address"`
	Phone   *string `json:"phone"`
}

type FiscalCurrency struct {
	Code     string `json:"code"`
	Decimals int    `json:"decimals"`
}

type FiscalPayloadItem struct {
	Code               string `json:"code"`
	Description        string `json:"description"`
	Quantity           int    `json:"quantity"`
	UnitPrice          int64  `json:"unit_price"`
	Discount           int64  `json:"discount"`
	Total              int64  `json:"total"`
	TaxRateBasisPoints int    `json:"tax_rate_basis_points"`
}

type FiscalPayloadTotals struct {
	Subtotal          int64 `json:"subtotal"`
	Discount          int64 `json:"discount"`
	Total             int64 `json:"total"`
	Tax               int64 `json:"tax"`
	TotalExcludingTax int64 `json:"total_excluding_tax"`
}

type fiscalAnswer struct {
	VerificationCode string `json:"verification_code"`
	VerificationUrl  string `json:"verification_url"`
}

func (service *FiscalService) Send(ctx context.Context, companyId uuid.UUID, saleId uuid.UUID) (FiscalView, error) {
	return service.sendDocument(ctx, FiscalReceipt, companyId, saleId)
}

func (service *FiscalService) sendDocument(ctx context.Context, document FiscalDocument, companyId uuid.UUID, saleId uuid.UUID) (FiscalView, error) {
	target, isClaimed, claimError := service.claim(ctx, document, companyId, saleId)
	if claimError != nil {
		return FiscalView{}, claimError
	}
	if !isClaimed {
		return service.current(ctx, document, companyId, saleId)
	}

	fiscalResult := service.post(ctx, companyId, target)

	recordError := service.record(ctx, document, companyId, saleId, fiscalResult)
	if recordError != nil {
		return FiscalView{}, recordError
	}
	return service.current(ctx, document, companyId, saleId)
}

func (service *FiscalService) SendWaiting(ctx context.Context, companyId uuid.UUID) (SendWaitingView, error) {
	sendSummary := SendWaitingView{}
	for _, document := range []FiscalDocument{FiscalReceipt, FiscalCreditNote} {
		waitingSaleIds, listError := inTenant(ctx, service.openDatabase, companyId, func(tenantTransaction database.Querier) ([]uuid.UUID, error) {
			staleBefore := time.Now().UTC().Add(-fiscalStaleAfter)
			return service.repository.ListWaitingFiscal(ctx, tenantTransaction, document, companyId, staleBefore, fiscalSendWaitingSize)
		})
		if listError != nil {
			return SendWaitingView{}, listError
		}

		for _, waitingSaleId := range waitingSaleIds {
			fiscalView, sendError := service.sendDocument(ctx, document, companyId, waitingSaleId)
			if sendError != nil {
				return SendWaitingView{}, sendError
			}
			if fiscalView.Status == "sent" {
				sendSummary.Sent++
			} else {
				sendSummary.Failed++
			}
		}

		stillWaiting, countError := inTenant(ctx, service.openDatabase, companyId, func(tenantTransaction database.Querier) (int64, error) {
			return service.repository.CountWaitingFiscal(ctx, tenantTransaction, document, companyId)
		})
		if countError != nil {
			return SendWaitingView{}, countError
		}
		sendSummary.StillWaiting += stillWaiting
	}
	return sendSummary, nil
}

func (service *FiscalService) claim(ctx context.Context, document FiscalDocument, companyId uuid.UUID, saleId uuid.UUID) (fiscalTarget, bool, error) {
	claimTransaction, beginError := service.openDatabase.Writer.BeginTx(ctx, nil)
	if beginError != nil {
		return fiscalTarget{}, false, fmt.Errorf("failed to begin the EFD claim: %w", beginError)
	}
	defer claimTransaction.Rollback()

	setTenantError := database.SetTenant(ctx, claimTransaction, service.openDatabase.IsPostgres(), companyId)
	if setTenantError != nil {
		return fiscalTarget{}, false, setTenantError
	}

	companySettings, settingsError := service.salesService.findSettings(ctx, claimTransaction, companyId)
	if settingsError != nil {
		return fiscalTarget{}, false, settingsError
	}
	if !companySettings.EfdEnabled {
		return fiscalTarget{}, false, ErrEfdOff
	}
	if companySettings.EfdEndpoint == nil || companySettings.EfdApiKey == nil {
		return fiscalTarget{}, false, ErrEfdNotReady
	}

	existingFiscal, findError := service.repository.FindFiscal(ctx, claimTransaction, document, companyId, saleId)
	if findError != nil {
		return fiscalTarget{}, false, findError
	}
	if existingFiscal == nil {
		_, saleError := service.salesService.Get(ctx, claimTransaction, companyId, saleId)
		if saleError != nil {
			return fiscalTarget{}, false, saleError
		}
		return fiscalTarget{}, false, ErrFiscalNotQueued
	}

	claimedAt := time.Now().UTC()
	isClaimed, claimError := service.repository.ClaimFiscal(ctx, claimTransaction, document, companyId, saleId, claimedAt, claimedAt.Add(-fiscalStaleAfter))
	if claimError != nil {
		return fiscalTarget{}, false, claimError
	}
	if !isClaimed {
		return fiscalTarget{}, false, nil
	}

	saleView, saleError := service.salesService.Get(ctx, claimTransaction, companyId, saleId)
	if saleError != nil {
		return fiscalTarget{}, false, saleError
	}
	companyProfile, profileError := service.settingsRepository.FindCompanyProfile(ctx, claimTransaction, companyId)
	if profileError != nil {
		return fiscalTarget{}, false, profileError
	}
	if companyProfile == nil {
		return fiscalTarget{}, false, ErrMissingSettings
	}

	commitError := claimTransaction.Commit()
	if commitError != nil {
		return fiscalTarget{}, false, fmt.Errorf("failed to commit the EFD claim: %w", commitError)
	}

	fiscalPayload := BuildFiscalPayload(saleView, *companyProfile)
	idempotencyKey := saleView.Id.String()
	if document == FiscalCreditNote {
		fiscalPayload = BuildCreditNotePayload(fiscalPayload, saleView)
		idempotencyKey += ":credit-note"
	}
	target := fiscalTarget{
		endpoint:       *companySettings.EfdEndpoint,
		apiKey:         *companySettings.EfdApiKey,
		idempotencyKey: idempotencyKey,
		payload:        fiscalPayload,
	}
	return target, true, nil
}

func (service *FiscalService) post(ctx context.Context, companyId uuid.UUID, target fiscalTarget) FiscalResult {
	payloadBytes, marshalError := json.Marshal(target.payload)
	if marshalError != nil {
		return failedResult(fmt.Sprintf("Could not build the EFD receipt: %v", marshalError))
	}

	fiscalRequest, requestError := http.NewRequestWithContext(ctx, http.MethodPost, target.endpoint, bytes.NewReader(payloadBytes))
	if requestError != nil {
		return failedResult("The EFD address is not valid")
	}
	fiscalRequest.Header.Set("Content-Type", "application/json")
	fiscalRequest.Header.Set("Accept", "application/json")
	fiscalRequest.Header.Set("Authorization", "Bearer "+target.apiKey)
	fiscalRequest.Header.Set("Idempotency-Key", target.idempotencyKey)

	slog.Info("calling EFD",
		"companyId", companyId,
		"document", target.payload.DocumentType,
		"saleId", target.payload.SaleId,
		"receiptNumber", target.payload.ReceiptNumber,
	)
	startedAt := time.Now()
	fiscalResponse, sendError := service.httpClient.Do(fiscalRequest)
	callDuration := time.Since(startedAt)
	if sendError != nil {
		slog.Warn("EFD unreachable",
			"companyId", companyId,
			"saleId", target.payload.SaleId,
			"durationMs", callDuration.Milliseconds(),
			"error", sendError,
		)
		return failedResult("Could not reach the EFD. It will be sent again.")
	}
	defer fiscalResponse.Body.Close()

	responseBytes, readError := io.ReadAll(io.LimitReader(fiscalResponse.Body, fiscalResponseLimit))
	slog.Info("EFD responded",
		"companyId", companyId,
		"saleId", target.payload.SaleId,
		"status", fiscalResponse.StatusCode,
		"durationMs", callDuration.Milliseconds(),
	)
	if readError != nil {
		return failedResult("The EFD answer could not be read. It will be sent again.")
	}

	isAccepted := fiscalResponse.StatusCode >= 200 && fiscalResponse.StatusCode < 300
	if !isAccepted {
		answerText := strings.TrimSpace(string(responseBytes))
		return failedResult(fmt.Sprintf("The EFD refused the receipt (%d): %s", fiscalResponse.StatusCode, answerText))
	}

	answer := fiscalAnswer{}
	unmarshalError := json.Unmarshal(responseBytes, &answer)
	if unmarshalError != nil {
		answer = fiscalAnswer{}
	}

	sentAt := time.Now().UTC()
	acceptedResult := FiscalResult{
		Status:           "sent",
		VerificationCode: limitedTextOrNil(answer.VerificationCode, 200),
		VerificationUrl:  httpsUrlOrNil(answer.VerificationUrl),
		SentAt:           &sentAt,
	}
	return acceptedResult
}

func (service *FiscalService) record(ctx context.Context, document FiscalDocument, companyId uuid.UUID, saleId uuid.UUID, fiscalResult FiscalResult) error {
	_, recordError := inTenant(ctx, service.openDatabase, companyId, func(tenantTransaction database.Querier) (bool, error) {
		updateError := service.repository.RecordFiscalResult(ctx, tenantTransaction, document, companyId, saleId, fiscalResult, time.Now().UTC())
		return updateError == nil, updateError
	})
	return recordError
}

func (service *FiscalService) current(ctx context.Context, document FiscalDocument, companyId uuid.UUID, saleId uuid.UUID) (FiscalView, error) {
	return inTenant(ctx, service.openDatabase, companyId, func(tenantTransaction database.Querier) (FiscalView, error) {
		foundFiscal, findError := service.repository.FindFiscal(ctx, tenantTransaction, document, companyId, saleId)
		if findError != nil {
			return FiscalView{}, findError
		}
		if foundFiscal == nil {
			return FiscalView{}, ErrSaleNotFound
		}
		return *foundFiscal, nil
	})
}

func inTenant[Result any](ctx context.Context, openDatabase *database.Database, companyId uuid.UUID, work func(tenantTransaction database.Querier) (Result, error)) (Result, error) {
	var emptyResult Result

	tenantTransaction, beginError := openDatabase.Writer.BeginTx(ctx, nil)
	if beginError != nil {
		return emptyResult, fmt.Errorf("failed to begin the EFD transaction: %w", beginError)
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
		return emptyResult, fmt.Errorf("failed to commit the EFD transaction: %w", commitError)
	}
	return workResult, nil
}

func BuildFiscalPayload(saleView SaleView, companyProfile settings.CompanyProfile) FiscalPayload {
	companyLocation, locationError := time.LoadLocation(companyProfile.Timezone)
	if locationError != nil {
		companyLocation = time.UTC
	}
	localIssuedAt := saleView.CreatedAt.In(companyLocation)

	payloadItems := []FiscalPayloadItem{}
	for _, lineView := range saleView.Items {
		description := lineView.ProductName
		if lineView.VariantLabel != "" {
			description += " " + lineView.VariantLabel
		}
		for _, addonView := range lineView.Addons {
			description += " + " + addonView.Name
		}
		payloadItem := FiscalPayloadItem{
			Code:               lineView.Sku,
			Description:        description,
			Quantity:           lineView.Quantity,
			UnitPrice:          lineView.UnitPrice + lineView.AddonsUnitTotal,
			Discount:           lineView.DiscountAmount,
			Total:              lineView.LineTotal,
			TaxRateBasisPoints: saleView.TaxRateBasisPoints,
		}
		payloadItems = append(payloadItems, payloadItem)
	}

	fiscalPayload := FiscalPayload{
		DocumentType:  "receipt",
		SaleId:        saleView.Id,
		ReceiptNumber: saleView.ReceiptNumber,
		IssuedAt:      saleView.CreatedAt.UTC(),
		LocalDate:     localIssuedAt.Format("2006-01-02"),
		LocalTime:     localIssuedAt.Format("15:04:05"),
		Timezone:      companyLocation.String(),
		Seller: FiscalSeller{
			Name:    companyProfile.Name,
			Tin:     companyProfile.Tin,
			Address: companyProfile.Address,
			Phone:   companyProfile.Phone,
		},
		ShopName: saleView.ShopName,
		Currency: FiscalCurrency{
			Code:     saleView.CurrencyCode,
			Decimals: saleView.CurrencyDecimals,
		},
		Items: payloadItems,
		Totals: FiscalPayloadTotals{
			Subtotal:          saleView.Subtotal,
			Discount:          saleView.DiscountTotal,
			Total:             saleView.Total,
			Tax:               saleView.TaxTotal,
			TotalExcludingTax: saleView.Total - saleView.TaxTotal,
		},
		Payments: saleView.Payments,
	}
	return fiscalPayload
}

func BuildCreditNotePayload(receiptPayload FiscalPayload, saleView SaleView) FiscalPayload {
	originalReceiptNumber := saleView.ReceiptNumber
	creditNotePayload := receiptPayload
	creditNotePayload.DocumentType = "credit_note"
	creditNotePayload.OriginalReceiptNumber = &originalReceiptNumber
	creditNotePayload.Reason = saleView.VoidReason
	if saleView.Fiscal != nil {
		creditNotePayload.OriginalVerification = saleView.Fiscal.VerificationCode
	}
	if saleView.VoidedAt != nil {
		companyLocation, locationError := time.LoadLocation(receiptPayload.Timezone)
		if locationError != nil {
			companyLocation = time.UTC
		}
		localVoidedAt := saleView.VoidedAt.In(companyLocation)
		creditNotePayload.IssuedAt = saleView.VoidedAt.UTC()
		creditNotePayload.LocalDate = localVoidedAt.Format("2006-01-02")
		creditNotePayload.LocalTime = localVoidedAt.Format("15:04:05")
	}
	return creditNotePayload
}

func failedResult(problem string) FiscalResult {
	return FiscalResult{
		Status:    "failed",
		LastError: limitedTextOrNil(problem, fiscalErrorLimit),
	}
}

func limitedTextOrNil(rawText string, maxLength int) *string {
	trimmedText := strings.TrimSpace(rawText)
	if trimmedText == "" {
		return nil
	}
	textRunes := []rune(trimmedText)
	if len(textRunes) > maxLength {
		trimmedText = string(textRunes[:maxLength])
	}
	return &trimmedText
}

func httpsUrlOrNil(rawUrl string) *string {
	trimmedUrl := strings.TrimSpace(rawUrl)
	parsedUrl, parseError := url.Parse(trimmedUrl)
	isUsableUrl := parseError == nil && parsedUrl.Scheme == "https" && parsedUrl.Host != "" && len(trimmedUrl) <= 500
	if !isUsableUrl {
		return nil
	}
	return &trimmedUrl
}

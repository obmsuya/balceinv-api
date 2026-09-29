package accounting

import (
	"errors"
	"io"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

func (handler *Handler) RequireAccounting(needsFullMode bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		accountingMode, modeError := handler.service.Mode(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId)
		if modeError != nil {
			return modeError
		}
		isOff := accountingMode == ModeOff || (needsFullMode && accountingMode != ModeFull)
		if isOff {
			return respondWithServiceError(c, ErrFeatureOff)
		}
		return c.Next()
	}
}

func (handler *Handler) Status(c *fiber.Ctx) error {
	statusView, statusError := handler.service.Status(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c))
	if statusError != nil {
		return respondWithServiceError(c, statusError)
	}
	return response.Success(c, "Books status", statusView)
}

func (handler *Handler) Start(c *fiber.Ctx) error {
	request := StartRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	statusView, startError := handler.service.Start(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if startError != nil {
		return respondWithServiceError(c, startError)
	}
	return response.Created(c, "Books started", statusView)
}

func (handler *Handler) CatchUp(c *fiber.Ctx) error {
	catchUpView, catchUpError := handler.service.CatchUp(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c))
	if catchUpError != nil {
		return respondWithServiceError(c, catchUpError)
	}
	return response.Success(c, "Missed records added to the books", catchUpView)
}

func (handler *Handler) RecordMoney(c *fiber.Ctx) error {
	request := MoneyRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	entryView, isNew, recordError := handler.service.RecordMoney(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if recordError != nil {
		return respondWithServiceError(c, recordError)
	}
	if !isNew {
		return response.Success(c, "Already recorded", entryView)
	}
	return response.Created(c, "Recorded", entryView)
}

func (handler *Handler) Reverse(c *fiber.Ctx) error {
	entryId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrEntryNotFound)
	}
	request := ReverseRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	entryView, reverseError := handler.service.Reverse(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), entryId, request)
	if reverseError != nil {
		return respondWithServiceError(c, reverseError)
	}
	return response.Created(c, "Entry reversed", entryView)
}

func (handler *Handler) PostManual(c *fiber.Ctx) error {
	request := ManualRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	entryView, isNew, postError := handler.service.PostManual(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if postError != nil {
		return respondWithServiceError(c, postError)
	}
	if !isNew {
		return response.Success(c, "Already recorded", entryView)
	}
	return response.Created(c, "Entry posted", entryView)
}

func (handler *Handler) ClosePeriod(c *fiber.Ctx) error {
	request := CloseRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	statusView, closeError := handler.service.ClosePeriod(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if closeError != nil {
		return respondWithServiceError(c, closeError)
	}
	return response.Success(c, "Period closed", statusView)
}

func (handler *Handler) Accounts(c *fiber.Ctx) error {
	accountViews, listError := handler.service.Accounts(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c))
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Success(c, "Accounts", accountViews)
}

func (handler *Handler) CreateAccount(c *fiber.Ctx) error {
	request := AccountRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	accountView, createError := handler.service.CreateAccount(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if createError != nil {
		return respondWithServiceError(c, createError)
	}
	return response.Created(c, "Account added", accountView)
}

func (handler *Handler) UpdateAccount(c *fiber.Ctx) error {
	accountId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrAccountNotFound)
	}
	request := AccountUpdateRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	accountView, updateError := handler.service.UpdateAccount(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), accountId, request)
	if updateError != nil {
		return respondWithServiceError(c, updateError)
	}
	return response.Success(c, "Account saved", accountView)
}

func (handler *Handler) Entries(c *fiber.Ctx) error {
	pagination := httpx.ParsePagination(c)
	entryFilter, filterError := parseEntryFilter(c)
	if filterError != nil {
		return respondWithServiceError(c, filterError)
	}
	entryPage, listError := handler.service.Entries(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), entryFilter, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Entries", entryPage)
}

func (handler *Handler) Entry(c *fiber.Ctx) error {
	entryId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrEntryNotFound)
	}
	entryView, findError := handler.service.Entry(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), entryId)
	if findError != nil {
		return respondWithServiceError(c, findError)
	}
	return response.Success(c, "Entry", entryView)
}

func (handler *Handler) UploadReceipt(c *fiber.Ctx) error {
	uploadedFile, formFileError := c.FormFile("image")
	if formFileError != nil {
		return response.Error(c, fiber.StatusBadRequest, "bad_request", "Attach the photo as a field named image")
	}
	if uploadedFile.Size > maximumReceiptBytes {
		return respondWithServiceError(c, media.ErrImageTooLarge)
	}
	openedFile, openError := uploadedFile.Open()
	if openError != nil {
		return response.Error(c, fiber.StatusBadRequest, "bad_request", "The file could not be read")
	}
	defer openedFile.Close()
	imageBytes, readError := io.ReadAll(io.LimitReader(openedFile, maximumReceiptBytes+1))
	if readError != nil {
		return response.Error(c, fiber.StatusBadRequest, "bad_request", "The file could not be read")
	}

	uploadView, uploadError := handler.service.UploadReceipt(c.UserContext(), httpx.CurrentPrincipal(c), imageBytes)
	if uploadError != nil {
		return respondWithServiceError(c, uploadError)
	}
	return response.Created(c, "Receipt photo saved", uploadView)
}

func (handler *Handler) Receipt(c *fiber.Ctx) error {
	entryId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrEntryNotFound)
	}
	storedObject, getError := handler.service.Receipt(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), entryId)
	if errors.Is(getError, storage.ErrObjectNotFound) {
		return respondWithServiceError(c, ErrEntryNotFound)
	}
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	c.Set(fiber.HeaderContentType, storedObject.ContentType)
	c.Set(fiber.HeaderCacheControl, "private, max-age=3600")
	return c.Send(storedObject.Body)
}

func (handler *Handler) Overview(c *fiber.Ctx) error {
	overview, reportError := handler.service.Overview(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), reportRequest(c))
	if reportError != nil {
		return respondWithServiceError(c, reportError)
	}
	return response.Success(c, "Overview", overview)
}

func (handler *Handler) ProfitAndLoss(c *fiber.Ctx) error {
	report, reportError := handler.service.ProfitAndLoss(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), reportRequest(c))
	if reportError != nil {
		return respondWithServiceError(c, reportError)
	}
	return response.Success(c, "Profit and loss", report)
}

func (handler *Handler) BalanceSheet(c *fiber.Ctx) error {
	report, reportError := handler.service.BalanceSheet(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), c.Query("as_of"))
	if reportError != nil {
		return respondWithServiceError(c, reportError)
	}
	return response.Success(c, "Balance sheet", report)
}

func (handler *Handler) TrialBalance(c *fiber.Ctx) error {
	report, reportError := handler.service.TrialBalance(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), c.Query("as_of"))
	if reportError != nil {
		return respondWithServiceError(c, reportError)
	}
	return response.Success(c, "Trial balance", report)
}

func (handler *Handler) Statement(c *fiber.Ctx) error {
	report, reportError := handler.service.Statement(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), reportRequest(c), c.Query("account"))
	if reportError != nil {
		return respondWithServiceError(c, reportError)
	}
	return response.Success(c, "Account statement", report)
}

func (handler *Handler) VatReport(c *fiber.Ctx) error {
	report, reportError := handler.service.VatReport(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), reportRequest(c))
	if reportError != nil {
		return respondWithServiceError(c, reportError)
	}
	return response.Success(c, "VAT report", report)
}

func (handler *Handler) Integrity(c *fiber.Ctx) error {
	report, reportError := handler.service.Integrity(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), reportRequest(c))
	if reportError != nil {
		return respondWithServiceError(c, reportError)
	}
	return response.Success(c, "Books check", report)
}

func reportRequest(c *fiber.Ctx) ReportRequest {
	return ReportRequest{
		FromDate: c.Query("from"),
		ToDate:   c.Query("to"),
		Shop:     c.Query("shop"),
	}
}

func parseEntryFilter(c *fiber.Ctx) (EntryFilter, error) {
	entryFilter := EntryFilter{
		FromDate: c.Query("from"),
		ToDate:   c.Query("to"),
	}
	for _, rawDate := range []string{entryFilter.FromDate, entryFilter.ToDate} {
		if rawDate != "" && !datePattern.MatchString(rawDate) {
			return EntryFilter{}, ErrInvalidDate
		}
	}
	rawSources := c.Query("source_type")
	if rawSources != "" {
		entryFilter.SourceTypes = strings.Split(rawSources, ",")
	}
	for _, rawId := range []struct {
		value  string
		target **uuid.UUID
	}{{c.Query("account_id"), &entryFilter.AccountId}, {c.Query("shop"), &entryFilter.ShopId}} {
		if rawId.value == "" {
			continue
		}
		parsedId, parseError := uuid.Parse(rawId.value)
		if parseError != nil {
			return EntryFilter{}, ErrInvalidFilter
		}
		*rawId.target = &parsedId
	}
	return entryFilter, nil
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	status, code, isKnown := statusFor(serviceError)
	if !isKnown {
		return serviceError
	}
	return response.Error(c, status, code, serviceError.Error())
}

func statusFor(serviceError error) (int, string, bool) {
	errorResponses := []struct {
		target error
		status int
		code   string
	}{
		{ErrFeatureOff, fiber.StatusForbidden, "feature_off"},
		{ErrNotStarted, fiber.StatusConflict, "books_not_started"},
		{ErrAlreadyStarted, fiber.StatusConflict, "books_already_started"},
		{ErrBeforeStart, fiber.StatusBadRequest, "before_books_start"},
		{ErrPeriodClosed, fiber.StatusConflict, "period_closed"},
		{ErrFutureDate, fiber.StatusBadRequest, "future_date"},
		{ErrInvalidDate, fiber.StatusBadRequest, "invalid_date"},
		{ErrInvalidFilter, fiber.StatusBadRequest, "invalid_filter"},
		{ErrRangeTooLong, fiber.StatusBadRequest, "range_too_long"},
		{ErrUnbalanced, fiber.StatusBadRequest, "entry_unbalanced"},
		{ErrTooFewLines, fiber.StatusBadRequest, "entry_lines_invalid"},
		{ErrInvalidLine, fiber.StatusBadRequest, "entry_lines_invalid"},
		{ErrAccountNotUsable, fiber.StatusBadRequest, "account_not_usable"},
		{ErrUnknownMethod, fiber.StatusBadRequest, "unknown_payment_method"},
		{ErrEntryNotFound, fiber.StatusNotFound, "not_found"},
		{ErrAccountNotFound, fiber.StatusNotFound, "not_found"},
		{ErrShopNotFound, fiber.StatusNotFound, "not_found"},
		{ErrNotReversible, fiber.StatusConflict, "not_reversible"},
		{ErrAlreadyReversed, fiber.StatusConflict, "already_reversed"},
		{ErrSameMoneyAccount, fiber.StatusBadRequest, "same_money_account"},
		{ErrExpenseAccount, fiber.StatusBadRequest, "expense_account_required"},
		{ErrVatTooLarge, fiber.StatusBadRequest, "vat_too_large"},
		{ErrCodeTaken, fiber.StatusConflict, "account_code_taken"},
		{ErrSystemAccount, fiber.StatusConflict, "system_account"},
		{ErrCloseBackwards, fiber.StatusBadRequest, "close_backwards"},
		{ErrCloseTooRecent, fiber.StatusBadRequest, "close_too_recent"},
		{ErrClientRefReused, fiber.StatusConflict, "entry_ref_reused"},
		{ErrInvalidAttachment, fiber.StatusBadRequest, "invalid_attachment"},
		{media.ErrEmptyImage, fiber.StatusBadRequest, "invalid_image"},
		{media.ErrUnsupportedImage, fiber.StatusBadRequest, "invalid_image"},
		{media.ErrImageTooLarge, fiber.StatusRequestEntityTooLarge, "receipt_too_large"},
	}
	for _, errorResponse := range errorResponses {
		if errors.Is(serviceError, errorResponse.target) {
			return errorResponse.status, errorResponse.code, true
		}
	}
	return 0, "", false
}

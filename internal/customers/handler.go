package customers

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/documents"
	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

func (handler *Handler) List(c *fiber.Ctx) error {
	pagination := httpx.ParsePagination(c)
	customerFilter := CustomerFilter{
		SearchText:      c.Query("q"),
		IncludeInactive: c.Query("include_inactive") == "true",
		SortRecent:      c.Query("sort") == "recent",
	}

	customerPage, listError := handler.service.List(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), customerFilter, pagination.Limit, pagination.Offset)
	if listError != nil {
		return RespondWithServiceError(c, listError)
	}
	return response.Paged(c, "Customers", customerPage)
}

func (handler *Handler) Debtors(c *fiber.Ctx) error {
	if c.Query("format") != "" {
		debtorsFile, documentError := handler.service.DebtorsDocument(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), DebtorsDocumentRequest{
			Format:   c.Query("format"),
			Language: c.Query("lang"),
			AsOf:     c.Query("as_of"),
		})
		if documentError != nil {
			return RespondWithServiceError(c, documentError)
		}
		return httpx.SendFile(c, debtorsFile.Name, debtorsFile.ContentType, debtorsFile.Bytes)
	}
	debtorsView, debtorsError := handler.service.Debtors(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), c.Query("as_of"))
	if debtorsError != nil {
		return RespondWithServiceError(c, debtorsError)
	}
	return response.Success(c, "Customers who owe", debtorsView)
}

func (handler *Handler) Get(c *fiber.Ctx) error {
	customerId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return RespondWithServiceError(c, ErrCustomerNotFound)
	}

	customerView, getError := handler.service.Get(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId, customerId)
	if getError != nil {
		return RespondWithServiceError(c, getError)
	}
	return response.Success(c, "Customer", customerView)
}

func (handler *Handler) Create(c *fiber.Ctx) error {
	request := CustomerRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	customerView, createError := handler.service.Create(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if createError != nil {
		return RespondWithServiceError(c, createError)
	}
	return response.Created(c, "Customer added", customerView)
}

func (handler *Handler) Update(c *fiber.Ctx) error {
	customerId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return RespondWithServiceError(c, ErrCustomerNotFound)
	}

	request := CustomerRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	customerView, updateError := handler.service.Update(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), customerId, request)
	if updateError != nil {
		return RespondWithServiceError(c, updateError)
	}
	return response.Success(c, "Customer saved", customerView)
}

func (handler *Handler) Deactivate(c *fiber.Ctx) error {
	customerId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return RespondWithServiceError(c, ErrCustomerNotFound)
	}

	customerView, deactivateError := handler.service.Deactivate(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), customerId)
	if deactivateError != nil {
		return RespondWithServiceError(c, deactivateError)
	}
	return response.Success(c, "Customer turned off", customerView)
}

func (handler *Handler) Restore(c *fiber.Ctx) error {
	customerId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return RespondWithServiceError(c, ErrCustomerNotFound)
	}

	customerView, restoreError := handler.service.Restore(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), customerId)
	if restoreError != nil {
		return RespondWithServiceError(c, restoreError)
	}
	return response.Success(c, "Customer turned back on", customerView)
}

func (handler *Handler) Sales(c *fiber.Ctx) error {
	customerId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return RespondWithServiceError(c, ErrCustomerNotFound)
	}

	pagination := httpx.ParsePagination(c)
	salePage, listError := handler.service.ListSales(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId, customerId, pagination.Limit, pagination.Offset)
	if listError != nil {
		return RespondWithServiceError(c, listError)
	}
	return response.Paged(c, "Customer purchases", salePage)
}

func (handler *Handler) Payments(c *fiber.Ctx) error {
	customerId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return RespondWithServiceError(c, ErrCustomerNotFound)
	}

	paymentViews, listError := handler.service.ListPayments(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId, customerId)
	if listError != nil {
		return RespondWithServiceError(c, listError)
	}
	return response.Success(c, "Customer payments", paymentViews)
}

func (handler *Handler) RecordPayment(c *fiber.Ctx) error {
	customerId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return RespondWithServiceError(c, ErrCustomerNotFound)
	}

	request := PaymentRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	paymentView, recordError := handler.service.RecordPayment(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), customerId, request)
	if recordError != nil {
		return RespondWithServiceError(c, recordError)
	}
	return response.Created(c, "Payment recorded", paymentView)
}

func (handler *Handler) VoidPayment(c *fiber.Ctx) error {
	customerId, isValidCustomerId := httpx.UuidParam(c, "id")
	if !isValidCustomerId {
		return RespondWithServiceError(c, ErrCustomerNotFound)
	}
	paymentId, isValidPaymentId := httpx.UuidParam(c, "paymentId")
	if !isValidPaymentId {
		return RespondWithServiceError(c, ErrPaymentNotFound)
	}

	request := VoidRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	paymentView, voidError := handler.service.VoidPayment(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), customerId, paymentId, request)
	if voidError != nil {
		return RespondWithServiceError(c, voidError)
	}
	return response.Success(c, "Payment cancelled", paymentView)
}

func (handler *Handler) Statement(c *fiber.Ctx) error {
	customerId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return RespondWithServiceError(c, ErrCustomerNotFound)
	}

	if c.Query("format") != "" {
		statementFile, documentError := handler.service.StatementDocument(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), customerId, StatementDocumentRequest{
			Format:   c.Query("format"),
			Language: c.Query("lang"),
			From:     c.Query("from"),
			To:       c.Query("to"),
		})
		if documentError != nil {
			return RespondWithServiceError(c, documentError)
		}
		return httpx.SendFile(c, statementFile.Name, statementFile.ContentType, statementFile.Bytes)
	}
	statementView, statementError := handler.service.Statement(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), customerId, c.Query("from"), c.Query("to"))
	if statementError != nil {
		return RespondWithServiceError(c, statementError)
	}
	return response.Success(c, "Customer statement", statementView)
}

func RespondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, documents.ErrUnknownFormat):
		return response.Error(c, fiber.StatusBadRequest, "invalid_format", serviceError.Error())
	case errors.Is(serviceError, ErrFeatureOff):
		return response.Error(c, fiber.StatusForbidden, "feature_off", serviceError.Error())
	case errors.Is(serviceError, ErrCustomerNotFound), errors.Is(serviceError, ErrPaymentNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrInvalidPhone):
		return response.Error(c, fiber.StatusBadRequest, "invalid_phone", serviceError.Error())
	case errors.Is(serviceError, ErrPhoneTaken):
		return response.Error(c, fiber.StatusConflict, "phone_taken", serviceError.Error())
	case errors.Is(serviceError, ErrOpeningBalanceTooLow):
		return response.Error(c, fiber.StatusBadRequest, "opening_balance_too_low", serviceError.Error())
	case errors.Is(serviceError, ErrOverpayment):
		return response.Error(c, fiber.StatusBadRequest, "overpayment", serviceError.Error())
	case errors.Is(serviceError, ErrAlreadyVoided):
		return response.Error(c, fiber.StatusConflict, "already_voided", serviceError.Error())
	case errors.Is(serviceError, ErrInvalidRange):
		return response.Error(c, fiber.StatusBadRequest, "invalid_range", serviceError.Error())
	case errors.Is(serviceError, ErrCustomerRequired), errors.Is(serviceError, ErrCustomerInactive):
		return response.Error(c, fiber.StatusBadRequest, "customer_required", serviceError.Error())
	case errors.Is(serviceError, ErrCreditLimitExceeded):
		return response.Error(c, fiber.StatusBadRequest, "credit_limit_exceeded", serviceError.Error())
	case errors.Is(serviceError, ErrInvalidName), errors.Is(serviceError, ErrInvalidReason):
		return response.Error(c, fiber.StatusBadRequest, "validation_failed", serviceError.Error())
	default:
		return serviceError
	}
}

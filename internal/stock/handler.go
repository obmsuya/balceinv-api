package stock

import (
	"errors"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
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

func (handler *Handler) Levels(c *fiber.Ctx) error {
	pagination := httpx.ParsePagination(c)
	levelFilter := LevelFilter{
		SearchText: c.Query("q"),
		Status:     c.Query("status"),
	}

	levelPage, listError := handler.service.Levels(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), levelFilter, pagination.Limit, pagination.Offset)
	if listError != nil {
		return RespondWithServiceError(c, listError)
	}
	return response.Paged(c, "Stock levels", levelPage)
}

func (handler *Handler) Summary(c *fiber.Ctx) error {
	summary, summaryError := handler.service.Summary(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c))
	if summaryError != nil {
		return RespondWithServiceError(c, summaryError)
	}
	return response.Success(c, "Stock summary", summary)
}

func (handler *Handler) Movements(c *fiber.Ctx) error {
	pagination := httpx.ParsePagination(c)

	movementFilter, isValidFilter := parseMovementFilter(c)
	if !isValidFilter {
		return RespondWithServiceError(c, ErrInvalidFilter)
	}

	movementPage, listError := handler.service.Movements(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), movementFilter, pagination.Limit, pagination.Offset)
	if listError != nil {
		return RespondWithServiceError(c, listError)
	}
	return response.Paged(c, "Stock movements", movementPage)
}

func (handler *Handler) Adjust(c *fiber.Ctx) error {
	request := AdjustmentRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	movementView, adjustError := handler.service.Adjust(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if adjustError != nil {
		return RespondWithServiceError(c, adjustError)
	}
	return response.Created(c, "Stock updated", movementView)
}

func parseMovementFilter(c *fiber.Ctx) (MovementFilter, bool) {
	movementFilter := MovementFilter{
		Reason: c.Query("reason"),
	}

	rawProductId := c.Query("product_id")
	if rawProductId != "" {
		productId, parseError := uuid.Parse(rawProductId)
		if parseError != nil {
			return MovementFilter{}, false
		}
		movementFilter.ProductId = &productId
	}

	fromTime, isValidFrom := parseOptionalTime(c.Query("from"))
	toTime, isValidTo := parseOptionalTime(c.Query("to"))
	if !isValidFrom || !isValidTo {
		return MovementFilter{}, false
	}
	movementFilter.From = fromTime
	movementFilter.To = toTime

	return movementFilter, true
}

func parseOptionalTime(rawTime string) (*time.Time, bool) {
	if rawTime == "" {
		return nil, true
	}
	parsedTime, parseError := time.Parse(time.RFC3339, rawTime)
	if parseError != nil {
		return nil, false
	}
	utcTime := parsedTime.UTC()
	return &utcTime, true
}

func RespondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrProductNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrNoActiveShop):
		return response.Error(c, fiber.StatusBadRequest, "no_active_shop", serviceError.Error())
	case errors.Is(serviceError, ErrWrongDirection):
		return response.Error(c, fiber.StatusBadRequest, "wrong_direction", serviceError.Error())
	case errors.Is(serviceError, ErrInvalidFilter):
		return response.Error(c, fiber.StatusBadRequest, "invalid_filter", serviceError.Error())
	case errors.Is(serviceError, ErrInsufficientStock):
		return response.Error(c, fiber.StatusConflict, "insufficient_stock", serviceError.Error())
	default:
		return serviceError
	}
}

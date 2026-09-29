package features

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
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

func (handler *Handler) Get(c *fiber.Ctx) error {
	featuresView, getError := handler.service.Get(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c))
	if getError != nil {
		return getError
	}
	return response.Success(c, "Features", featuresView)
}

func (handler *Handler) Update(c *fiber.Ctx) error {
	request := UpdateFeaturesRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	featuresView, updateError := handler.service.Update(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if updateError != nil {
		isRuleError := errors.Is(updateError, ErrNeedsCustomers) || errors.Is(updateError, ErrNeedsSuppliers) || errors.Is(updateError, ErrNeedsVatNumber)
		if isRuleError {
			return response.Error(c, fiber.StatusBadRequest, "invalid_features", updateError.Error())
		}
		return updateError
	}
	return response.Success(c, "Features saved", featuresView)
}

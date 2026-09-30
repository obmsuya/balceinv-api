package reports

import (
	"errors"
	"strconv"

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

func rangeRequestFrom(c *fiber.Ctx) RangeRequest {
	return RangeRequest{
		FromDate: c.Query("from"),
		ToDate:   c.Query("to"),
		Shop:     c.Query("shop"),
	}
}

func (handler *Handler) Summary(c *fiber.Ctx) error {
	summary, summaryError := handler.service.Summary(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), rangeRequestFrom(c))
	if summaryError != nil {
		return respondWithServiceError(c, summaryError)
	}
	return response.Success(c, "Sales summary", summary)
}

func (handler *Handler) Daily(c *fiber.Ctx) error {
	dayViews, dailyError := handler.service.Daily(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), rangeRequestFrom(c))
	if dailyError != nil {
		return respondWithServiceError(c, dailyError)
	}
	return response.Success(c, "Sales per day", dayViews)
}

func (handler *Handler) Products(c *fiber.Ctx) error {
	requestedLimit, limitError := strconv.Atoi(c.Query("limit", "0"))
	if limitError != nil {
		return respondWithServiceError(c, ErrInvalidSort)
	}
	productViews, productsError := handler.service.Products(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), rangeRequestFrom(c), c.Query("sort"), requestedLimit)
	if productsError != nil {
		return respondWithServiceError(c, productsError)
	}
	return response.Success(c, "Products sold", productViews)
}

func (handler *Handler) Cashiers(c *fiber.Ctx) error {
	cashierViews, cashiersError := handler.service.Cashiers(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), rangeRequestFrom(c))
	if cashiersError != nil {
		return respondWithServiceError(c, cashiersError)
	}
	return response.Success(c, "Sales per cashier", cashierViews)
}

func (handler *Handler) Shops(c *fiber.Ctx) error {
	shopViews, shopsError := handler.service.Shops(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), rangeRequestFrom(c))
	if shopsError != nil {
		return respondWithServiceError(c, shopsError)
	}
	return response.Success(c, "Sales per shop", shopViews)
}

func (handler *Handler) Inventory(c *fiber.Ctx) error {
	inventoryView, inventoryError := handler.service.Inventory(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), c.Query("shop"))
	if inventoryError != nil {
		return respondWithServiceError(c, inventoryError)
	}
	return response.Success(c, "Inventory", inventoryView)
}

func (handler *Handler) Dashboard(c *fiber.Ctx) error {
	dashboardView, dashboardError := handler.service.Dashboard(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), c.Query("shop"))
	if dashboardError != nil {
		return respondWithServiceError(c, dashboardError)
	}
	return response.Success(c, "Dashboard", dashboardView)
}

func (handler *Handler) Export(c *fiber.Ctx) error {
	exportRequest := ExportRequest{
		Report:   c.Params("report"),
		Format:   c.Query("format"),
		Language: c.Query("lang"),
		Sort:     c.Query("sort"),
		Range:    rangeRequestFrom(c),
	}
	exportedFile, exportError := handler.service.Export(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), exportRequest)
	if exportError != nil {
		return respondWithServiceError(c, exportError)
	}
	return httpx.SendFile(c, exportedFile.Name, exportedFile.ContentType, exportedFile.Bytes)
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrNoActiveShop):
		return response.Error(c, fiber.StatusBadRequest, "no_active_shop", serviceError.Error())
	case errors.Is(serviceError, ErrShopNotAssigned):
		return response.Error(c, fiber.StatusForbidden, "shop_not_assigned", serviceError.Error())
	case errors.Is(serviceError, ErrShopNotFound), errors.Is(serviceError, ErrUnknownReport):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrInvalidRange), errors.Is(serviceError, ErrRangeTooLong), errors.Is(serviceError, ErrInvalidSort):
		return response.Error(c, fiber.StatusBadRequest, "invalid_filter", serviceError.Error())
	case errors.Is(serviceError, documents.ErrUnknownFormat):
		return response.Error(c, fiber.StatusBadRequest, "invalid_format", serviceError.Error())
	default:
		return serviceError
	}
}

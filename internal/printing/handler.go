package printing

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/sales"
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

type receiptRequest struct {
	SaleId     string `json:"sale_id" validate:"required,uuid"`
	OpenDrawer bool   `json:"open_drawer"`
}

type testRequest struct {
	Port string `json:"port" validate:"max=300"`
}

func (handler *Handler) Status(c *fiber.Ctx) error {
	statusView, statusError := handler.service.Status(c.UserContext(), httpx.CurrentPrincipal(c).CompanyId)
	if statusError != nil {
		return respondWithPrintError(c, statusError)
	}
	return response.Success(c, "Printer status", statusView)
}

func (handler *Handler) Devices(c *fiber.Ctx) error {
	detectedPrinters, listError := ListDevices()
	if listError != nil {
		return respondWithPrintError(c, listError)
	}
	return response.Success(c, "Devices detected", detectedPrinters)
}

func (handler *Handler) Test(c *fiber.Ctx) error {
	request := testRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	usedPort, printError := handler.service.TestPrint(c.UserContext(), httpx.CurrentPrincipal(c).CompanyId, request.Port)
	if printError != nil {
		return respondWithPrintError(c, printError)
	}
	return response.Success(c, "Test print sent", fiber.Map{"port": usedPort})
}

func (handler *Handler) Receipt(c *fiber.Ctx) error {
	request := receiptRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	saleId := uuid.MustParse(request.SaleId)
	printError := handler.service.PrintReceipt(c.UserContext(), httpx.CurrentPrincipal(c).CompanyId, saleId, request.OpenDrawer)
	if printError != nil {
		return respondWithPrintError(c, printError)
	}
	return response.Success(c, "Receipt printed", nil)
}

func respondWithPrintError(c *fiber.Ctx, printError error) error {
	switch {
	case errors.Is(printError, ErrPrinterOff):
		return response.Error(c, fiber.StatusConflict, "printer_off", printError.Error())
	case errors.Is(printError, ErrPrinterNotSet):
		return response.Error(c, fiber.StatusConflict, "printer_not_set", printError.Error())
	case errors.Is(printError, ErrPrinterNotFound):
		return response.Error(c, fiber.StatusBadGateway, "printer_unreachable", printError.Error())
	case errors.Is(printError, sales.ErrSaleNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", printError.Error())
	default:
		return printError
	}
}

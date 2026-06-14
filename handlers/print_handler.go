package handlers

import (
	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type PrintHandler struct {
	service         *services.PrintService
	settingsService *services.SettingsService
}

func NewPrintHandler(service *services.PrintService, settingsService *services.SettingsService) *PrintHandler {
	return &PrintHandler{service: service, settingsService: settingsService}
}

// PrintReceipt handles POST /api/print/receipt
// Body: { sale_id: number, open_drawer: bool }
// The handler respects the settings.print_receipt_automatically flag —
// if the frontend sends this request, it means the user explicitly asked to print.
func (h *PrintHandler) PrintReceipt(c *fiber.Ctx) error {
	var body struct {
		SaleID     uint `json:"sale_id"`
		OpenDrawer bool `json:"open_drawer"`
	}
	if err := c.BodyParser(&body); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if body.SaleID == 0 {
		return utils.Error(c, fiber.StatusBadRequest, "sale_id is required")
	}

	if err := h.service.PrintReceipt(body.SaleID, body.OpenDrawer); err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.Success(c, "Receipt printed", nil)
}

// PrinterStatus handles GET /api/print/status
// Returns whether a printer is configured and enabled so the frontend
// can decide whether to show the "Print Receipt" button.
func (h *PrintHandler) PrinterStatus(c *fiber.Ctx) error {
	settings, err := h.settingsService.GetOrCreate()
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.Success(c, "Printer status", fiber.Map{
		"enabled":      settings.PrinterEnabled,
		"port":         settings.PrinterPort,
		"paper_width":  settings.PrinterPaperWidth,
		"open_drawer":  settings.OpenCashDrawer,
		"auto_print":   settings.PrintReceiptAutomatically,
	})
}
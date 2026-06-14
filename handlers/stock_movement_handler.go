package handlers

import (
	"strconv"
	"time"

	"github.com/chrisostomemataba/balceinv-api/repository"
	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type StockMovementHandler struct {
	service *services.StockMovementService
}

func NewStockMovementHandler(service *services.StockMovementService) *StockMovementHandler {
	return &StockMovementHandler{service: service}
}

func (h *StockMovementHandler) GetAll(c *fiber.Ctx) error {
	filters := repository.MovementFilters{}

	if s := c.Query("startDate"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			filters.StartDate = t
		}
	}
	if e := c.Query("endDate"); e != "" {
		if t, err := time.Parse("2006-01-02", e); err == nil {
			filters.EndDate = t
		}
	}
	if p := c.Query("productId"); p != "" {
		if id, err := strconv.ParseUint(p, 10, 32); err == nil {
			filters.ProductID = uint(id)
		}
	}
	filters.Reason = c.Query("reason")
	filters.Search = c.Query("search")

	movements, err := h.service.GetAll(filters)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Stock movements fetched", movements)
}

func (h *StockMovementHandler) GetByID(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid movement ID")
	}

	movement, err := h.service.GetByID(uint(id))
	if err != nil {
		return utils.Error(c, fiber.StatusNotFound, err.Error())
	}
	return utils.Success(c, "Stock movement fetched", movement)
}

func (h *StockMovementHandler) GetByProduct(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid product ID")
	}

	movements, err := h.service.GetByProduct(uint(id))
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Product movements fetched", movements)
}

func (h *StockMovementHandler) GetByDateRange(c *fiber.Ctx) error {
	startStr := c.Query("startDate")
	endStr := c.Query("endDate")

	if startStr == "" || endStr == "" {
		return utils.Error(c, fiber.StatusBadRequest, "startDate and endDate are required")
	}

	start, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid startDate")
	}
	end, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid endDate")
	}

	movements, err := h.service.GetByDateRange(start, end)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Movements fetched for date range", movements)
}

func (h *StockMovementHandler) Create(c *fiber.Ctx) error {
	var input services.CreateMovementInput
	if err := c.BodyParser(&input); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	payload := c.Locals("user").(*utils.TokenPayload)

	movement, err := h.service.Create(input, payload.UserID)
	if err != nil {
		status := fiber.StatusBadRequest
		if err.Error() == "product not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(c, status, err.Error())
	}
	return utils.Success(c, "Stock movement created", movement)
}

func (h *StockMovementHandler) GetSummary(c *fiber.Ctx) error {
	summary, err := h.service.GetSummary()
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Movement summary fetched", summary)
}
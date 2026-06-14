package handlers

import (
	"strconv"
	"time"

	"github.com/chrisostomemataba/balceinv-api/repository"
	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type SaleHandler struct {
	service *services.SaleService
}

func NewSaleHandler(service *services.SaleService) *SaleHandler {
	return &SaleHandler{service: service}
}

func (h *SaleHandler) GetAll(c *fiber.Ctx) error {
	filters := repository.SaleFilters{}

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
	filters.PaymentType = c.Query("paymentType")
	filters.SaleType = c.Query("saleType")

	sales, err := h.service.GetAll(filters)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Sales fetched", sales)
}

func (h *SaleHandler) GetByID(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid sale ID")
	}

	sale, err := h.service.GetByID(uint(id))
	if err != nil {
		return utils.Error(c, fiber.StatusNotFound, err.Error())
	}
	return utils.Success(c, "Sale fetched", sale)
}

func (h *SaleHandler) Create(c *fiber.Ctx) error {
	var input services.CreateSaleInput
	if err := c.BodyParser(&input); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Attach the authenticated user's ID from the JWT payload
	payload := c.Locals("user").(*utils.TokenPayload)
	input.UserID = payload.UserID

	result, err := h.service.CreateSale(input)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return utils.Success(c, "Sale completed", result)
}

func (h *SaleHandler) GetDaily(c *fiber.Ctx) error {
	date := time.Now()
	if d := c.Query("date"); d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			date = t
		}
	}

	summary, err := h.service.GetDailySummary(date)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Daily sales summary", summary)
}

func (h *SaleHandler) GetMonthly(c *fiber.Ctx) error {
	now := time.Now()
	year := now.Year()
	month := int(now.Month())

	if y := c.Query("year"); y != "" {
		if v, err := strconv.Atoi(y); err == nil {
			year = v
		}
	}
	if m := c.Query("month"); m != "" {
		if v, err := strconv.Atoi(m); err == nil {
			month = v
		}
	}

	summary, err := h.service.GetMonthlySummary(year, month)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Monthly sales summary", summary)
}

func (h *SaleHandler) GetByDateRange(c *fiber.Ctx) error {
	startStr := c.Query("startDate")
	endStr := c.Query("endDate")

	if startStr == "" || endStr == "" {
		return utils.Error(c, fiber.StatusBadRequest, "startDate and endDate are required")
	}

	start, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid startDate format")
	}

	end, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid endDate format")
	}

	sales, err := h.service.GetByDateRange(start, end)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Sales fetched for date range", sales)
}
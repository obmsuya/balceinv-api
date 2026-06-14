package handlers

import (
	"strconv"
	"time"

	"github.com/chrisostomemataba/balceinv-api/repository"
	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type ReportHandler struct {
	service *services.ReportService
}

func NewReportHandler(service *services.ReportService) *ReportHandler {
	return &ReportHandler{service: service}
}

// parseDateRange reads startDate and endDate from query params.
// Both are optional — when absent the report covers all time.
func parseDateRange(c *fiber.Ctx) repository.ReportDateRange {
	dr := repository.ReportDateRange{}

	if s := c.Query("startDate"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			dr.StartDate = &t
		}
	}
	if e := c.Query("endDate"); e != "" {
		if t, err := time.Parse("2006-01-02", e); err == nil {
			dr.EndDate = &t
		}
	}

	return dr
}

func (h *ReportHandler) GetSalesSummary(c *fiber.Ctx) error {
	summary, err := h.service.GetSalesSummary(parseDateRange(c))
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Sales summary fetched", summary)
}

func (h *ReportHandler) GetTopProducts(c *fiber.Ctx) error {
	limit := 10
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}

	products, err := h.service.GetTopProducts(parseDateRange(c), limit)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Top products fetched", products)
}

func (h *ReportHandler) GetSalesByUser(c *fiber.Ctx) error {
	result, err := h.service.GetSalesByUser(parseDateRange(c))
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Sales by user fetched", result)
}

func (h *ReportHandler) GetInventory(c *fiber.Ctx) error {
	report, err := h.service.GetInventoryReport()
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Inventory report fetched", report)
}

func (h *ReportHandler) GetFinancial(c *fiber.Ctx) error {
	report, err := h.service.GetFinancialReport(parseDateRange(c))
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Financial report fetched", report)
}

func (h *ReportHandler) GetDailyTrend(c *fiber.Ctx) error {
	trend, err := h.service.GetDailyTrend(parseDateRange(c))
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Daily trend fetched", trend)
}
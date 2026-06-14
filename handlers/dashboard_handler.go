package handlers

import (
	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type DashboardHandler struct {
	db *gorm.DB
}

func NewDashboardHandler(db *gorm.DB) *DashboardHandler {
	return &DashboardHandler{db: db}
}

func (h *DashboardHandler) Get(c *fiber.Ctx) error {
	var userCount, productCount, saleCount int64
	var totalRevenue float64

	h.db.Model(&models.User{}).Count(&userCount)
	h.db.Model(&models.Product{}).Count(&productCount)
	h.db.Model(&models.Sale{}).Count(&saleCount)

	// GORM does not have a built-in Sum that returns a scalar,
	// so we use a raw select into a struct field.
	type revenueResult struct {
		Total float64
	}
	var rev revenueResult
	h.db.Model(&models.Sale{}).Select("COALESCE(SUM(total_amount), 0) as total").Scan(&rev)
	totalRevenue = rev.Total

	return utils.Success(c, "Dashboard summary", fiber.Map{
		"userCount":    userCount,
		"productCount": productCount,
		"saleCount":    saleCount,
		"totalRevenue": totalRevenue,
	})
}
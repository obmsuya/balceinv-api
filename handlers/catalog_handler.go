package handlers

import (
    "github.com/chrisostomemataba/balceinv-api/models"
    "github.com/chrisostomemataba/balceinv-api/utils"
    "github.com/gofiber/fiber/v2"
    "gorm.io/gorm"
)

type CatalogHandler struct {
    db *gorm.DB
}

func NewCatalogHandler(db *gorm.DB) *CatalogHandler {
    return &CatalogHandler{db: db}
}

func (h *CatalogHandler) GetAll(c *fiber.Ctx) error {
    var settings models.Settings
    if err := h.db.First(&settings).Error; err != nil {
        return utils.Error(c, fiber.StatusInternalServerError, "Could not load settings")
    }

    var company models.Company
    if err := h.db.First(&company, settings.CompanyID).Error; err != nil {
        return utils.Error(c, fiber.StatusInternalServerError, "Could not load company")
    }

    var items []models.CatalogProduct
    h.db.Where("business_type = ?", company.BusinessType).Find(&items)

    return utils.Success(c, "Catalog loaded", items)
}
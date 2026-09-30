package invoices

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/documents"
	"github.com/chrisostomemataba/balceinv-api/internal/sales"
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

func (handler *Handler) SaleDocument(c *fiber.Ctx) error {
	saleId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return response.Error(c, fiber.StatusNotFound, "not_found", sales.ErrSaleNotFound.Error())
	}

	documentRequest := DocumentRequest{
		Format:   c.Query("format"),
		Language: c.Query("lang"),
		Kind:     c.Query("kind"),
	}
	saleFile, documentError := handler.service.SaleDocument(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), saleId, documentRequest)
	if documentError != nil {
		switch {
		case errors.Is(documentError, sales.ErrSaleNotFound):
			return response.Error(c, fiber.StatusNotFound, "not_found", documentError.Error())
		case errors.Is(documentError, documents.ErrUnknownFormat), errors.Is(documentError, ErrUnknownKind):
			return response.Error(c, fiber.StatusBadRequest, "invalid_format", documentError.Error())
		default:
			return documentError
		}
	}

	return httpx.SendFile(c, saleFile.Name, saleFile.ContentType, saleFile.Bytes)
}

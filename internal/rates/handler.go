package rates

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/gofiber/fiber/v2"
)

var ErrMissingCompany = errors.New("company settings are missing")

type Handler struct {
	service            *Service
	settingsRepository *settings.Repository
}

func NewHandler(service *Service, settingsRepository *settings.Repository) *Handler {
	return &Handler{
		service:            service,
		settingsRepository: settingsRepository,
	}
}

func (handler *Handler) Latest(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)
	companyProfile, profileError := handler.settingsRepository.FindCompanyProfile(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId)
	if profileError != nil {
		return profileError
	}
	if companyProfile == nil {
		return ErrMissingCompany
	}

	ratesView, ratesError := handler.service.Latest(c.UserContext(), companyProfile.CurrencyCode)
	if ratesError != nil {
		return ratesError
	}
	return response.Success(c, "Exchange rates", ratesView)
}

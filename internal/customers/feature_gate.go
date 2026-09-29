package customers

import (
	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/features"
	"github.com/gofiber/fiber/v2"
)

func CustomersOn(companyFeatures features.Features) bool {
	return companyFeatures.CustomersEnabled
}

func OrdersOn(companyFeatures features.Features) bool {
	return companyFeatures.CustomersEnabled && companyFeatures.CustomerOrdersEnabled
}

func FeatureGate(featuresRepository *features.Repository, isOn func(features.Features) bool) func(fiber.Handler) fiber.Handler {
	return func(routeHandler fiber.Handler) fiber.Handler {
		return func(c *fiber.Ctx) error {
			companyFeatures, findError := featuresRepository.Find(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId)
			if findError != nil {
				return findError
			}
			if !isOn(companyFeatures) {
				return response.Error(c, fiber.StatusForbidden, "feature_off", ErrFeatureOff.Error())
			}
			return routeHandler(c)
		}
	}
}

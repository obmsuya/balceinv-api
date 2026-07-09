package middleware

import (
	"strings"

	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/gofiber/fiber/v2"
)

var licenseCheckSkippedPaths = []string{
	"/health",
	"/api/auth/login",
	"/api/auth/logout",
	"/api/auth/refresh",
	"/api/setup",
	"/api/setup/status",
	"/api/license/status",
	"/api/license/packages",
	"/api/license/pay",
	"/api/license/hardware-id",
}

// LicenseCheck returns a Fiber middleware handler that enforces license validity
// on all routes except those explicitly listed in licenseCheckSkippedPaths.
// Returns HTTP 402 with a JSON error body if the license check fails.
func LicenseCheck() fiber.Handler {
	return func(fiberContext *fiber.Ctx) error {
		requestPath := fiberContext.Path()

		pathIsSkipped := false
		for _, skippedPath := range licenseCheckSkippedPaths {
			pathMatchesSkipped := strings.HasPrefix(requestPath, skippedPath)
			if pathMatchesSkipped {
				pathIsSkipped = true
				break
			}
		}

		if pathIsSkipped {
			return fiberContext.Next()
		}

		licenseCheckError := license.Check()
		licenseIsValid := licenseCheckError == nil
		if licenseIsValid {
			return fiberContext.Next()
		}

		return fiberContext.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
			"success": false,
			"error":   "subscription_required",
			"message": licenseCheckError.Error(),
		})
	}
}

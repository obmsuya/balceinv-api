package httpx

import (
	"net/url"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/common/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const principalLocalKey = "principal"

func SetPrincipal(c *fiber.Ctx, principal *identity.Principal) {
	c.Locals(principalLocalKey, principal)
}

func CurrentPrincipal(c *fiber.Ctx) *identity.Principal {
	principal, isPrincipal := c.Locals(principalLocalKey).(*identity.Principal)
	if !isPrincipal {
		return nil
	}
	return principal
}

func RequirePermission(permissionId string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		principal := CurrentPrincipal(c)
		if principal == nil {
			return response.Error(c, fiber.StatusUnauthorized, "unauthenticated", "Please sign in")
		}

		isAllowed := principal.Can(permissionId)
		if !isAllowed {
			return response.Error(c, fiber.StatusForbidden, "forbidden", "You do not have permission to do this")
		}

		return c.Next()
	}
}

func BindAndValidate(c *fiber.Ctx, requestBody any) (bool, error) {
	parseError := c.BodyParser(requestBody)
	if parseError != nil {
		return false, response.Error(c, fiber.StatusBadRequest, "bad_request", "Request body is not valid JSON")
	}

	fieldErrors := validation.Validate(requestBody)
	hasFieldErrors := len(fieldErrors) > 0
	if hasFieldErrors {
		return false, response.ValidationError(c, fieldErrors)
	}

	return true, nil
}

func UuidParam(c *fiber.Ctx, parameterName string) (uuid.UUID, bool) {
	parsedId, parseError := uuid.Parse(c.Params(parameterName))
	if parseError != nil {
		return uuid.Nil, false
	}
	return parsedId, true
}

func OriginGuard(allowedOrigins []string) fiber.Handler {
	allowedOriginSet := map[string]bool{}
	for _, allowedOrigin := range allowedOrigins {
		allowedOriginSet[strings.TrimRight(allowedOrigin, "/")] = true
	}

	return func(c *fiber.Ctx) error {
		requestMethod := c.Method()
		isSafeMethod := requestMethod == fiber.MethodGet || requestMethod == fiber.MethodHead || requestMethod == fiber.MethodOptions
		if isSafeMethod {
			return c.Next()
		}

		requestOrigin := c.Get(fiber.HeaderOrigin)
		hasNoOrigin := requestOrigin == ""
		if hasNoOrigin {
			return c.Next()
		}

		isAllowedOrigin := allowedOriginSet[requestOrigin]
		isSameOrigin := originHost(requestOrigin) == c.Hostname()
		if isAllowedOrigin || isSameOrigin {
			return c.Next()
		}

		return response.Error(c, fiber.StatusForbidden, "origin_not_allowed", "This site is not allowed to make changes")
	}
}

func originHost(rawOrigin string) string {
	parsedOrigin, parseError := url.Parse(rawOrigin)
	if parseError != nil {
		return ""
	}
	return parsedOrigin.Host
}

package middleware

import (
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

func Protected(accessSecret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := c.Cookies("access_token")
		if token == "" {
			return utils.Error(c, fiber.StatusUnauthorized, "Not authenticated")
		}

		payload, err := utils.VerifyToken(token, accessSecret)
		if err != nil {
			return utils.Error(c, fiber.StatusUnauthorized, "Invalid or expired token")
		}

		c.Locals("user", payload)
		return c.Next()
	}
}
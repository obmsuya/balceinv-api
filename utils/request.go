package utils

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

func ExtractToken(c *fiber.Ctx, cookieName string) string {
	token := strings.TrimPrefix(c.Get("Authorization"), "Bearer ")
	if token == "" {
		token = c.Cookies(cookieName)
	}
	return token
}

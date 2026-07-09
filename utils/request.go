package utils

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// ExtractToken reads a bearer token from the Authorization header first,
// falling back to the named cookie. The Tauri desktop client never has
// cookies delivered cross-origin (SameSite=Lax blocks tauri://localhost /
// https://tauri.localhost requests to http://localhost:<port>), so it sends
// tokens via the Authorization header instead. The browser/dev client is
// same-origin and relies on the cookie. Every endpoint that reads a token
// must check both, or it silently breaks for one client or the other.
func ExtractToken(c *fiber.Ctx, cookieName string) string {
	token := strings.TrimPrefix(c.Get("Authorization"), "Bearer ")
	if token == "" {
		token = c.Cookies(cookieName)
	}
	return token
}

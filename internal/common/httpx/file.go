package httpx

import "github.com/gofiber/fiber/v2"

func SendFile(c *fiber.Ctx, fileName string, contentType string, fileBytes []byte) error {
	c.Set(fiber.HeaderContentType, contentType)
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="`+fileName+`"`)
	return c.Send(fileBytes)
}

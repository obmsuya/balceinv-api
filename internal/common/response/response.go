package response

import "github.com/gofiber/fiber/v2"

const RequestIdLocalKey = "requestId"

type Page[Item any] struct {
	Items  []Item `json:"items"`
	Total  int64  `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func Success(c *fiber.Ctx, message string, data any) error {
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"message": message,
		"data":    data,
	})
}

func Created(c *fiber.Ctx, message string, data any) error {
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"message": message,
		"data":    data,
	})
}

func Paged[Item any](c *fiber.Ctx, message string, page Page[Item]) error {
	hasNoItems := page.Items == nil
	if hasNoItems {
		page.Items = []Item{}
	}
	return Success(c, message, page)
}

func Error(c *fiber.Ctx, status int, code string, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"success":   false,
		"code":      code,
		"message":   message,
		"requestId": requestIdOf(c),
	})
}

func ValidationError(c *fiber.Ctx, fieldErrors []FieldError) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"success":   false,
		"code":      "validation_failed",
		"message":   "Some fields are invalid",
		"fields":    fieldErrors,
		"requestId": requestIdOf(c),
	})
}

func requestIdOf(c *fiber.Ctx) string {
	requestId, isString := c.Locals(RequestIdLocalKey).(string)
	if !isString {
		return ""
	}
	return requestId
}

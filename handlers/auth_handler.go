package handlers

import (
	"time"

	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type AuthHandler struct {
	service *services.AuthService
}

func NewAuthHandler(service *services.AuthService) *AuthHandler {
	return &AuthHandler{service: service}
}

func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var input services.LoginInput
	if err := c.BodyParser(&input); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	result, err := h.service.Login(input)
	if err != nil {
		return utils.Error(c, fiber.StatusUnauthorized, err.Error())
	}

	c.Cookie(&fiber.Cookie{
		Name:     "access_token",
		Value:    result.AccessToken,
		HTTPOnly: true,
		SameSite: "Lax",
		MaxAge:   14400, // 4 hours
	})

	c.Cookie(&fiber.Cookie{
		Name:     "refresh_token",
		Value:    result.RefreshToken,
		HTTPOnly: true,
		SameSite: "Lax",
		MaxAge:   604800, // 7 days
	})

	return utils.Success(c, "Logged in successfully", fiber.Map{"user": result.User})
}

func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	refreshToken := c.Cookies("refresh_token")
	h.service.Logout(refreshToken)

	c.Cookie(&fiber.Cookie{Name: "access_token", Value: "", Expires: time.Now().Add(-1 * time.Hour)})
	c.Cookie(&fiber.Cookie{Name: "refresh_token", Value: "", Expires: time.Now().Add(-1 * time.Hour)})

	return utils.Success(c, "Logged out successfully", nil)
}

func (h *AuthHandler) Refresh(c *fiber.Ctx) error {
	refreshToken := c.Cookies("refresh_token")
	if refreshToken == "" {
		return utils.Error(c, fiber.StatusUnauthorized, "No refresh token")
	}

	newAccess, newRefresh, err := h.service.Refresh(refreshToken)
	if err != nil {
		return utils.Error(c, fiber.StatusForbidden, err.Error())
	}

	c.Cookie(&fiber.Cookie{Name: "access_token", Value: newAccess, HTTPOnly: true, SameSite: "Lax", MaxAge: 14400})
	c.Cookie(&fiber.Cookie{Name: "refresh_token", Value: newRefresh, HTTPOnly: true, SameSite: "Lax", MaxAge: 604800})

	return utils.Success(c, "Session refreshed", nil)
}

func (h *AuthHandler) Me(c *fiber.Ctx) error {
	payload := c.Locals("user").(*utils.TokenPayload)
	userData, err := h.service.GetCurrentUser(payload.UserID)
	if err != nil {
		return utils.Error(c, fiber.StatusNotFound, err.Error())
	}
	return utils.Success(c, "Current user", userData)
}
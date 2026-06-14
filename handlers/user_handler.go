package handlers

import (
	"strconv"

	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type UserHandler struct {
	service *services.UserService
}

func NewUserHandler(service *services.UserService) *UserHandler {
	return &UserHandler{service: service}
}

func (h *UserHandler) GetAll(c *fiber.Ctx) error {
	users, err := h.service.GetAll()
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Users fetched", users)
}

func (h *UserHandler) GetByID(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid user ID")
	}

	user, err := h.service.GetByID(uint(id))
	if err != nil {
		return utils.Error(c, fiber.StatusNotFound, err.Error())
	}
	return utils.Success(c, "User fetched", user)
}

func (h *UserHandler) Create(c *fiber.Ctx) error {
	var input services.CreateUserInput
	if err := c.BodyParser(&input); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	user, err := h.service.Create(input)
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "user already exists" {
			status = fiber.StatusConflict
		}
		if err.Error() == "role not found" {
			status = fiber.StatusNotFound
		}
		if err.Error() == "name, email, password, and role are required" {
			status = fiber.StatusBadRequest
		}
		return utils.Error(c, status, err.Error())
	}
	return utils.Success(c, "User created successfully", user)
}

func (h *UserHandler) Update(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid user ID")
	}

	var input services.UpdateUserInput
	if err := c.BodyParser(&input); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	user, err := h.service.Update(uint(id), input)
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "user not found" || err.Error() == "role not found" {
			status = fiber.StatusNotFound
		}
		if err.Error() == "email already in use" {
			status = fiber.StatusConflict
		}
		return utils.Error(c, status, err.Error())
	}
	return utils.Success(c, "User updated successfully", user)
}

func (h *UserHandler) UpdatePassword(c *fiber.Ctx) error {
	var body struct {
		UserID      uint   `json:"userId"`
		NewPassword string `json:"newPassword"`
	}
	if err := c.BodyParser(&body); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.service.UpdatePassword(body.UserID, body.NewPassword); err != nil {
		status := fiber.StatusBadRequest
		if err.Error() == "user not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(c, status, err.Error())
	}
	return utils.Success(c, "Password updated successfully. User sessions have been cleared.", nil)
}

func (h *UserHandler) Delete(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid user ID")
	}

	if err := h.service.Delete(uint(id)); err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "user not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(c, status, err.Error())
	}
	return utils.Success(c, "User deleted successfully", nil)
}
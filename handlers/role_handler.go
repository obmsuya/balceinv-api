package handlers

import (
	"strconv"

	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type RoleHandler struct {
	service *services.RoleService
}

func NewRoleHandler(service *services.RoleService) *RoleHandler {
	return &RoleHandler{service: service}
}

func (h *RoleHandler) GetAll(c *fiber.Ctx) error {
	roles, err := h.service.GetAll()
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Roles fetched", roles)
}

func (h *RoleHandler) GetByID(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid role ID")
	}

	role, err := h.service.GetByID(uint(id))
	if err != nil {
		return utils.Error(c, fiber.StatusNotFound, err.Error())
	}
	return utils.Success(c, "Role fetched", role)
}

func (h *RoleHandler) Create(c *fiber.Ctx) error {
	var body struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&body); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	role, err := h.service.Create(body.Name)
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "role already exists" {
			status = fiber.StatusConflict
		}
		if err.Error() == "role name is required" {
			status = fiber.StatusBadRequest
		}
		return utils.Error(c, status, err.Error())
	}
	return utils.Success(c, "Role created", role)
}

func (h *RoleHandler) Update(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid role ID")
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&body); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	role, err := h.service.Update(uint(id), body.Name)
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "role not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(c, status, err.Error())
	}
	return utils.Success(c, "Role updated", role)
}

func (h *RoleHandler) Delete(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid role ID")
	}

	if err := h.service.Delete(uint(id)); err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "role not found" {
			status = fiber.StatusNotFound
		}
		if err.Error() == "cannot delete role with assigned users" {
			status = fiber.StatusBadRequest
		}
		return utils.Error(c, status, err.Error())
	}
	return utils.Success(c, "Role deleted", nil)
}

func (h *RoleHandler) AssignRole(c *fiber.Ctx) error {
	var body struct {
		UserID uint `json:"userId"`
		RoleID uint `json:"roleId"`
	}
	if err := c.BodyParser(&body); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.service.AssignRole(body.UserID, body.RoleID); err != nil {
		status := fiber.StatusNotFound
		return utils.Error(c, status, err.Error())
	}
	return utils.Success(c, "Role assigned to user", nil)
}
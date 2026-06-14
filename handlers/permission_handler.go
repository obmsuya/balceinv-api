package handlers

import (
	"strconv"

	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type PermissionHandler struct {
	service *services.PermissionService
}

func NewPermissionHandler(service *services.PermissionService) *PermissionHandler {
	return &PermissionHandler{service: service}
}

func (h *PermissionHandler) GetAll(c *fiber.Ctx) error {
	perms, err := h.service.GetAll()
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Permissions fetched", perms)
}

func (h *PermissionHandler) GetRolePermissions(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid role ID")
	}

	perms, err := h.service.GetRolePermissions(uint(id))
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Role permissions fetched", perms)
}

func (h *PermissionHandler) GetUserPermissions(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid user ID")
	}

	perms, err := h.service.GetUserPermissions(uint(id))
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "user not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(c, status, err.Error())
	}
	return utils.Success(c, "User permissions fetched", perms)
}

func (h *PermissionHandler) AssignToRole(c *fiber.Ctx) error {
	var input services.AssignRolePermissionsInput
	if err := c.BodyParser(&input); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.service.AssignToRole(input); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return utils.Success(c, "Permissions assigned to role", nil)
}

func (h *PermissionHandler) AssignToUser(c *fiber.Ctx) error {
	var input services.AssignUserPermissionsInput
	if err := c.BodyParser(&input); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.service.AssignToUser(input); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return utils.Success(c, "Permissions assigned to user", nil)
}
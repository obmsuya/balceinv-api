package access

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

func (handler *Handler) ListPermissions(c *fiber.Ctx) error {
	permissionViews, listError := handler.service.ListPermissions(c.UserContext(), httpx.RequestQuerier(c))
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Success(c, "Permissions", permissionViews)
}

func (handler *Handler) ListRoles(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)
	pagination := httpx.ParsePagination(c)

	rolePage, listError := handler.service.ListRoles(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Roles", rolePage)
}

func (handler *Handler) GetRole(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)
	roleId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrRoleNotFound)
	}

	roleView, getError := handler.service.GetRole(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId, roleId)
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	return response.Success(c, "Role", roleView)
}

func (handler *Handler) CreateRole(c *fiber.Ctx) error {
	request := CreateRoleRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	roleView, createError := handler.service.CreateRole(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if createError != nil {
		return respondWithServiceError(c, createError)
	}
	return response.Created(c, "Role created", roleView)
}

func (handler *Handler) RenameRole(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)
	roleId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrRoleNotFound)
	}

	request := UpdateRoleRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	roleView, renameError := handler.service.RenameRole(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId, roleId, request)
	if renameError != nil {
		return respondWithServiceError(c, renameError)
	}
	return response.Success(c, "Role updated", roleView)
}

func (handler *Handler) DeleteRole(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)
	roleId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrRoleNotFound)
	}

	deleteError := handler.service.DeleteRole(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId, roleId)
	if deleteError != nil {
		return respondWithServiceError(c, deleteError)
	}
	return response.Success(c, "Role deleted", nil)
}

func (handler *Handler) ListRolePermissions(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)
	roleId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrRoleNotFound)
	}

	permissionViews, listError := handler.service.ListRolePermissions(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId, roleId)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Success(c, "Role permissions", permissionViews)
}

func (handler *Handler) ListUserPermissions(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)
	userId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrUserNotFound)
	}

	isOwnPermissions := userId == principal.UserId
	canViewUsers := principal.Can("users:view")
	if !isOwnPermissions && !canViewUsers {
		return response.Error(c, fiber.StatusForbidden, "forbidden", "You do not have permission to do this")
	}

	permissionViews, listError := handler.service.ListUserPermissions(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId, userId)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Success(c, "User permissions", permissionViews)
}

func (handler *Handler) AssignRolePermissions(c *fiber.Ctx) error {
	request := AssignRolePermissionsRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	roleId := uuid.MustParse(request.RoleId)
	assignError := handler.service.AssignRolePermissions(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), roleId, request.PermissionIds)
	if assignError != nil {
		return respondWithServiceError(c, assignError)
	}
	return response.Success(c, "Role permissions updated", nil)
}

func (handler *Handler) AssignUserPermissions(c *fiber.Ctx) error {
	request := AssignUserPermissionsRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	userId := uuid.MustParse(request.UserId)
	assignError := handler.service.AssignUserPermissions(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), userId, request.PermissionIds)
	if assignError != nil {
		return respondWithServiceError(c, assignError)
	}
	return response.Success(c, "User permissions updated", nil)
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrRoleNotFound), errors.Is(serviceError, ErrUserNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrRoleNameTaken):
		return response.Error(c, fiber.StatusConflict, "role_name_taken", serviceError.Error())
	case errors.Is(serviceError, ErrOwnerRoleLocked):
		return response.Error(c, fiber.StatusConflict, "owner_role_locked", serviceError.Error())
	case errors.Is(serviceError, ErrRoleInUse):
		return response.Error(c, fiber.StatusConflict, "role_in_use", serviceError.Error())
	case errors.Is(serviceError, ErrUnknownPermission):
		return response.Error(c, fiber.StatusBadRequest, "unknown_permission", serviceError.Error())
	case errors.Is(serviceError, ErrCannotGrantUnheldPermission):
		return response.Error(c, fiber.StatusForbidden, "cannot_grant_unheld_permission", serviceError.Error())
	default:
		return serviceError
	}
}

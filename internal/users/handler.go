package users

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

func (handler *Handler) List(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)
	pagination := httpx.ParsePagination(c)

	userPage, listError := handler.service.List(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId, c.Query("q"), pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Users", userPage)
}

func (handler *Handler) Get(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)
	userId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrUserNotFound)
	}

	userView, getError := handler.service.Get(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId, userId)
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	return response.Success(c, "User", userView)
}

func (handler *Handler) Create(c *fiber.Ctx) error {
	request := CreateUserRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	userView, createError := handler.service.Create(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if createError != nil {
		return respondWithServiceError(c, createError)
	}
	return response.Created(c, "User created", userView)
}

func (handler *Handler) Update(c *fiber.Ctx) error {
	userId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrUserNotFound)
	}

	request := UpdateUserRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	userView, updateError := handler.service.Update(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), userId, request)
	if updateError != nil {
		return respondWithServiceError(c, updateError)
	}
	return response.Success(c, "User updated", userView)
}

func (handler *Handler) Deactivate(c *fiber.Ctx) error {
	userId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrUserNotFound)
	}

	deactivateError := handler.service.Deactivate(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), userId)
	if deactivateError != nil {
		return respondWithServiceError(c, deactivateError)
	}
	return response.Success(c, "User deactivated", nil)
}

func (handler *Handler) ChangePassword(c *fiber.Ctx) error {
	request := UpdatePasswordRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	userId := uuid.MustParse(request.UserId)
	changeError := handler.service.ChangePassword(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), userId, request.NewPassword)
	if changeError != nil {
		return respondWithServiceError(c, changeError)
	}
	return response.Success(c, "Password updated", nil)
}

func (handler *Handler) AssignRole(c *fiber.Ctx) error {
	request := AssignRoleRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	userId := uuid.MustParse(request.UserId)
	roleId := uuid.MustParse(request.RoleId)
	userView, assignError := handler.service.AssignRole(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), userId, roleId)
	if assignError != nil {
		return respondWithServiceError(c, assignError)
	}
	return response.Success(c, "Role assigned", userView)
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrUserNotFound), errors.Is(serviceError, ErrRoleNotFound), errors.Is(serviceError, ErrShopNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrEmailTaken):
		return response.Error(c, fiber.StatusConflict, "email_taken", serviceError.Error())
	case errors.Is(serviceError, ErrLastOwner):
		return response.Error(c, fiber.StatusConflict, "last_owner", serviceError.Error())
	case errors.Is(serviceError, ErrCannotDeactivateSelf):
		return response.Error(c, fiber.StatusConflict, "cannot_deactivate_self", serviceError.Error())
	case errors.Is(serviceError, ErrOnlyOwnerCanManageOwners), errors.Is(serviceError, ErrForbidden):
		return response.Error(c, fiber.StatusForbidden, "forbidden", serviceError.Error())
	case errors.Is(serviceError, ErrPasswordTooLong):
		return response.Error(c, fiber.StatusBadRequest, "password_too_long", serviceError.Error())
	default:
		return serviceError
	}
}

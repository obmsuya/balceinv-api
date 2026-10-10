package admin

import (
	"errors"
	"slices"
	"strconv"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/gofiber/fiber/v2"
)

const (
	SessionCookieName = "balce_admin"
	cookiePath        = "/api/admin"
	staffLocalKey     = "platform_staff"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func currentStaff(c *fiber.Ctx) Staff {
	staff, _ := c.Locals(staffLocalKey).(Staff)
	return staff
}

func (handler *Handler) RequireStaff(allowedRoles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		staff, findError := handler.service.StaffForToken(c.UserContext(), httpx.RequestQuerier(c), c.Cookies(SessionCookieName))
		if findError != nil {
			return findError
		}
		if staff == nil {
			return response.Error(c, fiber.StatusUnauthorized, "admin_sign_in_required", "Sign in to the admin panel")
		}
		if len(allowedRoles) > 0 && !slices.Contains(allowedRoles, staff.Role) {
			return response.Error(c, fiber.StatusForbidden, "admin_forbidden", "Your admin role cannot do this")
		}
		c.Locals(staffLocalKey, *staff)
		return c.Next()
	}
}

func setSessionCookie(c *fiber.Ctx, sessionToken string, maxAgeSeconds int) {
	c.Cookie(&fiber.Cookie{
		Name:     SessionCookieName,
		Value:    sessionToken,
		Path:     cookiePath,
		MaxAge:   maxAgeSeconds,
		HTTPOnly: true,
		Secure:   c.Protocol() == "https",
		SameSite: fiber.CookieSameSiteStrictMode,
	})
}

func (handler *Handler) SignIn(c *fiber.Ctx) error {
	request := SignInRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	sessionToken, staffView, signInError := handler.service.SignIn(c.UserContext(), httpx.RequestQuerier(c), request)
	if signInError != nil {
		return respondWithServiceError(c, signInError)
	}
	setSessionCookie(c, sessionToken, int(SessionLifetime.Seconds()))
	return response.Success(c, "Signed in", staffView)
}

func (handler *Handler) SignOut(c *fiber.Ctx) error {
	signOutError := handler.service.SignOut(c.UserContext(), httpx.RequestQuerier(c), c.Cookies(SessionCookieName))
	if signOutError != nil {
		return signOutError
	}
	setSessionCookie(c, "", -1)
	return response.Success(c, "Signed out", nil)
}

func (handler *Handler) Me(c *fiber.Ctx) error {
	return response.Success(c, "Admin", toStaffView(currentStaff(c)))
}

func (handler *Handler) Shops(c *fiber.Ctx) error {
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	shopPage, listError := handler.service.Shops(c.UserContext(), httpx.RequestQuerier(c), c.Query("search"), offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Success(c, "Shops", shopPage)
}

func (handler *Handler) Shop(c *fiber.Ctx) error {
	companyId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrShopNotFound)
	}
	shopDetail, findError := handler.service.Shop(c.UserContext(), httpx.RequestQuerier(c), companyId)
	if findError != nil {
		return respondWithServiceError(c, findError)
	}
	return response.Success(c, "Shop", shopDetail)
}

func (handler *Handler) CreateShop(c *fiber.Ctx) error {
	request := CreateShopRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	createdShop, createError := handler.service.CreateShop(c.UserContext(), httpx.RequestQuerier(c), currentStaff(c), request)
	if createError != nil {
		return respondWithServiceError(c, createError)
	}
	return response.Created(c, "Shop created", createdShop)
}

func (handler *Handler) ExtendTrial(c *fiber.Ctx) error {
	companyId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrShopNotFound)
	}
	request := ExtendTrialRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	extendedTrial, extendError := handler.service.ExtendTrial(c.UserContext(), httpx.RequestQuerier(c), currentStaff(c), companyId, request)
	if extendError != nil {
		return respondWithServiceError(c, extendError)
	}
	return response.Success(c, "Trial extended", extendedTrial)
}

func (handler *Handler) ResetPassword(c *fiber.Ctx) error {
	companyId, isValidCompany := httpx.UuidParam(c, "id")
	userId, isValidUser := httpx.UuidParam(c, "userId")
	if !isValidCompany || !isValidUser {
		return respondWithServiceError(c, ErrUserNotFound)
	}
	passwordReset, resetError := handler.service.ResetPassword(c.UserContext(), httpx.RequestQuerier(c), currentStaff(c), companyId, userId)
	if resetError != nil {
		return respondWithServiceError(c, resetError)
	}
	return response.Success(c, "Password reset", passwordReset)
}

func (handler *Handler) SupportMessages(c *fiber.Ctx) error {
	messages, listError := handler.service.SupportMessages(c.UserContext(), httpx.RequestQuerier(c), c.Query("show") != "all")
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Success(c, "Support messages", messages)
}

func (handler *Handler) MarkHandled(c *fiber.Ctx) error {
	messageId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrMessageNotFound)
	}
	markError := handler.service.MarkHandled(c.UserContext(), httpx.RequestQuerier(c), currentStaff(c), messageId)
	if markError != nil {
		return respondWithServiceError(c, markError)
	}
	return response.Success(c, "Marked handled", nil)
}

func (handler *Handler) Audit(c *fiber.Ctx) error {
	auditViews, listError := handler.service.Audit(c.UserContext(), httpx.RequestQuerier(c))
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Success(c, "Audit log", auditViews)
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrBadSignIn):
		return response.Error(c, fiber.StatusUnauthorized, "admin_bad_sign_in", serviceError.Error())
	case errors.Is(serviceError, ErrShopNotFound), errors.Is(serviceError, ErrUserNotFound), errors.Is(serviceError, ErrMessageNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrNotOnTrial):
		return response.Error(c, fiber.StatusConflict, "admin_not_on_trial", serviceError.Error())
	case errors.Is(serviceError, ErrEmailAlreadyUsed):
		return response.Error(c, fiber.StatusConflict, "email_taken", serviceError.Error())
	default:
		return serviceError
	}
}

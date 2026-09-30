package auth

import (
	"errors"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const (
	SessionCookieName   = "balce_session"
	desktopClientHeader = "X-Balce-Client"
	userAgentMaxLength  = 300
)

type Handler struct {
	service             *Service
	alwaysSecureCookies bool
}

func NewHandler(service *Service, alwaysSecureCookies bool) *Handler {
	return &Handler{
		service:             service,
		alwaysSecureCookies: alwaysSecureCookies,
	}
}

func (handler *Handler) Login(c *fiber.Ctx) error {
	request := LoginRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	userAgent := c.Get(fiber.HeaderUserAgent)
	isUserAgentTooLong := len(userAgent) > userAgentMaxLength
	if isUserAgentTooLong {
		userAgent = userAgent[:userAgentMaxLength]
	}

	loginOutcome, loginError := handler.service.Login(c.UserContext(), request.Email, request.Password, c.IP(), userAgent)
	if loginError != nil {
		if errors.Is(loginError, ErrInvalidCredentials) {
			return response.Error(c, fiber.StatusUnauthorized, "invalid_credentials", loginError.Error())
		}
		return loginError
	}

	handler.writeSessionCookie(c, loginOutcome.SessionToken, int(SessionLifetime.Seconds()))

	loginView := LoginView{
		User: loginOutcome.View,
	}
	isDesktopClient := c.Get(desktopClientHeader) == "desktop"
	if isDesktopClient {
		loginView.SessionToken = loginOutcome.SessionToken
	}

	return response.Success(c, "Signed in", loginView)
}

func (handler *Handler) Logout(c *fiber.Ctx) error {
	logoutError := handler.service.Logout(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c))
	if logoutError != nil {
		return logoutError
	}

	handler.writeSessionCookie(c, "", -1)
	return response.Success(c, "Signed out", nil)
}

func (handler *Handler) Me(c *fiber.Ctx) error {
	currentUserView, currentUserError := handler.service.CurrentUser(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c))
	if currentUserError != nil {
		if errors.Is(currentUserError, ErrSessionInvalid) {
			return response.Error(c, fiber.StatusUnauthorized, "unauthenticated", "Please sign in")
		}
		return currentUserError
	}
	return response.Success(c, "Current user", currentUserView)
}

func (handler *Handler) SwitchShop(c *fiber.Ctx) error {
	request := SwitchShopRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	shopId := uuid.MustParse(request.ShopId)
	currentUserView, switchError := handler.service.SwitchShop(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), shopId)
	if switchError != nil {
		if errors.Is(switchError, ErrShopNotWorkable) {
			return response.Error(c, fiber.StatusForbidden, "shop_not_assigned", switchError.Error())
		}
		return switchError
	}
	return response.Success(c, "Shop switched", currentUserView)
}

func (handler *Handler) SetLanguage(c *fiber.Ctx) error {
	request := LanguageRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	currentUserView, languageError := handler.service.SetLanguage(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request.Locale)
	if languageError != nil {
		return languageError
	}
	return response.Success(c, "Language saved", currentUserView)
}

func (handler *Handler) MarkTourSeen(c *fiber.Ctx) error {
	request := TourRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	currentUserView, markError := handler.service.MarkTourSeen(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request.Tour)
	if markError != nil {
		return markError
	}
	return response.Success(c, "Tour remembered", currentUserView)
}

func (handler *Handler) Authenticate() fiber.Handler {
	return func(c *fiber.Ctx) error {
		sessionToken := sessionTokenFromRequest(c)
		hasNoToken := sessionToken == ""
		if hasNoToken {
			return response.Error(c, fiber.StatusUnauthorized, "unauthenticated", "Please sign in")
		}

		principal, authenticateError := handler.service.Authenticate(c.UserContext(), sessionToken)
		if authenticateError != nil {
			if errors.Is(authenticateError, ErrSessionInvalid) {
				handler.writeSessionCookie(c, "", -1)
				return response.Error(c, fiber.StatusUnauthorized, "unauthenticated", "Your session has ended. Please sign in again")
			}
			return authenticateError
		}

		httpx.SetPrincipal(c, principal)
		return c.Next()
	}
}

func (handler *Handler) writeSessionCookie(c *fiber.Ctx, cookieValue string, maxAgeSeconds int) {
	isSecureRequest := handler.alwaysSecureCookies || c.Protocol() == "https"

	sessionCookie := &fiber.Cookie{
		Name:     SessionCookieName,
		Value:    cookieValue,
		Path:     "/",
		MaxAge:   maxAgeSeconds,
		HTTPOnly: true,
		Secure:   isSecureRequest,
		SameSite: fiber.CookieSameSiteLaxMode,
	}

	isRemoval := maxAgeSeconds < 0
	if isRemoval {
		sessionCookie.MaxAge = 0
		sessionCookie.Expires = time.Unix(0, 0)
	}

	c.Cookie(sessionCookie)
}

func sessionTokenFromRequest(c *fiber.Ctx) string {
	authorizationHeader := c.Get(fiber.HeaderAuthorization)
	bearerToken, hasBearerPrefix := strings.CutPrefix(authorizationHeader, "Bearer ")
	if hasBearerPrefix {
		return strings.TrimSpace(bearerToken)
	}
	return c.Cookies(SessionCookieName)
}

package admin

import (
	"errors"
	"net/http"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/gofiber/fiber/v2"
)

type hardwareIdRequest struct {
	HardwareId string `json:"hardware_id" validate:"required,max=128"`
}

func (handler *Handler) ConnectWapangaji(c *fiber.Ctx) error {
	request := WapangajiConnectRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	statusView, connectError := handler.service.ConnectWapangaji(c.UserContext(), httpx.RequestQuerier(c), currentStaff(c), c.Cookies(SessionCookieName), request)
	if connectError != nil {
		return respondWithLicenceError(c, connectError, true)
	}
	return response.Success(c, "Connected to Wapangaji", statusView)
}

func (handler *Handler) DisconnectWapangaji(c *fiber.Ctx) error {
	disconnectError := handler.service.DisconnectWapangaji(c.UserContext(), httpx.RequestQuerier(c), c.Cookies(SessionCookieName))
	if disconnectError != nil {
		return disconnectError
	}
	return response.Success(c, "Disconnected from Wapangaji", WapangajiStatusView{})
}

func (handler *Handler) WapangajiStatus(c *fiber.Ctx) error {
	statusView, statusError := handler.service.WapangajiStatus(c.UserContext(), httpx.RequestQuerier(c), c.Cookies(SessionCookieName))
	if statusError != nil {
		return statusError
	}
	return response.Success(c, "Wapangaji", statusView)
}

func (handler *Handler) LookupLicence(c *fiber.Ctx) error {
	request := LicenceLookupRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	lookupView, lookupError := handler.service.LookupLicence(c.UserContext(), httpx.RequestQuerier(c), c.Cookies(SessionCookieName), request)
	if lookupError != nil {
		return respondWithLicenceError(c, lookupError, false)
	}
	return response.Success(c, "Licence", lookupView)
}

func (handler *Handler) LicencePackages(c *fiber.Ctx) error {
	packages, packagesError := handler.service.LicencePackages(c.UserContext())
	if packagesError != nil {
		return respondWithLicenceError(c, packagesError, false)
	}
	return response.Success(c, "Plans", packages)
}

func (handler *Handler) LicencePayments(c *fiber.Ctx) error {
	request := hardwareIdRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	payments, historyError := handler.service.LicencePayments(c.UserContext(), httpx.RequestQuerier(c), c.Cookies(SessionCookieName), request.HardwareId)
	if historyError != nil {
		return respondWithLicenceError(c, historyError, false)
	}
	return response.Success(c, "Payments", payments)
}

func (handler *Handler) RecordPayment(c *fiber.Ctx) error {
	request := RecordPaymentRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	licenceView, recordError := handler.service.RecordPayment(c.UserContext(), httpx.RequestQuerier(c), currentStaff(c), c.Cookies(SessionCookieName), request)
	if recordError != nil {
		return respondWithLicenceError(c, recordError, false)
	}
	return response.Success(c, "Payment recorded", licenceView)
}

func (handler *Handler) LicenceDevices(c *fiber.Ctx) error {
	request := hardwareIdRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	devicesView, devicesError := handler.service.LicenceDevices(c.UserContext(), httpx.RequestQuerier(c), c.Cookies(SessionCookieName), request.HardwareId)
	if devicesError != nil {
		return respondWithLicenceError(c, devicesError, false)
	}
	return response.Success(c, "Devices", devicesView)
}

func (handler *Handler) RemoveDevice(c *fiber.Ctx) error {
	request := RemoveDeviceRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	devicesView, removeError := handler.service.RemoveDevice(c.UserContext(), httpx.RequestQuerier(c), currentStaff(c), c.Cookies(SessionCookieName), request)
	if removeError != nil {
		return respondWithLicenceError(c, removeError, false)
	}
	return response.Success(c, "Device removed", devicesView)
}

func respondWithLicenceError(c *fiber.Ctx, serviceError error, isSignIn bool) error {
	wapangajiError := &WapangajiError{}
	switch {
	case errors.Is(serviceError, ErrWapangajiNotConnected):
		return response.Error(c, fiber.StatusConflict, "wapangaji_not_connected", serviceError.Error())
	case errors.Is(serviceError, ErrWapangajiUnreachable):
		return response.Error(c, fiber.StatusBadGateway, "wapangaji_unreachable", serviceError.Error())
	case errors.Is(serviceError, ErrWapangajiUpdateRequired):
		return response.Error(c, fiber.StatusConflict, "wapangaji_update_required", serviceError.Error())
	case errors.As(serviceError, &wapangajiError) && isSignIn && (wapangajiError.StatusCode == http.StatusUnauthorized || wapangajiError.StatusCode == http.StatusBadRequest):
		return response.Error(c, fiber.StatusUnauthorized, "wapangaji_bad_sign_in", wapangajiError.Message)
	case errors.As(serviceError, &wapangajiError):
		return response.Error(c, fiber.StatusUnprocessableEntity, "wapangaji_refused", wapangajiError.Message)
	default:
		return serviceError
	}
}

package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/gofiber/fiber/v2"
)

const djangoPackagesURL = "https://backend.wapangaji.com/api/v1/payments/balce/packages/"
const djangoPayURL = "https://backend.wapangaji.com/api/v1/payments/balce/pay/"
const djangoProxyTimeoutSeconds = 20
const contentTypeHeader = "Content-Type"
const applicationJsonContentType = "application/json"

// GetHardwareId returns this device's computed hardware ID so it can be
// looked up (or manually registered) on the licensing server. Deliberately
// has no dependency on a local license already existing — this is exactly
// the value needed to create the first one.
func GetHardwareId(fiberContext *fiber.Ctx) error {
	hardwareIdString, hardwareIdComputeError := license.ComputeHardwareId()
	if hardwareIdComputeError != nil {
		return fiberContext.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   hardwareIdComputeError.Error(),
		})
	}
	return fiberContext.JSON(fiber.Map{
		"success":     true,
		"hardware_id": hardwareIdString,
	})
}

// GetLicenseStatus returns the current license state to the frontend including
// expiry date, days remaining, and whether the app is in the grace period.
func GetLicenseStatus(fiberContext *fiber.Ctx) error {
	licenseStateObject, licenseLoadError := license.LoadLicenseState()
	licenseFileExists := licenseLoadError == nil

	if !licenseFileExists {
		// No local license.json yet — this is the normal state before a
		// customer has paid, but it's also the state right after paying,
		// since nothing else ever writes the file for the first time.
		// The frontend polls this exact endpoint after initiating payment
		// (useLicense.ts pollUntilLicensed), so checking Django here closes
		// that loop with no frontend changes needed.
		if activateError := license.ActivateFromDjango(); activateError == nil {
			licenseStateObject, licenseLoadError = license.LoadLicenseState()
			licenseFileExists = licenseLoadError == nil
		}
	}

	if !licenseFileExists {
		return fiberContext.JSON(fiber.Map{"success": true, "licensed": false, "message": licenseLoadError.Error()})
	}

	daysRemainingInt := license.GetDaysRemaining()
	isInGracePeriodBool := license.IsInGracePeriod()
	licenseCheckError := license.Check()
	licenseIsValidBool := licenseCheckError == nil

	return fiberContext.JSON(fiber.Map{
		"success":         true,
		"licensed":        licenseIsValidBool,
		"expires_at":      licenseStateObject.ExpiresAt,
		"days_remaining":  daysRemainingInt,
		"is_grace_period": isInGracePeriodBool,
		"plan":            licenseStateObject.DaysGranted,
		"max_devices":     licenseStateObject.MaxDevices,
	})
}

// GetLicensePackages proxies the package list request to Django and returns
// the response directly to the frontend.
func GetLicensePackages(fiberContext *fiber.Ctx) error {
	httpClientObject := &http.Client{Timeout: time.Duration(djangoProxyTimeoutSeconds) * time.Second}

	djangoGetRequest, djangoGetRequestBuildError := http.NewRequest(http.MethodGet, djangoPackagesURL, nil)
	if djangoGetRequestBuildError != nil {
		return fiberContext.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": "failed to build request to licensing server"})
	}

	djangoHttpResponse, djangoHttpNetworkError := httpClientObject.Do(djangoGetRequest)
	djangoServerIsUnreachable := djangoHttpNetworkError != nil
	if djangoServerIsUnreachable {
		return fiberContext.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"success": false, "error": "licensing server is unreachable"})
	}
	defer djangoHttpResponse.Body.Close()

	djangoResponseBodyBytes, djangoResponseBodyReadError := io.ReadAll(djangoHttpResponse.Body)
	if djangoResponseBodyReadError != nil {
		return fiberContext.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": "failed to read licensing server response"})
	}

	fiberContext.Set(contentTypeHeader, applicationJsonContentType)
	fiberContext.Status(djangoHttpResponse.StatusCode)
	return fiberContext.Send(djangoResponseBodyBytes)
}

// InitiateLicensePayment receives the payment request from the frontend, injects
// the local hardware ID into the payload, and proxies the request to Django.
func InitiateLicensePayment(fiberContext *fiber.Ctx) error {
	frontendRequestPayloadMap := make(map[string]interface{})
	frontendPayloadUnmarshalError := json.Unmarshal(fiberContext.Body(), &frontendRequestPayloadMap)
	frontendPayloadIsInvalid := frontendPayloadUnmarshalError != nil
	if frontendPayloadIsInvalid {
		return fiberContext.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "invalid request body"})
	}

	hardwareIdString, hardwareIdComputeError := license.ComputeHardwareId()
	hardwareIdIsUnavailable := hardwareIdComputeError != nil
	if hardwareIdIsUnavailable {
		return fiberContext.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": "failed to read hardware identifier"})
	}

	frontendRequestPayloadMap["hardware_id"] = hardwareIdString

	updatedPayloadBytes, updatedPayloadMarshalError := json.Marshal(frontendRequestPayloadMap)
	if updatedPayloadMarshalError != nil {
		return fiberContext.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": "failed to build payment request"})
	}

	djangoPostRequest, djangoPostRequestBuildError := http.NewRequest(http.MethodPost, djangoPayURL, bytes.NewReader(updatedPayloadBytes))
	if djangoPostRequestBuildError != nil {
		return fiberContext.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": "failed to build request to licensing server"})
	}
	djangoPostRequest.Header.Set(contentTypeHeader, applicationJsonContentType)

	httpClientObject := &http.Client{Timeout: time.Duration(djangoProxyTimeoutSeconds) * time.Second}
	djangoHttpResponse, djangoHttpNetworkError := httpClientObject.Do(djangoPostRequest)
	djangoServerIsUnreachable := djangoHttpNetworkError != nil
	if djangoServerIsUnreachable {
		return fiberContext.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"success": false, "error": "licensing server is unreachable"})
	}
	defer djangoHttpResponse.Body.Close()

	djangoResponseBodyBytes, djangoResponseBodyReadError := io.ReadAll(djangoHttpResponse.Body)
	if djangoResponseBodyReadError != nil {
		return fiberContext.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": "failed to read licensing server response"})
	}

	fiberContext.Set(contentTypeHeader, applicationJsonContentType)
	fiberContext.Status(djangoHttpResponse.StatusCode)
	return fiberContext.Send(djangoResponseBodyBytes)
}

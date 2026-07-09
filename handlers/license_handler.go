package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

const djangoPackagesURL = "https://backend.wapangaji.com/api/v1/payments/balce/packages/"
const djangoPayURL = "https://backend.wapangaji.com/api/v1/payments/balce/pay/"
const djangoProxyTimeoutSeconds = 20
const contentTypeHeader = "Content-Type"
const applicationJsonContentType = "application/json"

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
func GetLicenseStatus(fiberContext *fiber.Ctx) error {
	licenseStateObject, licenseLoadError := license.LoadLicenseState()
	licenseIsCurrentlyValid := licenseLoadError == nil && license.Check() == nil

	if !licenseIsCurrentlyValid {
		if activateError := license.ActivateFromDjango(); activateError == nil {
			licenseStateObject, licenseLoadError = license.LoadLicenseState()
		}
	}

	licenseFileExists := licenseLoadError == nil
	if !licenseFileExists {
		return utils.Success(fiberContext, "License status", fiber.Map{
			"licensed":        false,
			"is_grace_period": false,
			"is_trial":        false,
		})
	}

	daysRemainingInt := license.GetDaysRemaining()
	isInGracePeriodBool := license.IsInGracePeriod()
	licenseCheckError := license.Check()
	licenseIsValidBool := licenseCheckError == nil

	return utils.Success(fiberContext, "License status", fiber.Map{
		"licensed":        licenseIsValidBool,
		"expires_at":      licenseStateObject.ExpiresAt,
		"days_remaining":  daysRemainingInt,
		"is_grace_period": isInGracePeriodBool,
		"is_trial":        licenseStateObject.IsTrial,
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

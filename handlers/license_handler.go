package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

const djangoProxyTimeoutSeconds = 20
const noInternetMessage = "No internet connection. Check the connection and try again."
const paymentServiceTroubleMessage = "The payment service is not responding. Try again in a few minutes."

var supportedMobileMoneyProviders = map[string]bool{"Mpesa": true, "Tigo": true, "Airtel": true, "Halopesa": true, "Azampesa": true}

const contentTypeHeader = "Content-Type"
const applicationJsonContentType = "application/json"

func djangoPackagesURL() string {
	return license.DjangoBaseURL + "/balce/packages/"
}

func djangoPayURL() string {
	return license.DjangoBaseURL + "/balce/pay/"
}

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

func GetLicenseStatus(fiberContext *fiber.Ctx) error {
	licenseStatus := license.CurrentStatus()
	shouldAskServerForLicense := !licenseStatus.Licensed && licenseStatus.LockReason != license.LockReasonClock
	if shouldAskServerForLicense {
		activationError := license.ActivateFromDjango()
		if activationError == nil {
			licenseStatus = license.CurrentStatus()
		}
	}
	return utils.Success(fiberContext, "License status", licenseStatus)
}

func RefreshLicense(fiberContext *fiber.Ctx) error {
	activationError := license.ActivateFromDjango()
	serverIsUnreachable := errors.Is(activationError, license.ErrLicensingServerUnreachable)
	if serverIsUnreachable {
		return fiberContext.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"success": false, "error": noInternetMessage})
	}
	return utils.Success(fiberContext, "License status", license.CurrentStatus())
}

func sendDjangoResponse(fiberContext *fiber.Ctx, djangoHttpResponse *http.Response) error {
	djangoResponseBodyBytes, djangoResponseBodyReadError := io.ReadAll(djangoHttpResponse.Body)
	if djangoResponseBodyReadError != nil {
		return fiberContext.Status(fiber.StatusBadGateway).JSON(fiber.Map{"success": false, "error": paymentServiceTroubleMessage})
	}

	djangoServerFailed := djangoHttpResponse.StatusCode >= http.StatusInternalServerError
	djangoBodyIsJson := json.Valid(djangoResponseBodyBytes)
	if djangoServerFailed || !djangoBodyIsJson {
		return fiberContext.Status(fiber.StatusBadGateway).JSON(fiber.Map{"success": false, "error": paymentServiceTroubleMessage})
	}

	fiberContext.Set(contentTypeHeader, applicationJsonContentType)
	fiberContext.Status(djangoHttpResponse.StatusCode)
	return fiberContext.Send(djangoResponseBodyBytes)
}

// GetLicensePackages proxies the package list request to Django and returns
// the response directly to the frontend.
func GetLicensePackages(fiberContext *fiber.Ctx) error {
	httpClientObject := &http.Client{Timeout: time.Duration(djangoProxyTimeoutSeconds) * time.Second}

	djangoGetRequest, djangoGetRequestBuildError := http.NewRequest(http.MethodGet, djangoPackagesURL(), nil)
	if djangoGetRequestBuildError != nil {
		return fiberContext.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": "failed to build request to licensing server"})
	}

	djangoHttpResponse, djangoHttpNetworkError := httpClientObject.Do(djangoGetRequest)
	djangoServerIsUnreachable := djangoHttpNetworkError != nil
	if djangoServerIsUnreachable {
		return fiberContext.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"success": false, "error": noInternetMessage})
	}
	defer djangoHttpResponse.Body.Close()

	return sendDjangoResponse(fiberContext, djangoHttpResponse)
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

	phoneNumber, _ := frontendRequestPayloadMap["phone"].(string)
	mobileMoneyProvider, _ := frontendRequestPayloadMap["provider"].(string)
	packageId, _ := frontendRequestPayloadMap["package_id"].(float64)
	if phoneNumber == "" {
		return fiberContext.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "Enter the phone number that will pay."})
	}
	if !supportedMobileMoneyProviders[mobileMoneyProvider] {
		return fiberContext.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "Choose the mobile money network."})
	}
	if packageId <= 0 {
		return fiberContext.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "Choose a plan first."})
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

	djangoPostRequest, djangoPostRequestBuildError := http.NewRequest(http.MethodPost, djangoPayURL(), bytes.NewReader(updatedPayloadBytes))
	if djangoPostRequestBuildError != nil {
		return fiberContext.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": "failed to build request to licensing server"})
	}
	djangoPostRequest.Header.Set(contentTypeHeader, applicationJsonContentType)

	httpClientObject := &http.Client{Timeout: time.Duration(djangoProxyTimeoutSeconds) * time.Second}
	djangoHttpResponse, djangoHttpNetworkError := httpClientObject.Do(djangoPostRequest)
	djangoServerIsUnreachable := djangoHttpNetworkError != nil
	if djangoServerIsUnreachable {
		return fiberContext.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"success": false, "error": noInternetMessage})
	}
	defer djangoHttpResponse.Body.Close()

	return sendDjangoResponse(fiberContext, djangoHttpResponse)
}

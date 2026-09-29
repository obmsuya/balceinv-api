package licensing

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/gofiber/fiber/v2"
)

const (
	licensingProxyTimeout        = 20 * time.Second
	noInternetMessage            = "No internet connection. Check the connection and try again."
	paymentServiceTroubleMessage = "The payment service is not responding. Try again in a few minutes."
	licensingResponseLimit       = 1 << 20
)

var supportedMobileMoneyProviders = map[string]bool{"Mpesa": true, "Tigo": true, "Airtel": true, "Halopesa": true, "Azampesa": true}

var skippedPathPrefixes = []string{
	"/api/auth/login",
	"/api/auth/logout",
	"/api/setup",
	"/api/platform",
	"/api/license/",
}

func Enforce() fiber.Handler {
	return func(c *fiber.Ctx) error {
		requestPath := c.Path()
		isApiPath := strings.HasPrefix(requestPath, "/api/")
		if !isApiPath {
			return c.Next()
		}
		for _, skippedPrefix := range skippedPathPrefixes {
			if strings.HasPrefix(requestPath, skippedPrefix) {
				return c.Next()
			}
		}

		checkError := license.Check()
		if checkError == nil {
			return c.Next()
		}
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
			"success": false,
			"error":   "subscription_required",
			"code":    "subscription_required",
			"message": checkError.Error(),
		})
	}
}

func IssueTrialAfterSetup() fiber.Handler {
	return func(c *fiber.Ctx) error {
		handlerError := c.Next()
		isSetupDone := handlerError == nil && c.Response().StatusCode() < fiber.StatusBadRequest
		if isSetupDone {
			trialError := license.IssueTrialLicense()
			if trialError != nil {
				return trialError
			}
		}
		return handlerError
	}
}

func HardwareId(c *fiber.Ctx) error {
	hardwareId, computeError := license.ComputeHardwareId()
	if computeError != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": computeError.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "hardware_id": hardwareId})
}

func Status(c *fiber.Ctx) error {
	licenseStatus := license.CurrentStatus()
	shouldAskServer := !licenseStatus.Licensed && licenseStatus.LockReason != license.LockReasonClock
	if shouldAskServer {
		activationError := license.ActivateFromDjango()
		if activationError == nil {
			licenseStatus = license.CurrentStatus()
		}
	}
	return response.Success(c, "License status", licenseStatus)
}

func Refresh(c *fiber.Ctx) error {
	activationError := license.ActivateFromDjango()
	if errors.Is(activationError, license.ErrLicensingServerUnreachable) {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"success": false, "error": noInternetMessage, "message": noInternetMessage})
	}
	return response.Success(c, "License status", license.CurrentStatus())
}

func Packages(c *fiber.Ctx) error {
	packagesRequest, requestError := http.NewRequestWithContext(c.UserContext(), http.MethodGet, license.DjangoBaseURL+"/balce/packages/", nil)
	if requestError != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": "failed to build request to licensing server"})
	}
	return forwardToLicensingServer(c, packagesRequest)
}

func Pay(c *fiber.Ctx) error {
	paymentPayload := map[string]any{}
	unmarshalError := json.Unmarshal(c.Body(), &paymentPayload)
	if unmarshalError != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "invalid request body"})
	}

	phoneNumber, _ := paymentPayload["phone"].(string)
	mobileMoneyProvider, _ := paymentPayload["provider"].(string)
	packageId, _ := paymentPayload["package_id"].(float64)
	if phoneNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "Enter the phone number that will pay."})
	}
	if !supportedMobileMoneyProviders[mobileMoneyProvider] {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "Choose the mobile money network."})
	}
	if packageId <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "error": "Choose a plan first."})
	}

	hardwareId, computeError := license.ComputeHardwareId()
	if computeError != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": "failed to read hardware identifier"})
	}
	paymentPayload["hardware_id"] = hardwareId

	payloadBytes, marshalError := json.Marshal(paymentPayload)
	if marshalError != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": "failed to build payment request"})
	}
	payRequest, requestError := http.NewRequestWithContext(c.UserContext(), http.MethodPost, license.DjangoBaseURL+"/balce/pay/", bytes.NewReader(payloadBytes))
	if requestError != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "error": "failed to build request to licensing server"})
	}
	payRequest.Header.Set("Content-Type", "application/json")
	return forwardToLicensingServer(c, payRequest)
}

func forwardToLicensingServer(c *fiber.Ctx, licensingRequest *http.Request) error {
	licensingClient := &http.Client{Timeout: licensingProxyTimeout}
	licensingResponse, sendError := licensingClient.Do(licensingRequest)
	if sendError != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"success": false, "error": noInternetMessage})
	}
	defer licensingResponse.Body.Close()

	responseBytes, readError := io.ReadAll(io.LimitReader(licensingResponse.Body, licensingResponseLimit))
	if readError != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"success": false, "error": paymentServiceTroubleMessage})
	}
	serverFailed := licensingResponse.StatusCode >= http.StatusInternalServerError
	if serverFailed || !json.Valid(responseBytes) {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"success": false, "error": paymentServiceTroubleMessage})
	}

	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	c.Status(licensingResponse.StatusCode)
	return c.Send(responseBytes)
}

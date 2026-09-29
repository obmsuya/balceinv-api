package httpx

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const requestQuerierLocalKey = "requestQuerier"

type Pagination struct {
	Limit  int
	Offset int
}

func RequestLogging(writeSeparator func()) fiber.Handler {
	return func(c *fiber.Ctx) error {
		requestId := newRequestId()
		c.Locals(response.RequestIdLocalKey, requestId)
		c.Set("X-Request-Id", requestId)

		startedAt := time.Now()
		slog.Info("request started",
			"requestId", requestId,
			"method", c.Method(),
			"path", c.Path(),
		)

		handlerError := c.Next()
		if handlerError != nil {
			errorHandlerError := c.App().ErrorHandler(c, handlerError)
			if errorHandlerError != nil {
				c.Status(fiber.StatusInternalServerError)
			}
		}

		requestDuration := time.Since(startedAt)
		slog.Info("request finished",
			"requestId", requestId,
			"status", c.Response().StatusCode(),
			"durationMs", requestDuration.Milliseconds(),
		)
		writeSeparator()

		return nil
	}
}

func RequestTransaction(openDatabase *database.Database) fiber.Handler {
	return func(c *fiber.Ctx) error {
		requestMethod := c.Method()
		isReadOnlyRequest := requestMethod == fiber.MethodGet || requestMethod == fiber.MethodHead

		connectionPool := openDatabase.Writer
		if isReadOnlyRequest {
			connectionPool = openDatabase.Reader
		}

		transactionOptions := &sql.TxOptions{
			ReadOnly: isReadOnlyRequest && openDatabase.IsPostgres(),
		}

		requestTransaction, beginError := connectionPool.BeginTx(c.UserContext(), transactionOptions)
		if beginError != nil {
			return fmt.Errorf("failed to begin request transaction: %w", beginError)
		}
		defer requestTransaction.Rollback()

		principal := CurrentPrincipal(c)
		isAuthenticated := principal != nil
		if isAuthenticated {
			setTenantError := database.SetTenant(c.UserContext(), requestTransaction, openDatabase.IsPostgres(), principal.CompanyId)
			if setTenantError != nil {
				return setTenantError
			}
		}

		var requestQuerier database.Querier = requestTransaction
		isCountingQueries := openDatabase.QueryCounter != nil
		if isCountingQueries {
			requestQuerier = &database.CountingQuerier{
				Inner:   requestTransaction,
				Counter: openDatabase.QueryCounter,
			}
		}
		c.Locals(requestQuerierLocalKey, requestQuerier)

		handlerError := c.Next()
		if handlerError != nil {
			return handlerError
		}

		responseStatus := c.Response().StatusCode()
		isFailedResponse := responseStatus >= fiber.StatusBadRequest
		if isFailedResponse {
			return nil
		}

		commitError := requestTransaction.Commit()
		if commitError != nil {
			return fmt.Errorf("failed to commit request transaction: %w", commitError)
		}

		return nil
	}
}

func RequestQuerier(c *fiber.Ctx) database.Querier {
	requestQuerier, isQuerier := c.Locals(requestQuerierLocalKey).(database.Querier)
	if !isQuerier {
		return nil
	}
	return requestQuerier
}

func ErrorHandler(c *fiber.Ctx, handlerError error) error {
	responseStatus := fiber.StatusInternalServerError
	errorCode := "internal_error"
	errorMessage := "Something went wrong. Share the request id with support."

	var fiberError *fiber.Error
	isFiberError := errors.As(handlerError, &fiberError)
	if isFiberError {
		responseStatus = fiberError.Code
		errorCode = codeForStatus(fiberError.Code)
		errorMessage = fiberError.Message
	}

	isServerFault := responseStatus >= fiber.StatusInternalServerError
	if isServerFault {
		requestId, _ := c.Locals(response.RequestIdLocalKey).(string)
		slog.Error("request failed",
			"requestId", requestId,
			"path", c.Path(),
			"error", handlerError,
		)
	}

	return response.Error(c, responseStatus, errorCode, errorMessage)
}

func ParsePagination(c *fiber.Ctx) Pagination {
	requestedLimit := parseIntOrDefault(c.Query("limit"), 20)
	isLimitTooSmall := requestedLimit < 1
	if isLimitTooSmall {
		requestedLimit = 20
	}
	isLimitTooLarge := requestedLimit > 100
	if isLimitTooLarge {
		requestedLimit = 100
	}

	requestedOffset := parseIntOrDefault(c.Query("offset"), 0)
	isOffsetNegative := requestedOffset < 0
	if isOffsetNegative {
		requestedOffset = 0
	}

	return Pagination{
		Limit:  requestedLimit,
		Offset: requestedOffset,
	}
}

func parseIntOrDefault(rawValue string, defaultValue int) int {
	parsedValue, parseError := strconv.Atoi(rawValue)
	if parseError != nil {
		return defaultValue
	}
	return parsedValue
}

func codeForStatus(status int) string {
	switch status {
	case fiber.StatusBadRequest:
		return "bad_request"
	case fiber.StatusUnauthorized:
		return "unauthenticated"
	case fiber.StatusForbidden:
		return "forbidden"
	case fiber.StatusNotFound:
		return "not_found"
	case fiber.StatusConflict:
		return "conflict"
	case fiber.StatusTooManyRequests:
		return "rate_limited"
	case fiber.StatusServiceUnavailable:
		return "unavailable"
	default:
		return "error"
	}
}

func newRequestId() string {
	requestUuid, generateError := uuid.NewV7()
	if generateError != nil {
		return uuid.NewString()
	}
	return requestUuid.String()
}

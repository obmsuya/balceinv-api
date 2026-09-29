package platform

import (
	"context"
	"errors"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/lan"
	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	openDatabase *database.Database
	network      *lan.Controller
}

type networkRequest struct {
	LanEnabled *bool `json:"lan_enabled" validate:"required"`
}

func NewHandler(openDatabase *database.Database, network *lan.Controller) *Handler {
	return &Handler{
		openDatabase: openDatabase,
		network:      network,
	}
}

func (handler *Handler) Health(c *fiber.Ctx) error {
	pingContext, cancelPing := context.WithTimeout(c.UserContext(), 2*time.Second)
	defer cancelPing()

	pingError := handler.openDatabase.Writer.PingContext(pingContext)
	if pingError != nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "database_unreachable", "Database is not reachable")
	}

	return response.Success(c, "ok", fiber.Map{
		"engine": handler.openDatabase.Engine,
	})
}

func (handler *Handler) Describe(c *fiber.Ctx) error {
	platformMode := "desktop"
	if handler.openDatabase.IsPostgres() {
		platformMode = "cloud"
	}

	if handler.network == nil {
		return response.Success(c, "Platform", fiber.Map{
			"mode":          platformMode,
			"lan_available": false,
			"lan_enabled":   false,
			"lan_urls":      []string{},
		})
	}

	networkStatus := handler.network.Status()
	return response.Success(c, "Platform", fiber.Map{
		"mode":           platformMode,
		"lan_available":  networkStatus.LanAvailable,
		"lan_enabled":    networkStatus.LanEnabled,
		"lan_urls":       networkStatus.LanUrls,
		"listen_address": networkStatus.ListenAddress,
	})
}

func (handler *Handler) SetNetwork(c *fiber.Ctx) error {
	if !httpx.CurrentPrincipal(c).IsOwner {
		return response.Error(c, fiber.StatusForbidden, "forbidden", "Only the owner can change the network setting")
	}
	request := networkRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	networkStatus, setError := handler.network.SetEnabled(*request.LanEnabled)
	if errors.Is(setError, lan.ErrLanFixedByConfig) {
		return response.Error(c, fiber.StatusConflict, "lan_fixed", setError.Error())
	}
	if setError != nil {
		return setError
	}
	return response.Success(c, "Network setting saved", networkStatus)
}

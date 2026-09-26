package handlers

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/backup"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type BackupHandler struct {
	database     *gorm.DB
	databasePath string
}

func NewBackupHandler(database *gorm.DB, databasePath string) *BackupHandler {
	return &BackupHandler{database: database, databasePath: databasePath}
}

func cloudBackupErrorStatus(cloudBackupError error) int {
	licenseIsMissing := errors.Is(cloudBackupError, backup.ErrNoPaidLicense)
	if licenseIsMissing {
		return fiber.StatusPaymentRequired
	}
	return fiber.StatusBadGateway
}

func (handler *BackupHandler) BackupNow(fiberContext *fiber.Ctx) error {
	backupKey, uploadError := backup.UploadCloudBackup(handler.database)
	if uploadError != nil {
		return utils.Error(fiberContext, cloudBackupErrorStatus(uploadError), uploadError.Error())
	}
	return utils.Success(fiberContext, "Backup uploaded", fiber.Map{"key": backupKey})
}

func (handler *BackupHandler) List(fiberContext *fiber.Ctx) error {
	cloudBackups, backupListError := backup.ListCloudBackups()
	if backupListError != nil {
		return utils.Error(fiberContext, cloudBackupErrorStatus(backupListError), backupListError.Error())
	}
	return utils.Success(fiberContext, "Cloud backups", cloudBackups)
}

func (handler *BackupHandler) Restore(fiberContext *fiber.Ctx) error {
	payload := fiberContext.Locals("user").(*utils.TokenPayload)
	userIsAdmin := payload.Role == "Admin"
	if !userIsAdmin {
		return utils.Error(fiberContext, fiber.StatusForbidden, "Only an admin can restore a backup")
	}

	var restoreRequest struct {
		Date string `json:"date"`
	}
	bodyParseError := fiberContext.BodyParser(&restoreRequest)
	if bodyParseError != nil || restoreRequest.Date == "" {
		return utils.Error(fiberContext, fiber.StatusBadRequest, "date is required")
	}

	restoreError := backup.StageCloudRestore(handler.databasePath, restoreRequest.Date)
	if restoreError != nil {
		return utils.Error(fiberContext, cloudBackupErrorStatus(restoreError), restoreError.Error())
	}
	return utils.Success(fiberContext, "Backup downloaded. Restart Balce to finish restoring.", fiber.Map{"restart_required": true})
}

package handlers

import (
	"errors"
	"log"

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
	cloudIsUnreachable := errors.Is(cloudBackupError, backup.ErrCloudUnreachable)
	if cloudIsUnreachable {
		return fiber.StatusServiceUnavailable
	}
	return fiber.StatusBadGateway
}

func respondWithCloudError(fiberContext *fiber.Ctx, cloudBackupError error) error {
	log.Printf("cloud backup request failed: %v", cloudBackupError)
	return utils.Error(fiberContext, cloudBackupErrorStatus(cloudBackupError), backup.UserFacingCloudError(cloudBackupError))
}

func requestIsFromAdmin(fiberContext *fiber.Ctx) bool {
	payload := fiberContext.Locals("user").(*utils.TokenPayload)
	return payload.Role == "Admin"
}

func (handler *BackupHandler) stageRestoreKeepingCurrentData(stageRestore func() error) error {
	saveError := backup.SaveBeforeRestoreCopy(handler.database)
	if saveError != nil {
		return saveError
	}
	return stageRestore()
}

func (handler *BackupHandler) BackupNow(fiberContext *fiber.Ctx) error {
	backupKey, uploadError := backup.UploadCloudBackup(handler.database)
	if uploadError != nil {
		return respondWithCloudError(fiberContext, uploadError)
	}
	return utils.Success(fiberContext, "Backup uploaded", fiber.Map{"key": backupKey})
}

func (handler *BackupHandler) List(fiberContext *fiber.Ctx) error {
	cloudBackups, backupListError := backup.ListCloudBackups()
	if backupListError != nil {
		return respondWithCloudError(fiberContext, backupListError)
	}
	return utils.Success(fiberContext, "Cloud backups", cloudBackups)
}

func (handler *BackupHandler) Restore(fiberContext *fiber.Ctx) error {
	if !requestIsFromAdmin(fiberContext) {
		return utils.Error(fiberContext, fiber.StatusForbidden, "Only an admin can restore a backup")
	}

	var restoreRequest struct {
		Date string `json:"date"`
	}
	bodyParseError := fiberContext.BodyParser(&restoreRequest)
	if bodyParseError != nil || restoreRequest.Date == "" {
		return utils.Error(fiberContext, fiber.StatusBadRequest, "date is required")
	}

	restoreError := handler.stageRestoreKeepingCurrentData(func() error {
		return backup.StageCloudRestore(handler.databasePath, restoreRequest.Date)
	})
	if restoreError != nil {
		return respondWithCloudError(fiberContext, restoreError)
	}
	return utils.Success(fiberContext, "Backup downloaded. Restart the POS to finish restoring.", fiber.Map{"restart_required": true})
}

func (handler *BackupHandler) Status(fiberContext *fiber.Ctx) error {
	backupStatus, statusError := backup.GetBackupStatus(handler.databasePath)
	if statusError != nil {
		return utils.Error(fiberContext, fiber.StatusInternalServerError, statusError.Error())
	}
	return utils.Success(fiberContext, "Backup status", backupStatus)
}

func (handler *BackupHandler) BackupOnThisPC(fiberContext *fiber.Ctx) error {
	localBackup, localBackupError := backup.WriteLocalBackup(handler.database)
	if localBackupError != nil {
		return utils.Error(fiberContext, fiber.StatusInternalServerError, localBackupError.Error())
	}
	return utils.Success(fiberContext, "Backup saved on this PC", localBackup)
}

func (handler *BackupHandler) RestoreFromThisPC(fiberContext *fiber.Ctx) error {
	if !requestIsFromAdmin(fiberContext) {
		return utils.Error(fiberContext, fiber.StatusForbidden, "Only an admin can restore a backup")
	}

	var restoreRequest struct {
		Date string `json:"date"`
	}
	bodyParseError := fiberContext.BodyParser(&restoreRequest)
	if bodyParseError != nil || restoreRequest.Date == "" {
		return utils.Error(fiberContext, fiber.StatusBadRequest, "date is required")
	}

	stageLocalRestore := func() error {
		return backup.StageLocalRestore(handler.databasePath, restoreRequest.Date)
	}
	var restoreError error
	restoringBeforeRestoreCopy := restoreRequest.Date == backup.BeforeRestoreBackupName
	if restoringBeforeRestoreCopy {
		restoreError = stageLocalRestore()
	} else {
		restoreError = handler.stageRestoreKeepingCurrentData(stageLocalRestore)
	}
	if restoreError != nil {
		return utils.Error(fiberContext, fiber.StatusBadRequest, restoreError.Error())
	}
	return utils.Success(fiberContext, "Backup ready. Restart the POS to finish restoring.", fiber.Map{"restart_required": true})
}

func (handler *BackupHandler) ExportToFile(fiberContext *fiber.Ctx) error {
	if !requestIsFromAdmin(fiberContext) {
		return utils.Error(fiberContext, fiber.StatusForbidden, "Only an admin can save a backup file")
	}

	var exportRequest struct {
		Path string `json:"path"`
	}
	bodyParseError := fiberContext.BodyParser(&exportRequest)
	if bodyParseError != nil || exportRequest.Path == "" {
		return utils.Error(fiberContext, fiber.StatusBadRequest, "path is required")
	}

	exportError := backup.ExportBackup(handler.database, exportRequest.Path)
	if exportError != nil {
		return utils.Error(fiberContext, fiber.StatusBadRequest, exportError.Error())
	}
	return utils.Success(fiberContext, "Backup file saved", fiber.Map{"path": exportRequest.Path})
}

func (handler *BackupHandler) RestoreFromFile(fiberContext *fiber.Ctx) error {
	if !requestIsFromAdmin(fiberContext) {
		return utils.Error(fiberContext, fiber.StatusForbidden, "Only an admin can restore a backup")
	}

	var restoreRequest struct {
		Path string `json:"path"`
	}
	bodyParseError := fiberContext.BodyParser(&restoreRequest)
	if bodyParseError != nil || restoreRequest.Path == "" {
		return utils.Error(fiberContext, fiber.StatusBadRequest, "path is required")
	}

	restoreError := handler.stageRestoreKeepingCurrentData(func() error {
		return backup.StageFileRestore(handler.databasePath, restoreRequest.Path)
	})
	if restoreError != nil {
		return utils.Error(fiberContext, fiber.StatusBadRequest, restoreError.Error())
	}
	return utils.Success(fiberContext, "Backup ready. Restart the POS to finish restoring.", fiber.Map{"restart_required": true})
}

package backup

import (
	"errors"
	"log/slog"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/gofiber/fiber/v2"
)

var ErrOwnerOnly = errors.New("only the owner can restore or save backup files")

type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler {
	return &Handler{
		store: store,
	}
}

type restoreRequest struct {
	Date string `json:"date" validate:"required,max=40"`
}

type filePathRequest struct {
	Path string `json:"path" validate:"required,max=1000"`
}

func (handler *Handler) Status(c *fiber.Ctx) error {
	backupStatus, statusError := handler.store.Status()
	if statusError != nil {
		return respondWithBackupError(c, statusError)
	}
	return response.Success(c, "Backup status", backupStatus)
}

func (handler *Handler) BackupOnThisComputer(c *fiber.Ctx) error {
	localBackup, backupError := handler.store.WriteLocalBackup(c.UserContext())
	if backupError != nil {
		return respondWithBackupError(c, backupError)
	}
	return response.Success(c, "Backup saved on this computer", localBackup)
}

func (handler *Handler) RestoreFromThisComputer(c *fiber.Ctx) error {
	if !httpx.CurrentPrincipal(c).IsOwner {
		return respondWithBackupError(c, ErrOwnerOnly)
	}
	request := restoreRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	stageError := handler.store.StageLocalRestore(c.UserContext(), request.Date)
	if stageError != nil {
		return respondWithBackupError(c, stageError)
	}
	return response.Success(c, "Backup ready. Restart Balce to finish restoring.", fiber.Map{"restart_required": true})
}

func (handler *Handler) ExportToFile(c *fiber.Ctx) error {
	if !httpx.CurrentPrincipal(c).IsOwner {
		return respondWithBackupError(c, ErrOwnerOnly)
	}
	request := filePathRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	exportError := handler.store.ExportBackup(c.UserContext(), request.Path)
	if exportError != nil {
		return respondWithBackupError(c, exportError)
	}
	return response.Success(c, "Backup file saved", fiber.Map{"path": request.Path})
}

func (handler *Handler) RestoreFromFile(c *fiber.Ctx) error {
	if !httpx.CurrentPrincipal(c).IsOwner {
		return respondWithBackupError(c, ErrOwnerOnly)
	}
	request := filePathRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	stageError := handler.store.StageFileRestore(c.UserContext(), request.Path)
	if stageError != nil {
		return respondWithBackupError(c, stageError)
	}
	return response.Success(c, "Backup ready. Restart Balce to finish restoring.", fiber.Map{"restart_required": true})
}

func (handler *Handler) ListCloud(c *fiber.Ctx) error {
	cloudBackups, listError := handler.store.ListCloudBackups(c.UserContext())
	if listError != nil {
		return respondWithBackupError(c, listError)
	}
	return response.Success(c, "Cloud backups", cloudBackups)
}

func (handler *Handler) BackupToCloud(c *fiber.Ctx) error {
	backupKey, uploadError := handler.store.UploadCloudBackup(c.UserContext())
	if uploadError != nil {
		return respondWithBackupError(c, uploadError)
	}
	return response.Success(c, "Backup uploaded", fiber.Map{"key": backupKey})
}

func (handler *Handler) RestoreFromCloud(c *fiber.Ctx) error {
	if !httpx.CurrentPrincipal(c).IsOwner {
		return respondWithBackupError(c, ErrOwnerOnly)
	}
	request := restoreRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	stageError := handler.store.StageCloudRestore(c.UserContext(), request.Date)
	if stageError != nil {
		return respondWithBackupError(c, stageError)
	}
	return response.Success(c, "Backup downloaded. Restart Balce to finish restoring.", fiber.Map{"restart_required": true})
}

func respondWithBackupError(c *fiber.Ctx, backupError error) error {
	switch {
	case errors.Is(backupError, ErrOwnerOnly):
		return response.Error(c, fiber.StatusForbidden, "forbidden", backupError.Error())
	case errors.Is(backupError, ErrNoPaidLicense):
		return response.Error(c, fiber.StatusPaymentRequired, "license_required", backupError.Error())
	case errors.Is(backupError, ErrCloudUnreachable):
		return response.Error(c, fiber.StatusServiceUnavailable, "cloud_unreachable", ErrCloudUnreachable.Error())
	case errors.Is(backupError, ErrInvalidBackupName), errors.Is(backupError, ErrBackupNotFound), errors.Is(backupError, ErrInvalidBackupPath),
		errors.Is(backupError, ErrNotABackup), errors.Is(backupError, ErrOldFormatBackup), errors.Is(backupError, ErrNewerBackup),
		errors.Is(backupError, ErrDamagedBackup), errors.Is(backupError, ErrUnfinishedBackup), errors.Is(backupError, ErrNoCloudBackup):
		return response.Error(c, fiber.StatusBadRequest, "invalid_backup", backupError.Error())
	default:
		slog.Error("backup request failed", "error", backupError)
		return response.Error(c, fiber.StatusInternalServerError, "backup_failed", "The backup could not be completed: "+backupError.Error())
	}
}

package cleardata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/security"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
)

type Service struct {
	openDatabase *database.Database
	repository   *Repository
	objectStore  storage.Store
}

func NewService(openDatabase *database.Database, repository *Repository, objectStore storage.Store) *Service {
	return &Service{
		openDatabase: openDatabase,
		repository:   repository,
		objectStore:  objectStore,
	}
}

func (service *Service) Clear(ctx context.Context, principal *identity.Principal, request ClearRequest) (ClearSummary, error) {
	if !principal.IsOwner {
		return ClearSummary{}, ErrOwnerOnly
	}

	clearTransaction, beginError := service.openDatabase.Writer.BeginTx(ctx, nil)
	if beginError != nil {
		return ClearSummary{}, fmt.Errorf("failed to begin clearing: %w", beginError)
	}
	defer clearTransaction.Rollback()

	isPostgres := service.openDatabase.IsPostgres()
	setTenantError := database.SetTenant(ctx, clearTransaction, isPostgres, principal.CompanyId)
	if setTenantError != nil {
		return ClearSummary{}, setTenantError
	}

	foundCheck, checkError := service.repository.FindOwnerCheck(ctx, clearTransaction, principal.CompanyId, principal.UserId)
	if checkError != nil {
		return ClearSummary{}, checkError
	}
	if !sameBusinessName(foundCheck.BusinessName, request.BusinessName) {
		return ClearSummary{}, ErrNameMismatch
	}
	if !security.PasswordMatches(foundCheck.PasswordHash, request.Password) {
		return ClearSummary{}, ErrWrongPassword
	}

	keptShopId, keepError := service.repository.ShopToKeep(ctx, clearTransaction, principal.CompanyId, principal.ShopId)
	if keepError != nil {
		return ClearSummary{}, keepError
	}

	fileKeys, keysError := service.repository.StoredFileKeys(ctx, clearTransaction, principal.CompanyId, principal.UserId)
	if keysError != nil {
		return ClearSummary{}, keysError
	}

	allowError := service.repository.AllowJournalDelete(ctx, clearTransaction, isPostgres, principal.CompanyId)
	if allowError != nil {
		return ClearSummary{}, allowError
	}

	removedByTable, deleteError := service.repository.DeleteBusinessData(ctx, clearTransaction, principal.CompanyId)
	if deleteError != nil {
		return ClearSummary{}, deleteError
	}

	shopsRemoved, shopsError := service.repository.RemoveOtherShops(ctx, clearTransaction, principal.CompanyId, keptShopId)
	if shopsError != nil {
		return ClearSummary{}, shopsError
	}

	staffRemoved, staffError := service.repository.RemoveStaff(ctx, clearTransaction, principal.CompanyId, principal.UserId)
	if staffError != nil {
		return ClearSummary{}, staffError
	}

	restartError := service.repository.RestartKeptShop(ctx, clearTransaction, principal.CompanyId, keptShopId)
	if restartError != nil {
		return ClearSummary{}, restartError
	}

	endError := service.repository.EndJournalDelete(ctx, clearTransaction, isPostgres, principal.CompanyId)
	if endError != nil {
		return ClearSummary{}, endError
	}

	commitError := clearTransaction.Commit()
	if commitError != nil {
		return ClearSummary{}, fmt.Errorf("failed to finish clearing: %w", commitError)
	}

	filesRemoved := service.removeStoredFiles(ctx, fileKeys)
	slog.Info("business data cleared",
		"companyId", principal.CompanyId,
		"keptShopId", keptShopId,
		"shopsRemoved", shopsRemoved,
		"staffRemoved", staffRemoved,
		"filesRemoved", filesRemoved,
	)

	clearSummary := ClearSummary{
		Products:     removedByTable["products"],
		Sales:        removedByTable["sales"],
		Customers:    removedByTable["customers"],
		Suppliers:    removedByTable["suppliers"],
		ShopsRemoved: shopsRemoved,
		StaffRemoved: staffRemoved,
		FilesRemoved: filesRemoved,
		KeptShopId:   keptShopId.String(),
	}
	return clearSummary, nil
}

func (service *Service) removeStoredFiles(ctx context.Context, fileKeys []string) int {
	filesRemoved := 0
	for _, fileKey := range fileKeys {
		removeError := service.objectStore.Delete(ctx, fileKey)
		if removeError != nil && !errors.Is(removeError, storage.ErrObjectNotFound) {
			slog.Warn("a cleared file could not be removed", "key", fileKey, "error", removeError)
			continue
		}
		filesRemoved++
	}
	return filesRemoved
}

func sameBusinessName(storedName string, typedName string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(storedName), " "), strings.Join(strings.Fields(typedName), " "))
}

package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/google/uuid"
)

type Service struct {
	repository   *Repository
	openDatabase *database.Database
}

func NewService(repository *Repository, openDatabase *database.Database) *Service {
	return &Service{repository: repository, openDatabase: openDatabase}
}

func (service *Service) startMissingTrial(ctx context.Context, companyId uuid.UUID) (*Subscription, error) {
	writeTransaction, beginError := service.openDatabase.Writer.BeginTx(ctx, nil)
	if beginError != nil {
		return nil, fmt.Errorf("failed to begin trial transaction: %w", beginError)
	}
	defer writeTransaction.Rollback()

	setTenantError := database.SetTenant(ctx, writeTransaction, service.openDatabase.IsPostgres(), companyId)
	if setTenantError != nil {
		return nil, setTenantError
	}

	startedAt := time.Now().UTC()
	insertError := service.repository.InsertTrialIfMissing(ctx, writeTransaction, companyId, startedAt.AddDate(0, 0, license.TrialDurationDays), startedAt)
	if insertError != nil {
		return nil, insertError
	}

	startedSubscription, findError := service.repository.Find(ctx, writeTransaction, companyId)
	if findError != nil {
		return nil, findError
	}

	commitError := writeTransaction.Commit()
	if commitError != nil {
		return nil, fmt.Errorf("failed to commit trial: %w", commitError)
	}
	return startedSubscription, nil
}

func (service *Service) findOrStartTrial(ctx context.Context, querier database.Querier, companyId uuid.UUID) (*Subscription, error) {
	foundSubscription, findError := service.repository.Find(ctx, querier, companyId)
	if findError != nil {
		return nil, findError
	}
	if foundSubscription != nil {
		return foundSubscription, nil
	}
	return service.startMissingTrial(ctx, companyId)
}

func DeviceIdFor(companyId uuid.UUID) string {
	return "cloud-" + companyId.String()
}

func statusOf(foundSubscription *Subscription, currentTime time.Time) license.Status {
	if foundSubscription == nil {
		return license.StatusAt(nil, currentTime)
	}
	subscriptionState := &license.LicenseState{
		LicenseKey:  foundSubscription.LicenseKey,
		ExpiresAt:   foundSubscription.ExpiresAt.UTC().Format(time.RFC3339),
		MaxDevices:  foundSubscription.MaxDevices,
		DaysGranted: foundSubscription.DaysGranted,
		IsTrial:     foundSubscription.IsTrial,
	}
	return license.StatusAt(subscriptionState, currentTime)
}

func (service *Service) Status(ctx context.Context, querier database.Querier, companyId uuid.UUID) (license.Status, error) {
	foundSubscription, findError := service.findOrStartTrial(ctx, querier, companyId)
	if findError != nil {
		return license.Status{}, findError
	}
	return statusOf(foundSubscription, time.Now()), nil
}

func (service *Service) Refresh(ctx context.Context, querier database.Querier, companyId uuid.UUID) (license.Status, error) {
	foundSubscription, findError := service.findOrStartTrial(ctx, querier, companyId)
	if findError != nil {
		return license.Status{}, findError
	}

	remoteLicense, fetchError := license.FetchByHardwareId(DeviceIdFor(companyId))
	if errors.Is(fetchError, license.ErrLicensingServerUnreachable) {
		return statusOf(foundSubscription, time.Now()), fetchError
	}
	if fetchError != nil {
		return statusOf(foundSubscription, time.Now()), nil
	}

	remoteExpiry, parseError := time.Parse(time.RFC3339Nano, remoteLicense.ExpiresAt)
	if parseError != nil {
		return license.Status{}, fmt.Errorf("licensing server sent an unreadable expiry %q: %w", remoteLicense.ExpiresAt, parseError)
	}

	hasSubscription := foundSubscription != nil
	isRemoteNewer := hasSubscription && (foundSubscription.IsTrial || remoteExpiry.After(foundSubscription.ExpiresAt))
	if !isRemoteNewer {
		return statusOf(foundSubscription, time.Now()), nil
	}

	paidSubscription := Subscription{
		CompanyId:   companyId,
		LicenseKey:  remoteLicense.LicenseKey,
		ExpiresAt:   remoteExpiry.UTC(),
		DaysGranted: remoteLicense.DaysGranted,
		MaxDevices:  remoteLicense.MaxDevices,
		IsTrial:     false,
	}
	saveError := service.repository.SavePaid(ctx, querier, paidSubscription)
	if saveError != nil {
		return license.Status{}, saveError
	}
	return statusOf(&paidSubscription, time.Now()), nil
}

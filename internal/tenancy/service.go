package tenancy

import (
	"context"
	"errors"
	"github.com/chrisostomemataba/balceinv-api/license"
	"os"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/access"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/security"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/chrisostomemataba/balceinv-api/internal/users"
	"github.com/google/uuid"
)

var (
	ErrAlreadyConfigured = errors.New("this installation is already set up")
	ErrEmailTaken        = errors.New("this email is already in use")
)

type Service struct {
	repository         *Repository
	accessService      *access.Service
	usersRepository    *users.Repository
	settingsRepository *settings.Repository
}

func NewService(repository *Repository, accessService *access.Service, usersRepository *users.Repository, settingsRepository *settings.Repository) *Service {
	return &Service{
		repository:         repository,
		accessService:      accessService,
		usersRepository:    usersRepository,
		settingsRepository: settingsRepository,
	}
}

func (service *Service) IsConfigured(ctx context.Context, querier database.Querier) (bool, error) {
	companyCount, countError := service.repository.CountCompanies(ctx, querier)
	if countError != nil {
		return false, countError
	}
	return companyCount > 0, nil
}

func (service *Service) RunFirstSetup(ctx context.Context, querier database.Querier, isPostgres bool, request SetupRequest) (SetupResultView, error) {
	isConfigured, checkError := service.IsConfigured(ctx, querier)
	if checkError != nil {
		return SetupResultView{}, checkError
	}
	if isConfigured {
		return SetupResultView{}, ErrAlreadyConfigured
	}

	return service.CreateCompany(ctx, querier, isPostgres, request)
}

func (service *Service) CreateCompany(ctx context.Context, querier database.Querier, isPostgres bool, request SetupRequest) (SetupResultView, error) {
	createdAt := time.Now().UTC()
	companyId := uuid.Must(uuid.NewV7())

	setTenantError := database.SetTenant(ctx, querier, isPostgres, companyId)
	if setTenantError != nil {
		return SetupResultView{}, setTenantError
	}

	newCompany := Company{
		Id:               companyId,
		Name:             strings.TrimSpace(request.BusinessName),
		BusinessType:     valueOrDefault(strings.TrimSpace(request.BusinessType), "general"),
		Phone:            request.Phone,
		Address:          request.Address,
		Tin:              request.Tin,
		CurrencyCode:     valueOrDefault(request.CurrencyCode, "TZS"),
		CurrencyDecimals: decimalsOrDefault(request.CurrencyDecimals),
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
	}
	insertCompanyError := service.repository.InsertCompany(ctx, querier, newCompany)
	if insertCompanyError != nil {
		return SetupResultView{}, insertCompanyError
	}

	trialEndsAt := createdAt.AddDate(0, 0, license.TrialDurationDays)
	insertTrialError := service.repository.InsertTrialSubscription(ctx, querier, companyId, trialEndsAt, createdAt)
	if insertTrialError != nil {
		return SetupResultView{}, insertTrialError
	}

	insertSettingsError := service.settingsRepository.InsertDefaults(ctx, querier, companyId, createdAt)
	if insertSettingsError != nil {
		return SetupResultView{}, insertSettingsError
	}

	firstShop := Shop{
		Id:            uuid.Must(uuid.NewV7()),
		CompanyId:     companyId,
		Name:          valueOrDefault(strings.TrimSpace(request.ShopName), "Main Shop"),
		ReceiptPrefix: "SALE",
		IsActive:      true,
		CreatedAt:     createdAt,
		UpdatedAt:     createdAt,
	}
	insertShopError := service.repository.InsertShop(ctx, querier, firstShop)
	if insertShopError != nil {
		return SetupResultView{}, insertShopError
	}

	ownerRoleId, ownerRoleError := service.accessService.SetUpOwnerRole(ctx, querier, companyId, createdAt)
	if ownerRoleError != nil {
		return SetupResultView{}, ownerRoleError
	}

	passwordHash, hashError := security.HashPassword(request.OwnerPassword)
	if hashError != nil {
		return SetupResultView{}, hashError
	}

	ownerUser := users.User{
		Id:                 uuid.Must(uuid.NewV7()),
		CompanyId:          companyId,
		RoleId:             ownerRoleId,
		Name:               strings.TrimSpace(request.OwnerName),
		Email:              users.NormalizeEmail(request.OwnerEmail),
		PasswordHash:       passwordHash,
		IsActive:           true,
		MustChangePassword: request.OwnerMustReset,
		CreatedAt:          createdAt,
		UpdatedAt:          createdAt,
	}
	insertOwnerError := service.usersRepository.Insert(ctx, querier, ownerUser)
	if insertOwnerError != nil {
		if database.IsUniqueViolation(insertOwnerError) {
			return SetupResultView{}, ErrEmailTaken
		}
		return SetupResultView{}, insertOwnerError
	}

	_, assignShopError := service.usersRepository.ReplaceShops(ctx, querier, companyId, ownerUser.Id, []uuid.UUID{firstShop.Id})
	if assignShopError != nil {
		return SetupResultView{}, assignShopError
	}

	setupResult := SetupResultView{
		CompanyId: companyId,
		ShopId:    firstShop.Id,
		UserId:    ownerUser.Id,
	}

	return setupResult, nil
}

func valueOrDefault(value string, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

func decimalsOrDefault(requestedDecimals *int) int {
	if requestedDecimals == nil {
		return 0
	}
	return *requestedDecimals
}

func OldDesktopDataExists(oldDatabasePath string) bool {
	if oldDatabasePath == "" {
		return false
	}
	oldDatabaseInfo, statError := os.Stat(oldDatabasePath)
	if statError != nil {
		return false
	}
	return oldDatabaseInfo.Size() > 0
}

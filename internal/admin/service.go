package admin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/security"
	"github.com/chrisostomemataba/balceinv-api/internal/tenancy"
	"github.com/google/uuid"
)

const (
	SessionLifetime     = 8 * time.Hour
	salesWindow         = 30 * 24 * time.Hour
	shopPageLimit       = 50
	supportListLimit    = 200
	auditListLimit      = 200
	shopDetailAuditSize = 20
)

var (
	ErrBadSignIn        = errors.New("the email, password or code is wrong")
	ErrShopNotFound     = errors.New("shop not found")
	ErrUserNotFound     = errors.New("user not found in this business")
	ErrNotOnTrial       = errors.New("this business has a paid plan; renew it with the sales tool instead of extending a trial")
	ErrMessageNotFound  = errors.New("support message not found")
	ErrEmailAlreadyUsed = errors.New("this email is already in use")
)

type Service struct {
	repository     *Repository
	tenancyService *tenancy.Service
	isPostgres     bool
}

func NewService(repository *Repository, tenancyService *tenancy.Service, isPostgres bool) *Service {
	return &Service{repository: repository, tenancyService: tenancyService, isPostgres: isPostgres}
}

func NewOneTimePassword() (string, error) {
	randomBytes := make([]byte, 12)
	_, readError := rand.Read(randomBytes)
	if readError != nil {
		return "", fmt.Errorf("failed to generate a password: %w", readError)
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func toStaffView(staff Staff) StaffView {
	return StaffView{Id: staff.Id, Email: staff.Email, Name: staff.Name, Role: staff.Role}
}

func (service *Service) SignIn(ctx context.Context, querier database.Querier, request SignInRequest) (string, StaffView, error) {
	staff, findError := service.repository.FindStaffByEmail(ctx, querier, strings.ToLower(strings.TrimSpace(request.Email)))
	if findError != nil {
		return "", StaffView{}, findError
	}
	if staff == nil || !staff.IsActive {
		security.SpendComparisonTime(request.Password)
		return "", StaffView{}, ErrBadSignIn
	}
	isPasswordRight := security.PasswordMatches(staff.PasswordHash, request.Password)
	isCodeRight := TotpMatches(staff.TotpSecret, request.Code, time.Now().UTC())
	if !isPasswordRight || !isCodeRight {
		return "", StaffView{}, ErrBadSignIn
	}

	sessionToken, tokenHash, tokenError := security.NewSessionToken()
	if tokenError != nil {
		return "", StaffView{}, tokenError
	}
	now := time.Now().UTC()
	sessionError := service.repository.InsertSession(ctx, querier, tokenHash, staff.Id, now.Add(SessionLifetime), now)
	if sessionError != nil {
		return "", StaffView{}, sessionError
	}
	auditError := service.repository.InsertAudit(ctx, querier, staff.Id, "signed_in", nil, "", now)
	if auditError != nil {
		return "", StaffView{}, auditError
	}
	return sessionToken, toStaffView(*staff), nil
}

func (service *Service) StaffForToken(ctx context.Context, querier database.Querier, sessionToken string) (*Staff, error) {
	if sessionToken == "" {
		return nil, nil
	}
	return service.repository.FindStaffBySession(ctx, querier, security.HashSessionToken(sessionToken), time.Now().UTC())
}

func (service *Service) SignOut(ctx context.Context, querier database.Querier, sessionToken string) error {
	if sessionToken == "" {
		return nil
	}
	return service.repository.DeleteSession(ctx, querier, security.HashSessionToken(sessionToken))
}

func (service *Service) Shops(ctx context.Context, querier database.Querier, searchText string, offset int) (ShopPage, error) {
	openError := service.repository.OpenPlatformRead(ctx, querier, service.isPostgres)
	if openError != nil {
		return ShopPage{}, openError
	}
	return service.repository.ListShops(ctx, querier, searchText, time.Now().UTC().Add(-salesWindow), shopPageLimit, max(offset, 0))
}

func (service *Service) Shop(ctx context.Context, querier database.Querier, companyId uuid.UUID) (ShopDetailView, error) {
	openError := service.repository.OpenPlatformRead(ctx, querier, service.isPostgres)
	if openError != nil {
		return ShopDetailView{}, openError
	}
	shopDetail, findError := service.repository.FindShop(ctx, querier, companyId, time.Now().UTC().Add(-salesWindow))
	if findError != nil {
		return ShopDetailView{}, findError
	}
	if shopDetail == nil {
		return ShopDetailView{}, ErrShopNotFound
	}
	shopDetail.SubscriptionId = "cloud-" + companyId.String()
	recentAudit, auditError := service.repository.ListAudit(ctx, querier, &companyId, shopDetailAuditSize)
	if auditError != nil {
		return ShopDetailView{}, auditError
	}
	shopDetail.RecentAudit = recentAudit
	return *shopDetail, nil
}

func (service *Service) CreateShop(ctx context.Context, querier database.Querier, staff Staff, request CreateShopRequest) (CreatedShopView, error) {
	oneTimePassword, passwordError := NewOneTimePassword()
	if passwordError != nil {
		return CreatedShopView{}, passwordError
	}
	setupRequest := tenancy.SetupRequest{
		BusinessName:     strings.TrimSpace(request.BusinessName),
		ShopName:         strings.TrimSpace(request.ShopName),
		CurrencyCode:     request.CurrencyCode,
		CurrencyDecimals: request.CurrencyDecimals,
		OwnerName:        strings.TrimSpace(request.OwnerName),
		OwnerEmail:       strings.ToLower(strings.TrimSpace(request.OwnerEmail)),
		OwnerPassword:    oneTimePassword,
		OwnerMustReset:   true,
	}
	setupResult, setupError := service.tenancyService.CreateCompany(ctx, querier, service.isPostgres, setupRequest)
	if errors.Is(setupError, tenancy.ErrEmailTaken) {
		return CreatedShopView{}, ErrEmailAlreadyUsed
	}
	if setupError != nil {
		return CreatedShopView{}, setupError
	}

	auditError := service.repository.InsertAudit(ctx, querier, staff.Id, "created_shop", &setupResult.CompanyId, setupRequest.BusinessName+" · "+setupRequest.OwnerEmail, time.Now().UTC())
	if auditError != nil {
		return CreatedShopView{}, auditError
	}
	createdShop := CreatedShopView{CompanyId: setupResult.CompanyId, OwnerEmail: setupRequest.OwnerEmail, OneTimePassword: oneTimePassword}
	return createdShop, nil
}

func (service *Service) ExtendTrial(ctx context.Context, querier database.Querier, staff Staff, companyId uuid.UUID, request ExtendTrialRequest) (ExtendedTrialView, error) {
	shopExistsError := service.requireShop(ctx, querier, companyId)
	if shopExistsError != nil {
		return ExtendedTrialView{}, shopExistsError
	}
	tenantError := database.SetTenant(ctx, querier, service.isPostgres, companyId)
	if tenantError != nil {
		return ExtendedTrialView{}, tenantError
	}

	hasRow, isTrial, expiresAt, findError := service.repository.FindSubscription(ctx, querier, companyId)
	if findError != nil {
		return ExtendedTrialView{}, findError
	}
	if hasRow && !*isTrial {
		return ExtendedTrialView{}, ErrNotOnTrial
	}

	now := time.Now().UTC()
	startFrom := now
	if hasRow && expiresAt.After(now) {
		startFrom = *expiresAt
	}
	trialEndsAt := startFrom.Add(time.Duration(request.Days) * 24 * time.Hour)
	saveError := service.repository.SetTrialEnd(ctx, querier, companyId, hasRow, trialEndsAt, request.Days, now)
	if saveError != nil {
		return ExtendedTrialView{}, saveError
	}
	auditError := service.repository.InsertAudit(ctx, querier, staff.Id, "extended_trial", &companyId, daysText(request.Days)+" · until "+trialEndsAt.Format("2006-01-02"), now)
	if auditError != nil {
		return ExtendedTrialView{}, auditError
	}
	return ExtendedTrialView{TrialEndsAt: trialEndsAt}, nil
}

func (service *Service) ResetPassword(ctx context.Context, querier database.Querier, staff Staff, companyId uuid.UUID, userId uuid.UUID) (PasswordResetView, error) {
	shopExistsError := service.requireShop(ctx, querier, companyId)
	if shopExistsError != nil {
		return PasswordResetView{}, shopExistsError
	}
	tenantError := database.SetTenant(ctx, querier, service.isPostgres, companyId)
	if tenantError != nil {
		return PasswordResetView{}, tenantError
	}

	oneTimePassword, passwordError := NewOneTimePassword()
	if passwordError != nil {
		return PasswordResetView{}, passwordError
	}
	passwordHash, hashError := security.HashPassword(oneTimePassword)
	if hashError != nil {
		return PasswordResetView{}, hashError
	}
	now := time.Now().UTC()
	userEmail, resetError := service.repository.ResetUserPassword(ctx, querier, companyId, userId, passwordHash, now)
	if resetError != nil {
		return PasswordResetView{}, resetError
	}
	if userEmail == "" {
		return PasswordResetView{}, ErrUserNotFound
	}
	auditError := service.repository.InsertAudit(ctx, querier, staff.Id, "reset_password", &companyId, userEmail, now)
	if auditError != nil {
		return PasswordResetView{}, auditError
	}
	return PasswordResetView{Email: userEmail, OneTimePassword: oneTimePassword}, nil
}

func (service *Service) SupportMessages(ctx context.Context, querier database.Querier, onlyOpen bool) ([]SupportMessageView, error) {
	openError := service.repository.OpenPlatformRead(ctx, querier, service.isPostgres)
	if openError != nil {
		return nil, openError
	}
	return service.repository.ListSupportMessages(ctx, querier, onlyOpen, supportListLimit)
}

func (service *Service) MarkHandled(ctx context.Context, querier database.Querier, staff Staff, messageId uuid.UUID) error {
	openError := service.repository.OpenPlatformRead(ctx, querier, service.isPostgres)
	if openError != nil {
		return openError
	}
	companyId, findError := service.repository.FindSupportCompany(ctx, querier, messageId)
	if findError != nil {
		return findError
	}
	if companyId == nil {
		return ErrMessageNotFound
	}
	tenantError := database.SetTenant(ctx, querier, service.isPostgres, *companyId)
	if tenantError != nil {
		return tenantError
	}
	now := time.Now().UTC()
	markError := service.repository.MarkSupportHandled(ctx, querier, *companyId, messageId, staff.Id, now)
	if markError != nil {
		return markError
	}
	return service.repository.InsertAudit(ctx, querier, staff.Id, "handled_support", companyId, messageId.String(), now)
}

func (service *Service) Audit(ctx context.Context, querier database.Querier) ([]AuditView, error) {
	openError := service.repository.OpenPlatformRead(ctx, querier, service.isPostgres)
	if openError != nil {
		return nil, openError
	}
	return service.repository.ListAudit(ctx, querier, nil, auditListLimit)
}

func (service *Service) requireShop(ctx context.Context, querier database.Querier, companyId uuid.UUID) error {
	openError := service.repository.OpenPlatformRead(ctx, querier, service.isPostgres)
	if openError != nil {
		return openError
	}
	shopDetail, findError := service.repository.FindShop(ctx, querier, companyId, time.Now().UTC())
	if findError != nil {
		return findError
	}
	if shopDetail == nil {
		return ErrShopNotFound
	}
	return nil
}

func (service *Service) SaveStaff(ctx context.Context, querier database.Querier, email string, name string, role string) (string, string, error) {
	oneTimePassword, passwordError := NewOneTimePassword()
	if passwordError != nil {
		return "", "", passwordError
	}
	passwordHash, hashError := security.HashPassword(oneTimePassword)
	if hashError != nil {
		return "", "", hashError
	}
	totpSecret, secretError := NewTotpSecret()
	if secretError != nil {
		return "", "", secretError
	}
	staff := Staff{
		Id:           uuid.Must(uuid.NewV7()),
		Email:        strings.ToLower(strings.TrimSpace(email)),
		Name:         strings.TrimSpace(name),
		PasswordHash: passwordHash,
		TotpSecret:   totpSecret,
		Role:         role,
	}
	saveError := service.repository.SaveStaff(ctx, querier, staff, time.Now().UTC())
	if saveError != nil {
		return "", "", saveError
	}
	return oneTimePassword, totpSecret, nil
}

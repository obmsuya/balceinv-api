package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/access"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/security"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/chrisostomemataba/balceinv-api/internal/tenancy"
	"github.com/chrisostomemataba/balceinv-api/internal/users"
	"github.com/google/uuid"
)

const (
	SessionLifetime      = 30 * 24 * time.Hour
	SessionIdleTimeout   = 12 * time.Hour
	sessionTouchInterval = time.Minute
)

var (
	ErrInvalidCredentials = errors.New("email or password is incorrect")
	ErrSessionInvalid     = errors.New("session is missing, expired or revoked")
	ErrShopNotWorkable    = errors.New("you are not assigned to this shop")
)

type LoginOutcome struct {
	View         CurrentUserView
	SessionToken string
}

type Service struct {
	openDatabase      *database.Database
	repository        *Repository
	usersRepository   *users.Repository
	accessRepository  *access.Repository
	tenancyRepository *tenancy.Repository
}

func NewService(openDatabase *database.Database, repository *Repository, usersRepository *users.Repository, accessRepository *access.Repository, tenancyRepository *tenancy.Repository) *Service {
	return &Service{
		openDatabase:      openDatabase,
		repository:        repository,
		usersRepository:   usersRepository,
		accessRepository:  accessRepository,
		tenancyRepository: tenancyRepository,
	}
}

func (service *Service) Login(ctx context.Context, rawEmail string, password string, ipAddress string, userAgent string) (LoginOutcome, error) {
	isPostgres := service.openDatabase.IsPostgres()
	normalizedEmail := users.NormalizeEmail(rawEmail)
	attemptedAt := time.Now().UTC()

	loginTransaction, beginError := service.openDatabase.Writer.BeginTx(ctx, nil)
	if beginError != nil {
		return LoginOutcome{}, fmt.Errorf("failed to begin login transaction: %w", beginError)
	}
	defer loginTransaction.Rollback()

	enableLookupError := database.SetAuthLookup(ctx, loginTransaction, isPostgres, true)
	if enableLookupError != nil {
		return LoginOutcome{}, enableLookupError
	}

	loginCandidate, findCandidateError := service.repository.FindLoginCandidate(ctx, loginTransaction, normalizedEmail)
	if findCandidateError != nil {
		return LoginOutcome{}, findCandidateError
	}

	disableLookupError := database.SetAuthLookup(ctx, loginTransaction, isPostgres, false)
	if disableLookupError != nil {
		return LoginOutcome{}, disableLookupError
	}

	isKnownEmail := loginCandidate != nil
	passwordMatches := false
	if isKnownEmail {
		passwordMatches = security.PasswordMatches(loginCandidate.PasswordHash, password)
	} else {
		security.SpendComparisonTime(password)
	}
	isAccepted := isKnownEmail && passwordMatches && loginCandidate.IsActive

	loginAttempt := LoginAttempt{
		Id:        uuid.Must(uuid.NewV7()),
		Email:     normalizedEmail,
		IpAddress: ipAddress,
		Succeeded: isAccepted,
		CreatedAt: attemptedAt,
	}
	recordAttemptError := service.repository.InsertLoginAttempt(ctx, loginTransaction, loginAttempt)
	if recordAttemptError != nil {
		return LoginOutcome{}, recordAttemptError
	}

	if !isAccepted {
		commitAttemptError := loginTransaction.Commit()
		if commitAttemptError != nil {
			return LoginOutcome{}, fmt.Errorf("failed to record failed login: %w", commitAttemptError)
		}
		return LoginOutcome{}, ErrInvalidCredentials
	}

	setTenantError := database.SetTenant(ctx, loginTransaction, isPostgres, loginCandidate.CompanyId)
	if setTenantError != nil {
		return LoginOutcome{}, setTenantError
	}

	loggedInUser, findUserError := service.usersRepository.Find(ctx, loginTransaction, loginCandidate.CompanyId, loginCandidate.UserId)
	if findUserError != nil {
		return LoginOutcome{}, findUserError
	}

	workableShops, listShopsError := service.tenancyRepository.ListWorkableShops(ctx, loginTransaction, loggedInUser.CompanyId, loggedInUser.Id, loggedInUser.RoleIsOwner)
	if listShopsError != nil {
		return LoginOutcome{}, listShopsError
	}

	var initialShopId *uuid.UUID
	hasWorkableShop := len(workableShops) > 0
	if hasWorkableShop {
		initialShopId = &workableShops[0].Id
	}

	sessionToken, tokenHash, tokenError := security.NewSessionToken()
	if tokenError != nil {
		return LoginOutcome{}, tokenError
	}

	newSession := Session{
		Id:         uuid.Must(uuid.NewV7()),
		TokenHash:  tokenHash,
		CompanyId:  loggedInUser.CompanyId,
		UserId:     loggedInUser.Id,
		ShopId:     initialShopId,
		IpAddress:  ipAddress,
		UserAgent:  userAgent,
		CreatedAt:  attemptedAt,
		LastSeenAt: attemptedAt,
		ExpiresAt:  attemptedAt.Add(SessionLifetime),
	}
	insertSessionError := service.repository.InsertSession(ctx, loginTransaction, newSession)
	if insertSessionError != nil {
		return LoginOutcome{}, insertSessionError
	}

	currentUserView, buildViewError := service.buildCurrentUserView(ctx, loginTransaction, *loggedInUser, workableShops, initialShopId)
	if buildViewError != nil {
		return LoginOutcome{}, buildViewError
	}

	commitError := loginTransaction.Commit()
	if commitError != nil {
		return LoginOutcome{}, fmt.Errorf("failed to commit login: %w", commitError)
	}

	loginOutcome := LoginOutcome{
		View:         currentUserView,
		SessionToken: sessionToken,
	}

	return loginOutcome, nil
}

func (service *Service) Authenticate(ctx context.Context, sessionToken string) (*identity.Principal, error) {
	isPostgres := service.openDatabase.IsPostgres()
	tokenHash := security.HashSessionToken(sessionToken)
	checkedAt := time.Now().UTC()

	readTransaction, beginError := service.openDatabase.Reader.BeginTx(ctx, nil)
	if beginError != nil {
		return nil, fmt.Errorf("failed to begin session lookup: %w", beginError)
	}
	defer readTransaction.Rollback()

	enableLookupError := database.SetAuthLookup(ctx, readTransaction, isPostgres, true)
	if enableLookupError != nil {
		return nil, enableLookupError
	}

	foundSession, findSessionError := service.repository.FindSessionByTokenHash(ctx, readTransaction, tokenHash)
	if findSessionError != nil {
		return nil, findSessionError
	}
	if foundSession == nil {
		return nil, ErrSessionInvalid
	}

	disableLookupError := database.SetAuthLookup(ctx, readTransaction, isPostgres, false)
	if disableLookupError != nil {
		return nil, disableLookupError
	}

	setTenantError := database.SetTenant(ctx, readTransaction, isPostgres, foundSession.CompanyId)
	if setTenantError != nil {
		return nil, setTenantError
	}

	sessionUser, findUserError := service.usersRepository.Find(ctx, readTransaction, foundSession.CompanyId, foundSession.UserId)
	if findUserError != nil {
		return nil, findUserError
	}

	hasPassedLifetime := checkedAt.After(foundSession.ExpiresAt)
	hasBeenIdleTooLong := checkedAt.Sub(foundSession.LastSeenAt) > SessionIdleTimeout
	isUserUsable := sessionUser != nil && sessionUser.IsActive
	isSessionUsable := !hasPassedLifetime && !hasBeenIdleTooLong && isUserUsable
	if !isSessionUsable {
		readTransaction.Rollback()
		service.deleteSessionQuietly(ctx, foundSession.CompanyId, foundSession.Id)
		return nil, ErrSessionInvalid
	}

	effectivePermissions, permissionsError := service.accessRepository.ListEffectivePermissions(ctx, readTransaction, foundSession.CompanyId, foundSession.UserId, sessionUser.RoleIsOwner)
	if permissionsError != nil {
		return nil, permissionsError
	}
	readTransaction.Rollback()

	isDueForTouch := checkedAt.Sub(foundSession.LastSeenAt) > sessionTouchInterval
	if isDueForTouch {
		service.touchSessionQuietly(ctx, foundSession.CompanyId, foundSession.Id, checkedAt)
	}

	permissionIds := make(map[string]bool, len(effectivePermissions))
	for _, effectivePermission := range effectivePermissions {
		permissionIds[effectivePermission.Id] = true
	}

	principal := &identity.Principal{
		SessionId:     foundSession.Id,
		UserId:        sessionUser.Id,
		CompanyId:     sessionUser.CompanyId,
		ShopId:        foundSession.ShopId,
		RoleId:        sessionUser.RoleId,
		RoleName:      sessionUser.RoleName,
		IsOwner:       sessionUser.RoleIsOwner,
		PermissionIds: permissionIds,
	}

	return principal, nil
}

func (service *Service) Logout(ctx context.Context, querier database.Querier, principal *identity.Principal) error {
	return service.repository.DeleteSession(ctx, querier, principal.CompanyId, principal.SessionId)
}

func (service *Service) CurrentUser(ctx context.Context, querier database.Querier, principal *identity.Principal) (CurrentUserView, error) {
	sessionUser, findUserError := service.usersRepository.Find(ctx, querier, principal.CompanyId, principal.UserId)
	if findUserError != nil {
		return CurrentUserView{}, findUserError
	}
	if sessionUser == nil {
		return CurrentUserView{}, ErrSessionInvalid
	}

	workableShops, listShopsError := service.tenancyRepository.ListWorkableShops(ctx, querier, principal.CompanyId, principal.UserId, sessionUser.RoleIsOwner)
	if listShopsError != nil {
		return CurrentUserView{}, listShopsError
	}

	return service.buildCurrentUserView(ctx, querier, *sessionUser, workableShops, principal.ShopId)
}

func (service *Service) SwitchShop(ctx context.Context, querier database.Querier, principal *identity.Principal, shopId uuid.UUID) (CurrentUserView, error) {
	workableShops, listShopsError := service.tenancyRepository.ListWorkableShops(ctx, querier, principal.CompanyId, principal.UserId, principal.IsOwner)
	if listShopsError != nil {
		return CurrentUserView{}, listShopsError
	}

	isWorkableShop := false
	for _, workableShop := range workableShops {
		if workableShop.Id == shopId {
			isWorkableShop = true
		}
	}
	if !isWorkableShop {
		return CurrentUserView{}, ErrShopNotWorkable
	}

	updateError := service.repository.UpdateSessionShop(ctx, querier, principal.CompanyId, principal.SessionId, shopId)
	if updateError != nil {
		return CurrentUserView{}, updateError
	}

	switchedPrincipal := *principal
	switchedPrincipal.ShopId = &shopId

	return service.CurrentUser(ctx, querier, &switchedPrincipal)
}

func (service *Service) buildCurrentUserView(ctx context.Context, querier database.Querier, sessionUser users.User, workableShops []tenancy.ShopSummary, sessionShopId *uuid.UUID) (CurrentUserView, error) {
	companyBranding, brandingError := service.tenancyRepository.FindBranding(ctx, querier, sessionUser.CompanyId)
	if brandingError != nil {
		return CurrentUserView{}, brandingError
	}

	effectivePermissions, permissionsError := service.accessRepository.ListEffectivePermissions(ctx, querier, sessionUser.CompanyId, sessionUser.Id, sessionUser.RoleIsOwner)
	if permissionsError != nil {
		return CurrentUserView{}, permissionsError
	}

	permissionViews := make([]access.PermissionView, 0, len(effectivePermissions))
	for _, effectivePermission := range effectivePermissions {
		permissionViews = append(permissionViews, access.PermissionView{
			Id:          effectivePermission.Id,
			Resource:    effectivePermission.Resource,
			Action:      effectivePermission.Action,
			Description: effectivePermission.Description,
		})
	}

	currentUserView := CurrentUserView{
		Id:          sessionUser.Id,
		Name:        sessionUser.Name,
		Email:       sessionUser.Email,
		Role:        sessionUser.RoleName,
		RoleId:      sessionUser.RoleId,
		IsOwner:     sessionUser.RoleIsOwner,
		CompanyId:   sessionUser.CompanyId,
		CompanyName: companyBranding.Name,
		Branding: BrandingView{
			LogoUrl:          media.PublicUrl(companyBranding.LogoKey),
			PrimaryColor:     companyBranding.PrimaryColor,
			CurrencyCode:     companyBranding.CurrencyCode,
			CurrencyDecimals: companyBranding.CurrencyDecimals,
			Timezone:         companyBranding.Timezone,
			DefaultLocale:    companyBranding.DefaultLocale,
		},
		ShopId:             workableShopOrNil(sessionShopId, workableShops),
		Shops:              workableShops,
		Permissions:        permissionViews,
		Locale:             sessionUser.Locale,
		MustChangePassword: sessionUser.MustChangePassword,
	}

	return currentUserView, nil
}

func (service *Service) deleteSessionQuietly(ctx context.Context, companyId uuid.UUID, sessionId uuid.UUID) {
	writeError := service.writeForCompany(ctx, companyId, func(writeTransaction database.Querier) error {
		return service.repository.DeleteSession(ctx, writeTransaction, companyId, sessionId)
	})
	if writeError != nil {
		slog.Warn("could not delete unusable session", "sessionId", sessionId, "error", writeError)
	}
}

func (service *Service) touchSessionQuietly(ctx context.Context, companyId uuid.UUID, sessionId uuid.UUID, seenAt time.Time) {
	writeError := service.writeForCompany(ctx, companyId, func(writeTransaction database.Querier) error {
		return service.repository.TouchSession(ctx, writeTransaction, companyId, sessionId, seenAt)
	})
	if writeError != nil {
		slog.Warn("could not refresh session activity", "sessionId", sessionId, "error", writeError)
	}
}

func (service *Service) writeForCompany(ctx context.Context, companyId uuid.UUID, write func(writeTransaction database.Querier) error) error {
	writeTransaction, beginError := service.openDatabase.Writer.BeginTx(ctx, nil)
	if beginError != nil {
		return fmt.Errorf("failed to begin session write: %w", beginError)
	}
	defer writeTransaction.Rollback()

	setTenantError := database.SetTenant(ctx, writeTransaction, service.openDatabase.IsPostgres(), companyId)
	if setTenantError != nil {
		return setTenantError
	}

	writeError := write(writeTransaction)
	if writeError != nil {
		return writeError
	}

	return writeTransaction.Commit()
}

func workableShopOrNil(sessionShopId *uuid.UUID, workableShops []tenancy.ShopSummary) *uuid.UUID {
	if sessionShopId == nil {
		return nil
	}
	for _, workableShop := range workableShops {
		if workableShop.Id == *sessionShopId {
			return sessionShopId
		}
	}
	return nil
}

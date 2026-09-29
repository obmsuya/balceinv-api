package users

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/access"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/common/security"
	"github.com/google/uuid"
)

var (
	ErrUserNotFound             = errors.New("user not found")
	ErrRoleNotFound             = errors.New("role not found")
	ErrShopNotFound             = errors.New("one or more shops were not found")
	ErrEmailTaken               = errors.New("this email is already in use")
	ErrOnlyOwnerCanManageOwners = errors.New("only an owner can create, change or reset an owner")
	ErrLastOwner                = errors.New("the company needs at least one active owner")
	ErrCannotDeactivateSelf     = errors.New("you cannot deactivate your own account")
	ErrPasswordTooLong          = errors.New("password must be at most 72 bytes")
	ErrForbidden                = errors.New("you do not have permission to do this")
)

type Service struct {
	repository       *Repository
	accessRepository *access.Repository
}

func NewService(repository *Repository, accessRepository *access.Repository) *Service {
	return &Service{
		repository:       repository,
		accessRepository: accessRepository,
	}
}

func (service *Service) List(ctx context.Context, querier database.Querier, companyId uuid.UUID, searchText string, limit int, offset int) (response.Page[UserView], error) {
	emptyPage := response.Page[UserView]{}

	totalUsers, countError := service.repository.Count(ctx, querier, companyId, searchText)
	if countError != nil {
		return emptyPage, countError
	}

	pageUsers, listError := service.repository.List(ctx, querier, companyId, searchText, limit, offset)
	if listError != nil {
		return emptyPage, listError
	}

	pageUserIds := make([]uuid.UUID, 0, len(pageUsers))
	for _, pageUser := range pageUsers {
		pageUserIds = append(pageUserIds, pageUser.Id)
	}

	shopIdsByUser, shopsError := service.repository.ListShopIds(ctx, querier, companyId, pageUserIds)
	if shopsError != nil {
		return emptyPage, shopsError
	}

	userViews := make([]UserView, 0, len(pageUsers))
	for _, pageUser := range pageUsers {
		userViews = append(userViews, toUserView(pageUser, shopIdsByUser[pageUser.Id]))
	}

	userPage := response.Page[UserView]{
		Items:  userViews,
		Total:  totalUsers,
		Limit:  limit,
		Offset: offset,
	}

	return userPage, nil
}

func (service *Service) Get(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID) (UserView, error) {
	foundUser, findError := service.repository.Find(ctx, querier, companyId, userId)
	if findError != nil {
		return UserView{}, findError
	}
	if foundUser == nil {
		return UserView{}, ErrUserNotFound
	}

	shopIdsByUser, shopsError := service.repository.ListShopIds(ctx, querier, companyId, []uuid.UUID{userId})
	if shopsError != nil {
		return UserView{}, shopsError
	}

	return toUserView(*foundUser, shopIdsByUser[userId]), nil
}

func (service *Service) Create(ctx context.Context, querier database.Querier, principal *identity.Principal, request CreateUserRequest) (UserView, error) {
	roleId := uuid.MustParse(request.RoleId)
	chosenRole, findRoleError := service.accessRepository.FindRole(ctx, querier, principal.CompanyId, roleId)
	if findRoleError != nil {
		return UserView{}, findRoleError
	}
	if chosenRole == nil {
		return UserView{}, ErrRoleNotFound
	}
	if chosenRole.IsOwner && !principal.IsOwner {
		return UserView{}, ErrOnlyOwnerCanManageOwners
	}

	passwordHash, hashError := hashAllowedPassword(request.Password)
	if hashError != nil {
		return UserView{}, hashError
	}

	createdAt := time.Now().UTC()
	newUser := User{
		Id:                 uuid.Must(uuid.NewV7()),
		CompanyId:          principal.CompanyId,
		RoleId:             roleId,
		Name:               strings.TrimSpace(request.Name),
		Email:              NormalizeEmail(request.Email),
		PasswordHash:       passwordHash,
		IsActive:           true,
		MustChangePassword: false,
		CreatedAt:          createdAt,
		UpdatedAt:          createdAt,
	}

	insertError := service.repository.Insert(ctx, querier, newUser)
	if insertError != nil {
		if database.IsUniqueViolation(insertError) {
			return UserView{}, ErrEmailTaken
		}
		return UserView{}, insertError
	}

	assignShopsError := service.assignShops(ctx, querier, principal.CompanyId, newUser.Id, request.ShopIds)
	if assignShopsError != nil {
		return UserView{}, assignShopsError
	}

	return service.Get(ctx, querier, principal.CompanyId, newUser.Id)
}

func (service *Service) Update(ctx context.Context, querier database.Querier, principal *identity.Principal, userId uuid.UUID, request UpdateUserRequest) (UserView, error) {
	existingUser, findError := service.repository.Find(ctx, querier, principal.CompanyId, userId)
	if findError != nil {
		return UserView{}, findError
	}
	if existingUser == nil {
		return UserView{}, ErrUserNotFound
	}

	isActive := existingUser.IsActive
	hasActiveFlag := request.IsActive != nil
	if hasActiveFlag {
		isActive = *request.IsActive
	}

	changedUser := *existingUser
	changedUser.Name = strings.TrimSpace(request.Name)
	changedUser.Email = NormalizeEmail(request.Email)
	changedUser.RoleId = uuid.MustParse(request.RoleId)
	changedUser.IsActive = isActive

	saveError := service.saveGuardedChange(ctx, querier, principal, *existingUser, changedUser)
	if saveError != nil {
		return UserView{}, saveError
	}

	hasShopChanges := request.ShopIds != nil
	if hasShopChanges {
		assignShopsError := service.assignShops(ctx, querier, principal.CompanyId, userId, request.ShopIds)
		if assignShopsError != nil {
			return UserView{}, assignShopsError
		}
	}

	return service.Get(ctx, querier, principal.CompanyId, userId)
}

func (service *Service) AssignRole(ctx context.Context, querier database.Querier, principal *identity.Principal, userId uuid.UUID, roleId uuid.UUID) (UserView, error) {
	existingUser, findError := service.repository.Find(ctx, querier, principal.CompanyId, userId)
	if findError != nil {
		return UserView{}, findError
	}
	if existingUser == nil {
		return UserView{}, ErrUserNotFound
	}

	changedUser := *existingUser
	changedUser.RoleId = roleId

	saveError := service.saveGuardedChange(ctx, querier, principal, *existingUser, changedUser)
	if saveError != nil {
		return UserView{}, saveError
	}

	return service.Get(ctx, querier, principal.CompanyId, userId)
}

func (service *Service) Deactivate(ctx context.Context, querier database.Querier, principal *identity.Principal, userId uuid.UUID) error {
	existingUser, findError := service.repository.Find(ctx, querier, principal.CompanyId, userId)
	if findError != nil {
		return findError
	}
	if existingUser == nil {
		return ErrUserNotFound
	}

	changedUser := *existingUser
	changedUser.IsActive = false

	return service.saveGuardedChange(ctx, querier, principal, *existingUser, changedUser)
}

func (service *Service) ChangePassword(ctx context.Context, querier database.Querier, principal *identity.Principal, userId uuid.UUID, newPassword string) error {
	targetUser, findError := service.repository.Find(ctx, querier, principal.CompanyId, userId)
	if findError != nil {
		return findError
	}
	if targetUser == nil {
		return ErrUserNotFound
	}

	isOwnPassword := targetUser.Id == principal.UserId
	canEditUsers := principal.Can("users:edit")
	if !isOwnPassword && !canEditUsers {
		return ErrForbidden
	}
	if !isOwnPassword && targetUser.RoleIsOwner && !principal.IsOwner {
		return ErrOnlyOwnerCanManageOwners
	}

	passwordHash, hashError := hashAllowedPassword(newPassword)
	if hashError != nil {
		return hashError
	}

	updateError := service.repository.UpdatePasswordHash(ctx, querier, principal.CompanyId, userId, passwordHash)
	if updateError != nil {
		return updateError
	}

	keptSessionId := uuid.Nil
	if isOwnPassword {
		keptSessionId = principal.SessionId
	}

	return service.repository.RevokeSessions(ctx, querier, principal.CompanyId, userId, keptSessionId)
}

func (service *Service) saveGuardedChange(ctx context.Context, querier database.Querier, principal *identity.Principal, existingUser User, changedUser User) error {
	newRole, findRoleError := service.accessRepository.FindRole(ctx, querier, principal.CompanyId, changedUser.RoleId)
	if findRoleError != nil {
		return findRoleError
	}
	if newRole == nil {
		return ErrRoleNotFound
	}

	touchesOwnerRole := existingUser.RoleIsOwner || newRole.IsOwner
	if touchesOwnerRole && !principal.IsOwner {
		return ErrOnlyOwnerCanManageOwners
	}

	isDeactivatingSelf := existingUser.Id == principal.UserId && existingUser.IsActive && !changedUser.IsActive
	if isDeactivatingSelf {
		return ErrCannotDeactivateSelf
	}

	wasActiveOwner := existingUser.RoleIsOwner && existingUser.IsActive
	remainsActiveOwner := newRole.IsOwner && changedUser.IsActive
	isLosingAnOwner := wasActiveOwner && !remainsActiveOwner
	if isLosingAnOwner {
		activeOwnerCount, countError := service.repository.CountActiveOwners(ctx, querier, principal.CompanyId)
		if countError != nil {
			return countError
		}
		if activeOwnerCount <= 1 {
			return ErrLastOwner
		}
	}

	updateError := service.repository.UpdateProfile(ctx, querier, changedUser)
	if updateError != nil {
		if database.IsUniqueViolation(updateError) {
			return ErrEmailTaken
		}
		return updateError
	}

	roleChanged := existingUser.RoleId != changedUser.RoleId
	wasDeactivated := existingUser.IsActive && !changedUser.IsActive
	shouldRevokeSessions := roleChanged || wasDeactivated
	if shouldRevokeSessions {
		return service.repository.RevokeSessions(ctx, querier, principal.CompanyId, changedUser.Id, uuid.Nil)
	}

	return nil
}

func (service *Service) assignShops(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, rawShopIds []string) error {
	shopIds := uniqueUuids(rawShopIds)

	assignedCount, assignError := service.repository.ReplaceShops(ctx, querier, companyId, userId, shopIds)
	if assignError != nil {
		return assignError
	}

	allShopsFound := assignedCount == int64(len(shopIds))
	if !allShopsFound {
		return ErrShopNotFound
	}

	return nil
}

func NormalizeEmail(rawEmail string) string {
	return strings.ToLower(strings.TrimSpace(rawEmail))
}

func hashAllowedPassword(plainPassword string) (string, error) {
	isTooLong := len([]byte(plainPassword)) > 72
	if isTooLong {
		return "", ErrPasswordTooLong
	}
	return security.HashPassword(plainPassword)
}

func uniqueUuids(rawIds []string) []uuid.UUID {
	seenIds := map[uuid.UUID]bool{}
	uniqueIds := make([]uuid.UUID, 0, len(rawIds))
	for _, rawId := range rawIds {
		parsedId := uuid.MustParse(rawId)
		if seenIds[parsedId] {
			continue
		}
		seenIds[parsedId] = true
		uniqueIds = append(uniqueIds, parsedId)
	}
	return uniqueIds
}

func toUserView(user User, shopIds []uuid.UUID) UserView {
	hasNoShopIds := shopIds == nil
	if hasNoShopIds {
		shopIds = []uuid.UUID{}
	}

	return UserView{
		Id:     user.Id,
		Name:   user.Name,
		Email:  user.Email,
		RoleId: user.RoleId,
		Role: RoleSummary{
			Id:      user.RoleId,
			Name:    user.RoleName,
			IsOwner: user.RoleIsOwner,
		},
		ShopIds:            shopIds,
		Locale:             user.Locale,
		IsActive:           user.IsActive,
		MustChangePassword: user.MustChangePassword,
		CreatedAt:          user.CreatedAt,
		UpdatedAt:          user.UpdatedAt,
	}
}

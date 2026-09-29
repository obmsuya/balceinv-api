package identity

import "github.com/google/uuid"

type Principal struct {
	SessionId     uuid.UUID
	UserId        uuid.UUID
	CompanyId     uuid.UUID
	ShopId        *uuid.UUID
	RoleId        uuid.UUID
	RoleName      string
	IsOwner       bool
	PermissionIds map[string]bool
}

func (principal *Principal) Can(permissionId string) bool {
	if principal.IsOwner {
		return true
	}
	return principal.PermissionIds[permissionId]
}

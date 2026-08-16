package auth

import "errors"

var (
	ErrRoleManagementForbidden = errors.New("role management is forbidden")
	ErrRootOwnerProtected      = errors.New("root owner cannot be removed or demoted")
	ErrLastOwnerProtected      = errors.New("last owner cannot be removed or demoted")
	ErrInvalidRoleChange       = errors.New("role change is invalid")
)

type Role string

const (
	RoleOwner         Role = "owner"
	RoleAdministrator Role = "administrator"
)

type Permission string

const (
	PermissionManageUsers          Permission = "manage_users"
	PermissionManageApprovals      Permission = "manage_approvals"
	PermissionManageQuota          Permission = "manage_quota"
	PermissionViewAudit            Permission = "view_audit"
	PermissionManageSecrets        Permission = "manage_secrets"
	PermissionManageRoles          Permission = "manage_roles"
	PermissionManageVPNSettings    Permission = "manage_vpn_settings"
	PermissionManageGlobalSettings Permission = "manage_global_settings"
)

func (role Role) Allows(permission Permission) bool {
	switch permission {
	case PermissionManageUsers, PermissionManageApprovals, PermissionManageQuota, PermissionViewAudit:
		return role == RoleOwner || role == RoleAdministrator
	case PermissionManageSecrets, PermissionManageRoles, PermissionManageVPNSettings, PermissionManageGlobalSettings:
		return role == RoleOwner
	default:
		return false
	}
}

func ValidateRoleChange(actorRole, targetRole Role, targetIsRoot bool, ownerCount int, newRole *Role) error {
	if actorRole != RoleOwner {
		return ErrRoleManagementForbidden
	}
	if ownerCount < 0 || (targetRole != "" && targetRole != RoleOwner && targetRole != RoleAdministrator) {
		return ErrInvalidRoleChange
	}
	if newRole != nil && *newRole != RoleOwner && *newRole != RoleAdministrator {
		return ErrInvalidRoleChange
	}
	isRemovalOrDemotion := newRole == nil || *newRole != RoleOwner
	if targetIsRoot && isRemovalOrDemotion {
		return ErrRootOwnerProtected
	}
	if targetRole == RoleOwner && ownerCount <= 1 && isRemovalOrDemotion {
		return ErrLastOwnerProtected
	}
	return nil
}

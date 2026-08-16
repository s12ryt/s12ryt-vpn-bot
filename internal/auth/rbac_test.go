package auth

import (
	"errors"
	"testing"
)

func TestRolePermissions(t *testing.T) {
	adminAllowed := []Permission{
		PermissionManageUsers,
		PermissionManageApprovals,
		PermissionManageQuota,
		PermissionViewAudit,
	}
	for _, permission := range adminAllowed {
		if !RoleAdministrator.Allows(permission) {
			t.Errorf("administrator should allow %q", permission)
		}
	}
	adminDenied := []Permission{
		PermissionManageSecrets,
		PermissionManageRoles,
		PermissionManageVPNSettings,
		PermissionManageGlobalSettings,
	}
	for _, permission := range adminDenied {
		if RoleAdministrator.Allows(permission) {
			t.Errorf("administrator should deny %q", permission)
		}
	}
	for _, permission := range append(adminAllowed, adminDenied...) {
		if !RoleOwner.Allows(permission) {
			t.Errorf("owner should allow %q", permission)
		}
	}
	if Role("unknown").Allows(PermissionManageUsers) {
		t.Fatal("unknown role must not have permissions")
	}
}

func TestValidateRoleChangeProtectsRootAndLastOwner(t *testing.T) {
	administrator := RoleAdministrator
	tests := []struct {
		name       string
		actorRole  Role
		targetRoot bool
		ownerCount int
		newRole    *Role
		wantErr    error
	}{
		{name: "administrator cannot manage roles", actorRole: RoleAdministrator, ownerCount: 2, newRole: &administrator, wantErr: ErrRoleManagementForbidden},
		{name: "root owner cannot be removed", actorRole: RoleOwner, targetRoot: true, ownerCount: 2, newRole: nil, wantErr: ErrRootOwnerProtected},
		{name: "root owner cannot be demoted", actorRole: RoleOwner, targetRoot: true, ownerCount: 2, newRole: &administrator, wantErr: ErrRootOwnerProtected},
		{name: "last owner cannot be removed", actorRole: RoleOwner, ownerCount: 1, newRole: nil, wantErr: ErrLastOwnerProtected},
		{name: "last owner cannot be demoted", actorRole: RoleOwner, ownerCount: 1, newRole: &administrator, wantErr: ErrLastOwnerProtected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRoleChange(tt.actorRole, RoleOwner, tt.targetRoot, tt.ownerCount, tt.newRole)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateRoleChange() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRoleChangeAllowsOwnerToManageNonRootRoles(t *testing.T) {
	administrator := RoleAdministrator
	owner := RoleOwner
	tests := []struct {
		name       string
		targetRole Role
		ownerCount int
		newRole    *Role
	}{
		{name: "add administrator", targetRole: "", ownerCount: 1, newRole: &administrator},
		{name: "add owner", targetRole: "", ownerCount: 1, newRole: &owner},
		{name: "demote owner when another remains", targetRole: RoleOwner, ownerCount: 2, newRole: &administrator},
		{name: "remove administrator", targetRole: RoleAdministrator, ownerCount: 1, newRole: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateRoleChange(RoleOwner, tt.targetRole, false, tt.ownerCount, tt.newRole); err != nil {
				t.Fatalf("ValidateRoleChange() error = %v", err)
			}
		})
	}
}

func TestValidateRoleChangeRejectsInvalidRoleData(t *testing.T) {
	unknown := Role("unknown")
	administrator := RoleAdministrator
	tests := []struct {
		name       string
		targetRole Role
		ownerCount int
		newRole    *Role
	}{
		{name: "unknown target role", targetRole: unknown, ownerCount: 1, newRole: &administrator},
		{name: "unknown new role", targetRole: RoleAdministrator, ownerCount: 1, newRole: &unknown},
		{name: "negative owner count", targetRole: RoleAdministrator, ownerCount: -1, newRole: &administrator},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRoleChange(RoleOwner, tt.targetRole, false, tt.ownerCount, tt.newRole)
			if !errors.Is(err, ErrInvalidRoleChange) {
				t.Fatalf("ValidateRoleChange() error = %v, want %v", err, ErrInvalidRoleChange)
			}
		})
	}
}

package workspaceaccess

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type Store interface {
	GetAdminUser(context.Context, string, string) (db.AdminUser, error)
	GetAdminWorkspace(context.Context, string, string) (db.AdminWorkspace, error)
	GetAdminWorkspaceMember(context.Context, string, string, string) (db.AdminWorkspaceMember, error)
}

type Service struct{ store Store }

func New(store Store) *Service { return &Service{store: store} }

func (s *Service) Resolve(ctx context.Context, organizationUUID, userID, workspaceID string) (db.AdminWorkspace, auth.WorkspaceAccess, error) {
	user, err := s.store.GetAdminUser(ctx, organizationUUID, userID)
	if err != nil {
		return db.AdminWorkspace{}, auth.WorkspaceAccess{}, accessError(err)
	}
	if user.OrganizationUUID != organizationUUID {
		return db.AdminWorkspace{}, auth.WorkspaceAccess{}, ErrDenied
	}
	workspace, err := s.store.GetAdminWorkspace(ctx, organizationUUID, workspaceID)
	if err != nil {
		return db.AdminWorkspace{}, auth.WorkspaceAccess{}, accessError(err)
	}
	if workspace.OrganizationUUID != organizationUUID || workspace.ArchivedAt != nil {
		return workspace, auth.WorkspaceAccess{}, ErrDenied
	}
	explicitRole := ""
	if !workspace.IsDefault && user.Role != "admin" {
		member, memberErr := s.store.GetAdminWorkspaceMember(ctx, organizationUUID, workspace.ExternalID, user.ExternalID)
		if memberErr != nil && !errors.Is(memberErr, db.ErrNotFound) {
			return workspace, auth.WorkspaceAccess{}, memberErr
		}
		if memberErr == nil && member.UserUUID == user.UUID && member.WorkspaceUUID == workspace.UUID {
			explicitRole = member.WorkspaceRole
		}
	}
	access, err := Effective(user.Role, workspace.IsDefault, explicitRole)
	return workspace, access, err
}

func Effective(organizationRole string, isDefault bool, explicitRole string) (auth.WorkspaceAccess, error) {
	inherited := map[string]string{"admin": "workspace_admin", "user": "workspace_user"}
	role, known := inherited[organizationRole]
	if !known {
		return auth.WorkspaceAccess{}, ErrDenied
	}
	access := auth.WorkspaceAccess{OrganizationRole: organizationRole, Role: role, Source: "organization"}
	if isDefault || organizationRole == "admin" {
		return access, nil
	}
	if !Assignable(explicitRole) {
		return auth.WorkspaceAccess{}, ErrDenied
	}
	access.Role = explicitRole
	access.Source = "membership"
	return access, nil
}

func Assignable(role string) bool {
	return role == "workspace_admin" || role == "workspace_user"
}

func accessError(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return ErrDenied
	}
	return err
}

func (s *Service) ResolvePrincipal(ctx context.Context, principal auth.Principal, workspaceID string) (db.AdminWorkspace, auth.WorkspaceAccess, error) {
	if !principal.PlatformOrganizationAdmin(principal.OrganizationUUID) {
		if principal.UserExternalID == "" {
			return db.AdminWorkspace{}, auth.WorkspaceAccess{}, ErrDenied
		}
		return s.Resolve(ctx, principal.OrganizationUUID, principal.UserExternalID, workspaceID)
	}
	workspace, err := s.store.GetAdminWorkspace(ctx, principal.OrganizationUUID, workspaceID)
	if err != nil {
		return workspace, auth.WorkspaceAccess{}, accessError(err)
	}
	if workspace.OrganizationUUID != principal.OrganizationUUID || workspace.ArchivedAt != nil {
		return workspace, auth.WorkspaceAccess{}, ErrDenied
	}
	access, err := Effective("admin", workspace.IsDefault, "")
	access.Source = string(auth.OrganizationAccessPlatformAdmin)
	return workspace, access, err
}

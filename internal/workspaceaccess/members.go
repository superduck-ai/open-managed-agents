package workspaceaccess

import (
	"context"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

// ChangeMember 先锁定组织成员，再锁定工作区，校验和写入共享一个事务。
func ChangeMember(ctx context.Context, database *db.DB, principal auth.Principal, workspaceID, userID, role, operation string) (db.AdminWorkspaceMember, error) {
	var result db.AdminWorkspaceMember
	err := database.WithWorkspaceMemberTx(ctx, principal.OrganizationUUID, principal.UserExternalID, userID, workspaceID, func(tx *db.WorkspaceMemberTx) error {
		workspace, err := tx.GetAdminWorkspace(ctx, principal.OrganizationUUID, workspaceID)
		if err != nil {
			return err
		}
		if workspace.ArchivedAt != nil {
			return ErrDenied
		}
		if err := authorizeMemberChange(ctx, tx, principal, workspace.ExternalID); err != nil {
			return err
		}
		if workspace.IsDefault {
			return ErrDefaultProtected
		}
		user, err := tx.GetAdminUser(ctx, principal.OrganizationUUID, userID)
		if err != nil {
			return err
		}
		if err := validateMemberChange(user.Role, role, operation); err != nil {
			return err
		}
		result, err = writeMemberChange(ctx, tx, workspace, user, role, operation)
		return err
	})
	return result, err
}

func authorizeMemberChange(ctx context.Context, tx *db.WorkspaceMemberTx, principal auth.Principal, workspaceID string) error {
	// 工作区 Key 没有用户主体，不能因此取得组织成员管理权限。
	if principal.UserExternalID == "" {
		return ErrDenied
	}
	_, access, err := New(tx).Resolve(ctx, principal.OrganizationUUID, principal.UserExternalID, workspaceID)
	if err != nil {
		return err
	}
	if !access.ManageMembers() {
		return ErrDenied
	}
	return nil
}

func validateMemberChange(orgRole, role, operation string) error {
	if orgRole == "admin" {
		return ErrInheritedRole
	}
	if _, err := Effective(orgRole, true, ""); err != nil {
		return err
	}
	if operation != "delete" && !Assignable(role) && !(operation == "update" && role == "workspace_billing") {
		return ErrInvalidRole
	}
	return nil
}

func writeMemberChange(ctx context.Context, tx *db.WorkspaceMemberTx, workspace db.AdminWorkspace, user db.AdminUser, role, operation string) (db.AdminWorkspaceMember, error) {
	member, err := tx.GetAdminWorkspaceMember(ctx, workspace.OrganizationUUID, workspace.ExternalID, user.ExternalID)
	found := err == nil
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return member, err
	}
	if operation == "delete" {
		return tx.DeleteMember(ctx, workspace.OrganizationUUID, workspace.ExternalID, user.ExternalID)
	}
	if found {
		if operation == "create" {
			return member, db.ErrDuplicate
		}
		return tx.UpdateMember(ctx, workspace.OrganizationUUID, workspace.ExternalID, user.ExternalID, role)
	}
	if operation == "update" {
		return member, db.ErrNotFound
	}
	externalID, err := ids.New("wmem_")
	if err != nil {
		return member, err
	}
	return tx.InsertMember(ctx, db.AdminWorkspaceMember{
		ExternalID: externalID, OrganizationUUID: workspace.OrganizationUUID, WorkspaceUUID: workspace.UUID,
		WorkspaceExternalID: workspace.ExternalID, UserUUID: user.UUID, UserExternalID: user.ExternalID,
		WorkspaceRole: role, CreatedAt: time.Now().UTC(),
	})
}

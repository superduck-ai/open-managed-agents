package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/yourbatis"
)

type DevIdentity struct {
	OrganizationUUID string
	OrganizationName string
	User             AdminUser
	Workspaces       []AdminWorkspace
}

func (d *DB) CreateDevIdentity(ctx context.Context, input DevIdentity) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		organizations := NewAdminOrganizationMapper(executor)
		if _, err := organizations.FindByUUID(ctx, input.OrganizationUUID); err == nil {
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := organizations.Insert(ctx, input.OrganizationUUID, input.OrganizationName); err != nil {
			return err
		}
		userUUID, err := NewAdminUserMapper(executor).Insert(ctx, insertAdminUserParams{
			ExternalID: input.User.ExternalID, OrganizationUUID: input.OrganizationUUID,
			Email: input.User.Email, Name: input.User.Name, Role: input.User.Role,
		})
		if err != nil {
			return err
		}
		return createDevWorkspaces(ctx, executor, input, userUUID)
	})
}

func createDevWorkspaces(ctx context.Context, executor yourbatis.Executor, input DevIdentity, userUUID string) error {
	workspaces := NewAdminWorkspaceMapper(executor)
	members := NewAdminWorkspaceMemberMapper(executor)
	for _, workspace := range input.Workspaces {
		if _, err := workspaces.Insert(ctx, workspaceInsertParams(workspace)); err != nil {
			return err
		}
		if workspace.IsDefault {
			continue
		}
		if _, err := members.Insert(ctx, devMembershipParams(input.OrganizationUUID, workspace, input.User.ExternalID, userUUID)); err != nil {
			return err
		}
	}
	return nil
}

func devMembershipParams(organizationUUID string, workspace AdminWorkspace, userExternalID, userUUID string) insertAdminWorkspaceMemberParams {
	return insertAdminWorkspaceMemberParams{
		ExternalID:       "member_dev_init_" + userExternalID + "_" + workspace.ExternalID,
		OrganizationUUID: organizationUUID, WorkspaceUUID: workspace.UUID,
		WorkspaceExternalID: workspace.ExternalID, UserUUID: userUUID, UserExternalID: userExternalID,
		WorkspaceRole: "workspace_admin", CreatedAt: time.Now().UTC(),
	}
}

// EnsureAdminWorkspaceMember inserts an active membership when none exists.
func (d *DB) EnsureAdminWorkspaceMember(ctx context.Context, organizationUUID, workspaceUUID, workspaceExternalID, userUUID, userExternalID, role string) error {
	mapper := NewAdminWorkspaceMemberMapper(d.mapperDB)
	_, err := mapper.FindByUserExternalID(ctx, organizationUUID, workspaceExternalID, userExternalID)
	if err == nil {
		return nil
	}
	if !errors.Is(mapNoRows(err), platform.ErrNotFound) {
		return err
	}
	_, err = mapper.Insert(ctx, insertAdminWorkspaceMemberParams{
		ExternalID:          "member_dev_init_" + userExternalID + "_" + workspaceExternalID,
		OrganizationUUID:    organizationUUID,
		WorkspaceUUID:       workspaceUUID,
		WorkspaceExternalID: workspaceExternalID,
		UserUUID:            userUUID,
		UserExternalID:      userExternalID,
		WorkspaceRole:       role,
		CreatedAt:           time.Now(),
	})
	if isUniqueViolation(err) {
		return nil
	}
	return err
}

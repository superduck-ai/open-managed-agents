package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper AdminWorkspaceMemberMapper -sql ./admin_workspace_members_mapper.xml -out ./admin_workspace_members_mapper.sqlmap.gen.go -dialect postgres

type insertAdminWorkspaceMemberParams struct {
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	WorkspaceExternalID string
	UserUUID            string
	UserExternalID      string
	WorkspaceRole       string
	CreatedAt           time.Time
}

type updateAdminWorkspaceMemberRoleParams struct {
	OrganizationUUID    string
	WorkspaceExternalID string
	UserExternalID      string
	WorkspaceRole       string
}

type seedAdminWorkspaceMemberParams struct {
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	WorkspaceExternalID string
	UserUUID            string
	UserExternalID      string
}

type AdminWorkspaceMemberMapper interface {
	Insert(ctx context.Context, params insertAdminWorkspaceMemberParams) (AdminWorkspaceMember, error)
	FindByUserExternalID(ctx context.Context, organizationUUID,
		workspaceExternalID, userExternalID string) (AdminWorkspaceMember, error)
	FindPageAnchorByUserExternalID(ctx context.Context,
		workspaceUUID, userExternalID string) (pagePosition, bool, error)
	ListPage(ctx context.Context, organizationUUID, workspaceUUID string,
		anchor *pagePosition, before bool, limit int) ([]AdminWorkspaceMember, error)
	UpdateRoleByUserExternalID(ctx context.Context,
		params updateAdminWorkspaceMemberRoleParams) (AdminWorkspaceMember, error)
	SoftDeleteByUserExternalID(ctx context.Context, organizationUUID,
		workspaceExternalID, userExternalID string) (AdminWorkspaceMember, error)
	SeedDefault(ctx context.Context, params seedAdminWorkspaceMemberParams) error
}

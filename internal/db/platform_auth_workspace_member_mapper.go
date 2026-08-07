package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper PlatformAuthWorkspaceMemberMapper -sql ./platform_auth_workspace_member_mapper.xml -out ./platform_auth_workspace_member_mapper.sqlmap.gen.go -dialect postgres

type insertPlatformAuthWorkspaceMemberParams struct {
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	WorkspaceExternalID string
	UserUUID            string
	UserExternalID      string
	WorkspaceRole       string
}

type PlatformAuthWorkspaceMemberMapper interface {
	Insert(ctx context.Context, params insertPlatformAuthWorkspaceMemberParams) error
}

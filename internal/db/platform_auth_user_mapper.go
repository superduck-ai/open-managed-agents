package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper PlatformAuthUserMapper -sql ./platform_auth_user_mapper.xml -out ./platform_auth_user_mapper.sqlmap.gen.go -dialect postgres

type platformAuthUserContextRow struct {
	UserExternalID string `db:"user_external_id"`
	OrgUUID        string `db:"org_uuid"`
}

type platformSessionIdentityRow struct {
	OrganizationUUID    string `db:"organization_uuid"`
	WorkspaceUUID       string `db:"workspace_uuid"`
	WorkspaceExternalID string `db:"workspace_external_id"`
	UserUUID            string `db:"user_uuid"`
	UserExternalID      string `db:"user_external_id"`
}

type insertPlatformAuthUserParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	Email            string
	Name             string
	Role             string
}

type PlatformAuthUserMapper interface {
	FindContextByEmail(ctx context.Context, email string) (platformAuthUserContextRow, error)
	UpdateEmptyName(ctx context.Context, userExternalID, defaultName string) error
	Insert(ctx context.Context, params insertPlatformAuthUserParams) (string, error)
	ResolveSessionIdentity(ctx context.Context, organizationUUID, userID string, userUUID *string) (platformSessionIdentityRow, error)
}

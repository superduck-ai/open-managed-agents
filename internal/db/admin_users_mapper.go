package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper AdminUserMapper -sql ./admin_users_mapper.xml -out ./admin_users_mapper.sqlmap.gen.go -dialect postgres

type AdminUserMapper interface {
	FindByExternalID(ctx context.Context, organizationUUID, externalID string) (AdminUser, error)
	FindPageAnchorByExternalID(ctx context.Context, organizationUUID, externalID string) (pagePosition, bool, error)
	ListPage(ctx context.Context, organizationUUID, email string,
		anchor *pagePosition, before bool, limit int) ([]AdminUser, error)
	UpdateRoleByExternalID(ctx context.Context, organizationUUID, externalID, role string) (AdminUser, error)
	SoftDeleteByExternalID(ctx context.Context, organizationUUID, externalID string) (AdminUser, error)
	SoftDeleteWorkspaceMembersByUserUUID(ctx context.Context, organizationUUID, userUUID string) error
	SeedDefault(ctx context.Context, externalID, organizationUUID, email, name string) (string, error)
}

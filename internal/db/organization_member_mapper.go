package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper OrganizationMemberMapper -sql ./organization_member_mapper.xml -out ./organization_member_mapper.sqlmap.gen.go -dialect postgres

type OrganizationMemberMapper interface {
	LockOrganization(ctx context.Context, organizationUUID string) (string, error)
	FindRole(ctx context.Context, organizationUUID, userReference string) (string, error)
	FindMemberByReference(ctx context.Context, organizationUUID, userReference string) (AdminUser, error)
	CountAdmins(ctx context.Context, organizationUUID string) (int64, error)
}

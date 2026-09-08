package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper OrganizationMemberGuardMapper -sql ./organization_member_guard_mapper.xml -out ./organization_member_guard.sqlmap.gen.go -dialect postgres

type OrganizationMemberGuardMapper interface {
	LockOrganization(ctx context.Context, organizationUUID string) (string, error)
	FindRole(ctx context.Context, organizationUUID, userReference string) (string, error)
	CountAdmins(ctx context.Context, organizationUUID string) (int64, error)
}

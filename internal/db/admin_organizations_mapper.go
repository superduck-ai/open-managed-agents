package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper AdminOrganizationMapper -sql ./admin_organizations_mapper.xml -out ./admin_organizations_mapper.sqlmap.gen.go -dialect postgres

type AdminOrganizationMapper interface {
	FindByUUID(ctx context.Context, organizationUUID string) (AdminOrganization, error)
}

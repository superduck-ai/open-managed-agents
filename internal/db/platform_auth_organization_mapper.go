package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper PlatformAuthOrganizationMapper -sql ./platform_auth_organization_mapper.xml -out ./platform_auth_organization_mapper.sqlmap.gen.go -dialect postgres

type PlatformAuthOrganizationMapper interface {
	Insert(ctx context.Context, name string) (string, error)
}

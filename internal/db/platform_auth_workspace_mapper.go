package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper PlatformAuthWorkspaceMapper -sql ./platform_auth_workspace_mapper.xml -out ./platform_auth_workspace_mapper.sqlmap.gen.go -dialect postgres

type insertPlatformAuthWorkspaceParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	Name             string
	CompartmentID    string
}

type PlatformAuthWorkspaceMapper interface {
	Insert(ctx context.Context, params insertPlatformAuthWorkspaceParams) (string, error)
}

package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper PlatformAuthAPIKeyMapper -sql ./platform_auth_api_key_mapper.xml -out ./platform_auth_api_key_mapper.sqlmap.gen.go -dialect postgres

type insertPlatformAuthAPIKeyParams struct {
	ExternalID        string
	WorkspaceUUID     string
	KeyHash           string
	Status            string
	CreatedByUserUUID string
	Name              string
	PartialKeyHint    string
}

type PlatformAuthAPIKeyMapper interface {
	Insert(ctx context.Context, params insertPlatformAuthAPIKeyParams) error
}

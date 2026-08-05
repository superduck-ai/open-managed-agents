package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper WebhookWorkspaceMapper -sql ./webhook_workspace_mapper.xml -out ./webhook_workspace_mapper.sqlmap.gen.go -dialect postgres

type workspaceIdentifiersRow struct {
	OrganizationUUID    string `db:"organization_uuid"`
	WorkspaceExternalID string `db:"workspace_external_id"`
}

type WebhookWorkspaceMapper interface {
	FindIdentifiers(ctx context.Context, workspaceUUID string) (workspaceIdentifiersRow, error)
}

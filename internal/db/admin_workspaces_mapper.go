package db

import (
	"context"
	"encoding/json"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper AdminWorkspaceMapper -sql ./admin_workspaces_mapper.xml -out ./admin_workspaces_mapper.sqlmap.gen.go -dialect postgres

type insertAdminWorkspaceParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	Name             string
	CreatedAt        time.Time
	CompartmentID    string
	DisplayColor     string
	ExternalKeyID    *string
	Tags             json.RawMessage
}

type updateAdminWorkspaceParams struct {
	OrganizationUUID string
	ExternalID       string
	Name             string
	ExternalKeyID    *string
	Tags             json.RawMessage
	UpdatedAt        time.Time
}

type AdminWorkspaceMapper interface {
	Insert(ctx context.Context, params insertAdminWorkspaceParams) (AdminWorkspace, error)
	FindByIdentifier(ctx context.Context, organizationUUID, externalID string,
		workspaceUUID string) (AdminWorkspace, error)
	FindPageAnchorByExternalID(ctx context.Context, organizationUUID, externalID string) (pagePosition, bool, error)
	ListPage(ctx context.Context, organizationUUID string, includeArchived bool,
		anchor *pagePosition, before bool, limit int) ([]AdminWorkspace, error)
	UpdateByExternalID(ctx context.Context, params updateAdminWorkspaceParams) (AdminWorkspace, error)
	ArchiveByExternalID(ctx context.Context, organizationUUID, externalID string) (AdminWorkspace, error)
	CountByExternalKeyID(ctx context.Context, organizationUUID, externalID string) (int, error)
	SeedDefault(ctx context.Context, externalID, organizationUUID, name string) (string, error)
}

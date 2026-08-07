package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper AdminAPIKeyMapper -sql ./admin_api_keys_mapper.xml -out ./admin_api_keys_mapper.sqlmap.gen.go -dialect postgres

type adminAPIKeyPageAnchor struct {
	CreatedAt time.Time `db:"created_at"`
	UUID      string    `db:"uuid"`
}

type insertAdminAPIKeyParams struct {
	UUID              string
	ExternalID        string
	WorkspaceUUID     string
	KeyHash           string
	CreatedByUserUUID *string
	Name              string
	PartialKeyHint    string
	ExpiresAt         *time.Time
}

type seedAdminAPIKeyParams struct {
	ExternalID        string
	WorkspaceUUID     string
	KeyHash           string
	CreatedByUserUUID string
	Name              string
	PartialKeyHint    string
}

type apiKeyAuthRow struct {
	UUID                string `db:"uuid"`
	ExternalID          string `db:"external_id"`
	OrganizationUUID    string `db:"organization_uuid"`
	WorkspaceUUID       string `db:"workspace_uuid"`
	WorkspaceExternalID string `db:"workspace_external_id"`
}

type AdminAPIKeyMapper interface {
	FindByExternalID(ctx context.Context, organizationUUID string, externalID string) (AdminAPIKey, bool, error)
	FindPageAnchorByExternalID(ctx context.Context, organizationUUID string, externalID string) (adminAPIKeyPageAnchor, bool, error)
	ListPage(ctx context.Context, organizationUUID string,
		workspaceExternalID, createdByUserExternalID, status string,
		anchor *adminAPIKeyPageAnchor, before bool, limit int) ([]AdminAPIKey, error)
	Insert(ctx context.Context, params insertAdminAPIKeyParams) error
	UpdateByExternalID(ctx context.Context, organizationUUID string, externalID string,
		setName bool, name string, setStatus bool, status string) (int64, error)
	UpdateStatusByUUID(ctx context.Context, apiKeyUUID string, status string) (int64, error)
	SeedDefault(ctx context.Context, params seedAdminAPIKeyParams) error
	FindActiveByKeyHash(ctx context.Context, keyHash string) (apiKeyAuthRow, error)
}

package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper AdminAPIKeyMapper -sql ./admin_api_keys_mapper.xml -out ./admin_api_keys_mapper.sqlmap.gen.go -dialect postgres

type adminAPIKeyPageAnchor struct {
	CreatedAt time.Time `db:"created_at"`
	UUID      uuid.UUID `db:"uuid"`
}

type insertAdminAPIKeyParams struct {
	UUID              uuid.UUID
	ExternalID        string
	WorkspaceUUID     string
	KeyHash           string
	CreatedByUserUUID *string
	Name              string
	PartialKeyHint    string
	ExpiresAt         *time.Time
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
}

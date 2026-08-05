package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper AdminExternalKeyMapper -sql ./admin_external_keys_mapper.xml -out ./admin_external_keys_mapper.sqlmap.gen.go -dialect postgres

type insertAdminExternalKeyParams struct {
	ExternalID       string
	OrganizationUUID uuid.UUID
	DisplayName      string
	Geo              string
	ProviderConfig   json.RawMessage
	CreatedAt        time.Time
}

type updateAdminExternalKeyParams struct {
	OrganizationUUID string
	ExternalID       string
	DisplayName      string
	Geo              string
	ProviderConfig   json.RawMessage
	UpdatedAt        time.Time
}

type AdminExternalKeyMapper interface {
	Insert(ctx context.Context, params insertAdminExternalKeyParams) (AdminExternalKey, error)
	FindByExternalID(ctx context.Context, organizationUUID, externalID string) (AdminExternalKey, error)
	ListPage(ctx context.Context, organizationUUID string, limit, offset int) ([]AdminExternalKey, error)
	UpdateByExternalID(ctx context.Context, params updateAdminExternalKeyParams) (AdminExternalKey, error)
	SoftDeleteByExternalID(ctx context.Context, organizationUUID, externalID string) (int64, error)
}

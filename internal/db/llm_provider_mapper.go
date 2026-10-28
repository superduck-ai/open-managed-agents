package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper LLMProviderMapper -sql ./llm_provider_mapper.xml -out ./llm_provider_mapper.sqlmap.gen.go -dialect postgres

type llmProviderRow struct {
	Models           []byte    `db:"models"`
	UUID             string    `db:"uuid"`
	ExternalID       string    `db:"external_id"`
	OrganizationUUID string    `db:"organization_uuid"`
	WorkspaceUUID    *string   `db:"workspace_uuid"`
	Name             string    `db:"name"`
	Slug             string    `db:"slug"`
	APIBackend       string    `db:"api_backend"`
	BaseURL          string    `db:"base_url"`
	APIKeyLast4      string    `db:"api_key_last4"`
	Ciphertext       []byte    `db:"ciphertext"`
	Nonce            []byte    `db:"nonce"`
	WrappedDEK       []byte    `db:"wrapped_dek"`
	FormatVersion    int       `db:"format_version"`
	KeyProvider      string    `db:"key_provider"`
	KeyVersion       int64     `db:"key_version"`
	CreatedAt        time.Time `db:"created_at"`
	UpdatedAt        time.Time `db:"updated_at"`
}

type llmProviderWriteParams struct {
	Models           []byte
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    *string
	Name             string
	Slug             string
	APIBackend       string
	BaseURL          string
	APIKeyLast4      string
	Ciphertext       []byte
	Nonce            []byte
	WrappedDEK       []byte
	FormatVersion    int
	KeyProvider      string
	KeyVersion       int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type LLMProviderMapper interface {
	Insert(ctx context.Context, params llmProviderWriteParams) (llmProviderRow, error)
	ListByWorkspace(ctx context.Context, organizationUUID string, workspaceUUID *string) ([]llmProviderRow, error)
	FindByExternalID(ctx context.Context, organizationUUID string, workspaceUUID *string, externalID string) (llmProviderRow, error)
	ListByOrganization(ctx context.Context, organizationUUID string) ([]llmProviderRow, error)
	FindOrganizationDefault(ctx context.Context, organizationUUID string) (llmProviderRow, error)
	UpdateByExternalID(ctx context.Context, params llmProviderWriteParams) (llmProviderRow, error)
	UpdateOrganizationDefault(ctx context.Context, params llmProviderWriteParams) (llmProviderRow, error)
	DeleteByExternalID(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (int64, error)
	DeleteOrganizationDefault(ctx context.Context, organizationUUID, externalID string) (int64, error)
}

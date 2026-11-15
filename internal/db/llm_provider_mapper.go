package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper LLMProviderMapper -sql ./llm_provider_mapper.xml -out ./llm_provider_mapper.sqlmap.gen.go -dialect postgres

type llmProviderRow struct {
	Models               []byte    `db:"models"`
	UUID                 string    `db:"uuid"`
	ExternalID           string    `db:"external_id"`
	OrganizationUUID     string    `db:"organization_uuid"`
	SecretWorkspaceUUID  *string   `db:"secret_workspace_uuid"`
	CreatedByAccountUUID *string   `db:"created_by_account_uuid"`
	CreatedByName        *string   `db:"created_by_name"`
	Name                 string    `db:"name"`
	Slug                 string    `db:"slug"`
	APIBackend           string    `db:"api_backend"`
	BaseURL              string    `db:"base_url"`
	APIKeyLast4          string    `db:"api_key_last4"`
	Ciphertext           []byte    `db:"ciphertext"`
	Nonce                []byte    `db:"nonce"`
	WrappedDEK           []byte    `db:"wrapped_dek"`
	FormatVersion        int       `db:"format_version"`
	KeyProvider          string    `db:"key_provider"`
	KeyVersion           int64     `db:"key_version"`
	CreatedAt            time.Time `db:"created_at"`
	UpdatedAt            time.Time `db:"updated_at"`
}

type llmProviderWriteParams struct {
	Models               []byte
	UUID                 string
	ExternalID           string
	OrganizationUUID     string
	SecretWorkspaceUUID  *string
	CreatedByAccountUUID *string
	Name                 string
	Slug                 string
	APIBackend           string
	BaseURL              string
	APIKeyLast4          string
	Ciphertext           []byte
	Nonce                []byte
	WrappedDEK           []byte
	FormatVersion        int
	KeyProvider          string
	KeyVersion           int64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type LLMProviderMapper interface {
	Insert(ctx context.Context, params llmProviderWriteParams) (llmProviderRow, error)
	FindByExternalID(ctx context.Context, organizationUUID, externalID string) (llmProviderRow, error)
	ListByOrganization(ctx context.Context, organizationUUID string) ([]llmProviderRow, error)
	FindOrganizationDefault(ctx context.Context, organizationUUID string) (llmProviderRow, error)
	UpdateByExternalID(ctx context.Context, params llmProviderWriteParams) (llmProviderRow, error)
	DeleteByExternalID(ctx context.Context, organizationUUID, externalID string) (int64, error)
}

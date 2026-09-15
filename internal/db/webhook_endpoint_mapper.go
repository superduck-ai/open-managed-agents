package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper WebhookEndpointMapper -sql ./webhook_endpoint_mapper.xml -out ./webhook_endpoint_mapper.sqlmap.gen.go -dialect postgres

type webhookEndpointRow struct {
	UUID                string         `db:"uuid"`
	ExternalID          string         `db:"external_id"`
	OrganizationUUID    string         `db:"organization_uuid"`
	WorkspaceUUID       string         `db:"workspace_uuid"`
	CreatedByAPIKeyUUID *string        `db:"created_by_api_key_uuid"`
	URL                 string         `db:"url"`
	Name                string         `db:"name"`
	Description         string         `db:"description"`
	EnabledEvents       []byte         `db:"enabled_events"`
	SigningSecret       string         `db:"signing_secret"`
	Status              string         `db:"status"`
	DisabledReason      sql.NullString `db:"disabled_reason"`
	ConsecutiveFailures int            `db:"consecutive_failures"`
	CreatedAt           time.Time      `db:"created_at"`
	UpdatedAt           time.Time      `db:"updated_at"`
	DeletedAt           *time.Time     `db:"deleted_at"`
}

type insertWebhookEndpointParams struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID *string
	URL                 string
	Name                string
	Description         string
	EnabledEvents       json.RawMessage
	SigningSecret       string
	Status              string
	DisabledReason      *string
	ConsecutiveFailures int
	CreatedAt           time.Time
}

type updateWebhookEndpointParams struct {
	WorkspaceUUID       string
	ExternalID          string
	URL                 string
	Name                string
	Description         string
	EnabledEvents       json.RawMessage
	Status              string
	DisabledReason      *string
	ConsecutiveFailures int
	UpdatedAt           time.Time
}

type regenerateWebhookEndpointSecretParams struct {
	WorkspaceUUID string
	ExternalID    string
	SigningSecret string
	UpdatedAt     time.Time
}

type recordWebhookEndpointFailureParams struct {
	EndpointUUID string
	DisableAfter int
	Reason       string
}

type WebhookEndpointMapper interface {
	Insert(ctx context.Context, params insertWebhookEndpointParams) (webhookEndpointRow, error)
	List(ctx context.Context, workspaceUUID string) ([]webhookEndpointRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, externalID string) (webhookEndpointRow, error)
	UpdateByExternalID(ctx context.Context, params updateWebhookEndpointParams) (webhookEndpointRow, error)
	UpdateSigningSecret(ctx context.Context, params regenerateWebhookEndpointSecretParams) (int64, error)
	SoftDeleteByExternalID(ctx context.Context, workspaceUUID, externalID string) (int64, error)
	Exists(ctx context.Context, workspaceUUID string) (bool, error)
	ListActiveForEvent(ctx context.Context, workspaceUUID, eventType string) ([]webhookEndpointRow, error)
	RecordDeliverySuccess(ctx context.Context, endpointUUID string) error
	RecordDeliveryFailure(ctx context.Context, params recordWebhookEndpointFailureParams) error
}

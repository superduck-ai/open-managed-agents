package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper TelemetryDestinationMapper -sql ./telemetry_destination_mapper.xml -out ./telemetry_destination_mapper.sqlmap.gen.go -dialect postgres

type telemetryDestinationRow struct {
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationUUID      string     `db:"organization_uuid"`
	DisplayName           *string    `db:"display_name"`
	EndpointURL           string     `db:"endpoint_url"`
	TracesEnabled         bool       `db:"traces_enabled"`
	TracesEndpointURL     string     `db:"traces_endpoint_url"`
	TracesContentMode     string     `db:"traces_content_mode"`
	TracesMaxContentBytes int        `db:"traces_max_content_bytes"`
	WorkspaceScope        string     `db:"workspace_scope"`
	HeaderNames           []byte     `db:"header_names"`
	HeadersCiphertext     []byte     `db:"headers_ciphertext"`
	HeadersNonce          []byte     `db:"headers_nonce"`
	HeadersWrappedDEK     []byte     `db:"headers_wrapped_dek"`
	HeadersFormatVersion  *int       `db:"headers_format_version"`
	HeadersKeyProvider    *string    `db:"headers_key_provider"`
	HeadersKeyVersion     *int64     `db:"headers_key_version"`
	State                 string     `db:"state"`
	DisabledReason        *string    `db:"disabled_reason"`
	FailureStartedAt      *time.Time `db:"failure_started_at"`
	LastError             *string    `db:"last_error"`
	LastErrorAt           *time.Time `db:"last_error_at"`
	LastSuccessAt         *time.Time `db:"last_success_at"`
	LastTestAt            *time.Time `db:"last_test_at"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
}

type telemetryDestinationWorkspaceRow struct {
	DestinationUUID string `db:"destination_uuid"`
	WorkspaceUUID   string `db:"workspace_uuid"`
}

type telemetryWorkspaceIdentityRow struct {
	UUID       string `db:"uuid"`
	ExternalID string `db:"external_id"`
}

type telemetryDestinationWriteParams struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	DisplayName           *string
	EndpointURL           string
	TracesEnabled         bool
	TracesEndpointURL     string
	TracesContentMode     string
	TracesMaxContentBytes int
	WorkspaceScope        string
	HeaderNames           []byte
	HeadersCiphertext     []byte
	HeadersNonce          []byte
	HeadersWrappedDEK     []byte
	HeadersFormatVersion  *int
	HeadersKeyProvider    *string
	HeadersKeyVersion     *int64
	State                 string
	DisabledReason        *string
	ExpectedUpdatedAt     time.Time
	Now                   time.Time
}

type telemetryDeliveryFailureParams struct {
	DestinationUUID     string
	Error               string
	ImmediateReason     string
	DisableAfterSeconds int64
	Now                 time.Time
}

type TelemetryDestinationMapper interface {
	LockOrganization(ctx context.Context, organizationUUID string) (string, error)
	CountActive(ctx context.Context, organizationUUID string) (int64, error)
	Insert(ctx context.Context, params telemetryDestinationWriteParams) (telemetryDestinationRow, error)
	Update(ctx context.Context, params telemetryDestinationWriteParams) (telemetryDestinationRow, error)
	FindByExternalID(ctx context.Context, organizationUUID, externalID string) (telemetryDestinationRow, bool, error)
	FindByUUID(ctx context.Context, destinationUUID string) (telemetryDestinationRow, bool, error)
	ListPage(ctx context.Context, organizationUUID string, workspaceUUIDs []string, limit, offset int) ([]telemetryDestinationRow, error)
	ListWorkspaces(ctx context.Context, organizationUUID string, destinationUUIDs []string) ([]telemetryDestinationWorkspaceRow, error)
	DeleteWorkspaces(ctx context.Context, organizationUUID, destinationUUID string) error
	InsertWorkspace(ctx context.Context, organizationUUID, destinationUUID, workspaceUUID string) error
	SoftDelete(ctx context.Context, organizationUUID, externalID string, now time.Time) (int64, error)
	ListActiveForWorkspace(ctx context.Context, organizationUUID, workspaceUUID string) ([]telemetryDestinationRow, error)
	MarkTested(ctx context.Context, organizationUUID, externalID string, now, notAfter time.Time) (int64, error)
	RecordDeliverySuccess(ctx context.Context, destinationUUID string, now time.Time) error
	RecordDeliveryFailure(ctx context.Context, params telemetryDeliveryFailureParams) (bool, error)
	ResolveWorkspaces(ctx context.Context, organizationUUID string, externalIDs []string) ([]telemetryWorkspaceIdentityRow, error)
	WorkspaceIdentities(ctx context.Context, organizationUUID string, workspaceUUIDs []string) ([]telemetryWorkspaceIdentityRow, error)
}

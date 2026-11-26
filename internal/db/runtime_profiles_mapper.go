package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper RuntimeProfileMapper -sql ./runtime_profiles_mapper.xml -out ./runtime_profiles_mapper.sqlmap.gen.go -dialect postgres

type upsertRuntimeProfileParams struct {
	UUID                string
	ExternalID          string
	Scope               string
	OrganizationUUID    *string
	Name                string
	ProviderKind        string
	EndpointURL         string
	IsolationMode       string
	Config              string
	CapabilityOverrides string
	SecretEnvelope      *string
	CredentialHint      string
	CredentialRevision  int64
	Status              string
	Version             int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type updateRuntimeProfileHealthParams struct {
	UUID                string
	LastHealthStatus    string
	LastHealthError     string
	LastHealthCheckedAt time.Time
}

type updateOrganizationRuntimePolicyParams struct {
	OrganizationUUID               string
	DefaultRuntimeProfileUUID      string
	AllowedRuntimeProfileUUIDsJSON string
}

type runtimeProfileRow struct {
	ID                  int64           `db:"id"`
	UUID                string          `db:"uuid"`
	ExternalID          string          `db:"external_id"`
	Scope               string          `db:"scope"`
	OrganizationUUID    sql.NullString  `db:"organization_uuid"`
	Name                string          `db:"name"`
	ProviderKind        string          `db:"provider_kind"`
	EndpointURL         string          `db:"endpoint_url"`
	IsolationMode       string          `db:"isolation_mode"`
	Config              json.RawMessage `db:"config"`
	CapabilityOverrides json.RawMessage `db:"capability_overrides"`
	SecretEnvelope      sql.NullString  `db:"secret_envelope"`
	CredentialHint      string          `db:"credential_hint"`
	CredentialRevision  int64           `db:"credential_revision"`
	Status              string          `db:"status"`
	LastHealthStatus    string          `db:"last_health_status"`
	LastHealthCheckedAt *time.Time      `db:"last_health_checked_at"`
	LastHealthError     string          `db:"last_health_error"`
	Version             int64           `db:"version"`
	CreatedAt           time.Time       `db:"created_at"`
	UpdatedAt           time.Time       `db:"updated_at"`
}

type organizationRuntimePolicyRow struct {
	OrganizationUUID               string `db:"organization_uuid"`
	DefaultRuntimeProfileUUID      string `db:"default_runtime_profile_uuid"`
	AllowedRuntimeProfileUUIDsJSON string `db:"allowed_runtime_profile_uuids_json"`
}

type RuntimeProfileMapper interface {
	ListPlatform(ctx context.Context) ([]runtimeProfileRow, error)
	FindByUUID(ctx context.Context, profileUUID string) (runtimeProfileRow, error)
	FindByExternalID(ctx context.Context, externalID string) (runtimeProfileRow, error)
	Insert(ctx context.Context, params upsertRuntimeProfileParams) (runtimeProfileRow, error)
	UpdateByUUID(ctx context.Context, params upsertRuntimeProfileParams) (runtimeProfileRow, error)
	UpdateHealthByUUID(ctx context.Context, params updateRuntimeProfileHealthParams) (int64, error)
	DeleteByUUID(ctx context.Context, profileUUID string, expectedVersion int64) (int64, error)
	FindOrganizationPolicy(ctx context.Context, organizationUUID string) (organizationRuntimePolicyRow, error)
	UpdateOrganizationPolicy(ctx context.Context, params updateOrganizationRuntimePolicyParams) (organizationRuntimePolicyRow, error)
	FindByWorkspace(ctx context.Context, organizationUUID string, workspaceUUID string) (runtimeProfileRow, error)
}

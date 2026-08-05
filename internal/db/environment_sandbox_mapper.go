package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EnvironmentSandboxMapper -sql ./environment_sandbox_mapper.xml -out ./environment_sandbox_mapper.sqlmap.gen.go -dialect postgres

type environmentSandboxMapperRow struct {
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationUUID      string     `db:"organization_uuid"`
	WorkspaceUUID         string     `db:"workspace_uuid"`
	EnvironmentUUID       string     `db:"environment_uuid"`
	EnvironmentExternalID string     `db:"environment_external_id"`
	WorkUUID              *string    `db:"work_uuid"`
	WorkExternalID        *string    `db:"work_external_id"`
	Provider              string     `db:"provider"`
	Template              string     `db:"template"`
	ProviderSandboxID     *string    `db:"provider_sandbox_id"`
	State                 string     `db:"state"`
	Metadata              []byte     `db:"metadata"`
	LastError             *string    `db:"last_error"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
	StoppedAt             *time.Time `db:"stopped_at"`
}

type environmentSandboxWriteParams struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	EnvironmentUUID       string
	EnvironmentExternalID string
	WorkUUID              *string
	WorkExternalID        *string
	Provider              string
	Template              string
	ProviderSandboxID     *string
	State                 string
	Metadata              []byte
	LastError             *string
	CreatedAt             time.Time
}

type environmentSandboxStateParams struct {
	WorkspaceUUID     string
	ExternalID        string
	State             string
	ProviderSandboxID *string
	LastError         *string
	StoppedAt         *time.Time
}

type EnvironmentSandboxMapper interface {
	Insert(ctx context.Context, params environmentSandboxWriteParams) (environmentSandboxMapperRow, error)
	UpdateState(ctx context.Context, params environmentSandboxStateParams) error
	FindActiveForWork(ctx context.Context, workspaceUUID, environmentExternalID, workExternalID string) (environmentSandboxMapperRow, error)
	FindRenewableByCodeSessionExternalID(ctx context.Context, codeSessionExternalID string) (environmentSandboxMapperRow, error)
}

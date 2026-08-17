package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper DreamMapper -sql ./dream_mapper.xml -out ./dream_mapper.sqlmap.gen.go -dialect postgres

type dreamRow struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	OrganizationUUID    string     `db:"organization_uuid"`
	WorkspaceUUID       string     `db:"workspace_uuid"`
	CreatedByAPIKeyUUID string     `db:"created_by_api_key_uuid"`
	InputStoreUUID      string     `db:"input_store_uuid"`
	SessionIDs          []byte     `db:"session_ids"`
	Instructions        *string    `db:"instructions"`
	Model               string     `db:"model"`
	Status              string     `db:"status"`
	OutputStoreUUID     *string    `db:"output_store_uuid"`
	Error               *string    `db:"error"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	ArchivedAt          *time.Time `db:"archived_at"`
}

type insertDreamParams struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID string
	InputStoreUUID      string
	SessionIDs          []byte
	Instructions        *string
	Model               string
	Status              string
	CreatedAt           time.Time
}

type updateDreamStatusParams struct {
	WorkspaceUUID string
	ExternalID    string
	Status        string
}

type setDreamOutputStoreParams struct {
	WorkspaceUUID   string
	ExternalID      string
	OutputStoreUUID string
}

type setDreamErrorParams struct {
	WorkspaceUUID string
	ExternalID    string
	Error         string
}

type listDreamsParams struct {
	WorkspaceUUID   string
	Limit           int
	HasCursor       bool
	CursorCreatedAt time.Time
	CursorUUID      string
}

// DreamMapper contains queries whose primary table is dreams.
type DreamMapper interface {
	Insert(ctx context.Context, params insertDreamParams) (dreamRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, externalID string) (dreamRow, error)
	FindForUpdate(ctx context.Context, workspaceUUID, externalID string) (dreamRow, error)
	ListPage(ctx context.Context, params listDreamsParams) ([]dreamRow, error)
	UpdateStatus(ctx context.Context, params updateDreamStatusParams) (int64, error)
	SetOutputStore(ctx context.Context, params setDreamOutputStoreParams) (int64, error)
	SetError(ctx context.Context, params setDreamErrorParams) (int64, error)
	ArchiveByExternalID(ctx context.Context, workspaceUUID, externalID string) (int64, error)
}

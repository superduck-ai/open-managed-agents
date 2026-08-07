package db

import (
	"context"
	"database/sql"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper WorkbenchPromptMapper -sql ./workbench_prompt_mapper.xml -out ./workbench_prompt_mapper.sqlmap.gen.go -dialect postgres

type workbenchPromptRow struct {
	OrgUUID               string         `db:"organization_uuid"`
	PromptUUID            string         `db:"prompt_uuid"`
	WorkspaceUUID         string         `db:"workspace_uuid"`
	WorkspaceDisplayID    string         `db:"workspace_display_id"`
	Name                  string         `db:"name"`
	IsSharedWithWorkspace bool           `db:"is_shared_with_workspace"`
	LatestRevisionUUID    sql.NullString `db:"latest_revision_uuid"`
	DeletedAt             sql.NullTime   `db:"deleted_at"`
	CreatedAt             time.Time      `db:"created_at"`
	UpdatedAt             time.Time      `db:"updated_at"`
}

type upsertWorkbenchPromptParams struct {
	OrganizationUUID      string
	PromptUUID            string
	WorkspaceUUID         string
	WorkspaceDisplayID    string
	Name                  string
	IsSharedWithWorkspace bool
	LatestRevisionUUID    *string
	DeletedAt             *time.Time
	CreatedAt             time.Time
}

type resetWorkbenchPromptParams struct {
	OrganizationUUID   string
	PromptUUID         string
	WorkspaceUUID      string
	WorkspaceDisplayID string
}

type WorkbenchPromptMapper interface {
	FindByPromptUUID(ctx context.Context, organizationUUID, promptUUID string) (workbenchPromptRow, error)
	ListByWorkspace(ctx context.Context, organizationUUID, workspaceUUID string) ([]workbenchPromptRow, error)
	Upsert(ctx context.Context, params upsertWorkbenchPromptParams) (workbenchPromptRow, error)
	ResetAndReturnUUID(ctx context.Context, params resetWorkbenchPromptParams) (string, error)
}

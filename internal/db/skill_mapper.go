package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SkillMapper -sql ./skill_mapper.xml -out ./skill_mapper.sqlmap.gen.go -dialect postgres

type skillRow struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	WorkspaceUUID       string     `db:"workspace_uuid"`
	CreatedByAPIKeyUUID string     `db:"created_by_api_key_uuid"`
	DisplayTitle        *string    `db:"display_title"`
	LatestVersion       *string    `db:"latest_version"`
	Source              string     `db:"source"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
}

type insertSkillParams struct {
	UUID                string
	ExternalID          string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID string
	DisplayTitle        *string
	LatestVersion       string
	CreatedAt           time.Time
}

type updateSkillLatestVersionParams struct {
	WorkspaceUUID string
	ExternalID    string
	LatestVersion string
	UpdatedAt     time.Time
}

type SkillMapper interface {
	FindExternalIDByDisplayTitle(ctx context.Context, workspaceUUID, displayTitle string) (string, bool, error)
	Insert(ctx context.Context, params insertSkillParams) (skillRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, externalID string) (skillRow, error)
	FindForUpdateByExternalID(ctx context.Context, workspaceUUID, externalID string) (skillRow, error)
	ListPage(ctx context.Context, workspaceUUID string, limit, offset int) ([]skillRow, error)
	FindUUIDByExternalID(ctx context.Context, workspaceUUID, externalID string) (string, error)
	UpdateLatestVersionByExternalID(ctx context.Context, params updateSkillLatestVersionParams) (skillRow, error)
	SoftDeleteByUUID(ctx context.Context, workspaceUUID, skillUUID string) (skillRow, error)
	UpdateLatestVersionByUUID(ctx context.Context, workspaceUUID, skillUUID string, latestVersion *string) error
}

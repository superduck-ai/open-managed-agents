package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper ConsoleWorkspaceMapper -sql ./console_workspaces_mapper.xml -out ./console_workspaces_mapper.sqlmap.gen.go -dialect postgres

type upsertConsoleWorkspaceParams struct {
	UUID         string
	ExternalID   string
	OrgUUID      string
	Name         string
	DisplayColor string
}

type consoleWorkspaceRow struct {
	UUID          string     `db:"uuid"`
	ExternalID    string     `db:"external_id"`
	OrgUUID       string     `db:"org_uuid"`
	Name          string     `db:"name"`
	DisplayColor  string     `db:"display_color"`
	Color         string     `db:"color"`
	ExternalKeyID *string    `db:"external_key_id"`
	Tags          []byte     `db:"tags"`
	ArchivedAt    *time.Time `db:"archived_at"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
}

type ConsoleWorkspaceMapper interface {
	Upsert(ctx context.Context, params upsertConsoleWorkspaceParams) (consoleWorkspaceRow, error)
	List(ctx context.Context, orgUUID string, includeArchived bool) ([]consoleWorkspaceRow, error)
}

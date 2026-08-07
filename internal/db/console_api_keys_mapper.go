package db

import (
	"context"
	"database/sql"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper ConsoleAPIKeyMapper -sql ./console_api_keys_mapper.xml -out ./console_api_keys_mapper.sqlmap.gen.go -dialect postgres

type insertConsoleAPIKeyQuery struct {
	ExternalID         string
	APIKeyUUID         string
	OrganizationUUID   string
	WorkspaceUUID      string
	WorkspaceDisplayID string
	Name               string
	KeyPrefix          string
	KeySuffix          string
	KeyHash            string
	CreatedByUserUUID  *string
	ExpiresAt          *time.Time
}

type updateConsoleAPIKeyStatusQuery struct {
	OrganizationUUID string
	WorkspaceUUID    string
	ExternalID       string
	Status           string
}

type consoleAPIKeyRow struct {
	ID                  string         `db:"id"`
	WorkspaceAPIKeyUUID string         `db:"workspace_api_key_uuid"`
	OrgUUID             string         `db:"org_uuid"`
	WorkspaceUUID       string         `db:"workspace_uuid"`
	WorkspaceDisplayID  string         `db:"workspace_display_id"`
	Name                string         `db:"name"`
	KeyPrefix           string         `db:"key_prefix"`
	KeySuffix           string         `db:"key_suffix"`
	Status              string         `db:"status"`
	CreatedByUserUUID   sql.NullString `db:"created_by_user_uuid"`
	LastUsedAt          *time.Time     `db:"last_used_at"`
	ExpiresAt           *time.Time     `db:"expires_at"`
	ArchivedAt          *time.Time     `db:"archived_at"`
	CreatedAt           time.Time      `db:"created_at"`
	UpdatedAt           time.Time      `db:"updated_at"`
}

type ConsoleAPIKeyMapper interface {
	List(ctx context.Context, organizationUUID, workspaceUUID string) ([]consoleAPIKeyRow, error)
	CountUnarchived(ctx context.Context, organizationUUID, workspaceUUID string) (int64, error)
	Insert(ctx context.Context, params insertConsoleAPIKeyQuery) (consoleAPIKeyRow, error)
	UpdateStatus(ctx context.Context, params updateConsoleAPIKeyStatusQuery) (consoleAPIKeyRow, error)
}

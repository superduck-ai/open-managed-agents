package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SkillVersionMapper -sql ./skill_version_mapper.xml -out ./skill_version_mapper.sqlmap.gen.go -dialect postgres

type skillVersionRow struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	WorkspaceUUID       string     `db:"workspace_uuid"`
	SkillUUID           string     `db:"skill_uuid"`
	SkillExternalID     string     `db:"skill_external_id"`
	Version             string     `db:"version"`
	Name                string     `db:"name"`
	Description         string     `db:"description"`
	Directory           string     `db:"directory"`
	S3Bucket            string     `db:"s3_bucket"`
	S3Key               string     `db:"s3_key"`
	SizeBytes           int64      `db:"size_bytes"`
	SHA256              string     `db:"sha256"`
	CreatedByAPIKeyUUID string     `db:"created_by_api_key_uuid"`
	CreatedAt           time.Time  `db:"created_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
}

type insertSkillVersionParams struct {
	UUID                string
	ExternalID          string
	WorkspaceUUID       string
	SkillUUID           string
	SkillExternalID     string
	Version             string
	Name                string
	Description         string
	Directory           string
	S3Bucket            string
	S3Key               string
	SizeBytes           int64
	SHA256              string
	CreatedByAPIKeyUUID string
	CreatedAt           time.Time
}

type sessionSkillArchiveValidationParams struct {
	Source        string
	WorkspaceUUID string
	VersionUUID   string
	Directory     string
	S3Bucket      string
	S3Key         string
	SizeBytes     int64
	SHA256        string
}

type SkillVersionMapper interface {
	ValidateSkillArchiveVersion(ctx context.Context, params sessionSkillArchiveValidationParams) (bool, error)
	Insert(ctx context.Context, params insertSkillVersionParams) (skillVersionRow, error)
	Find(ctx context.Context, workspaceUUID, skillExternalID, version string) (skillVersionRow, error)
	FindLatest(ctx context.Context, workspaceUUID, skillExternalID string) (skillVersionRow, error)
	ListPageBySkillUUID(ctx context.Context, workspaceUUID, skillUUID string, limit, offset int) ([]skillVersionRow, error)
	ListBySkillUUID(ctx context.Context, workspaceUUID, skillUUID string) ([]skillVersionRow, error)
	SoftDeleteBySkillUUID(ctx context.Context, workspaceUUID, skillUUID string) error
	SoftDeleteByVersion(ctx context.Context, workspaceUUID, skillUUID, version string) (skillVersionRow, error)
	FindLatestVersion(ctx context.Context, workspaceUUID, skillUUID string) (string, bool, error)
}

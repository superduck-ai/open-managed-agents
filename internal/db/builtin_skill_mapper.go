package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper BuiltinSkillMapper -sql ./builtin_skill_mapper.xml -out ./builtin_skill_mapper.sqlmap.gen.go -dialect postgres

type builtinSkillRow struct {
	ID            int64      `db:"id"`
	UUID          string     `db:"uuid"`
	ExternalID    string     `db:"external_id"`
	DisplayTitle  string     `db:"display_title"`
	LatestVersion *string    `db:"latest_version"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
	DeletedAt     *time.Time `db:"deleted_at"`
}

type builtinSkillVersionRow struct {
	ID              int64      `db:"id"`
	UUID            string     `db:"uuid"`
	ExternalID      string     `db:"external_id"`
	SkillID         int64      `db:"skill_id"`
	SkillExternalID string     `db:"skill_external_id"`
	Version         string     `db:"version"`
	Name            string     `db:"name"`
	Description     string     `db:"description"`
	Directory       string     `db:"directory"`
	S3Bucket        string     `db:"s3_bucket"`
	S3Key           string     `db:"s3_key"`
	SizeBytes       int64      `db:"size_bytes"`
	SHA256          string     `db:"sha256"`
	CreatedAt       time.Time  `db:"created_at"`
	DeletedAt       *time.Time `db:"deleted_at"`
}

type upsertBuiltinSkillParams struct {
	ExternalID    string
	DisplayTitle  string
	LatestVersion string
	CreatedAt     time.Time
}

type upsertBuiltinSkillVersionParams struct {
	ExternalID      string
	SkillID         int64
	SkillExternalID string
	Version         string
	Name            string
	Description     string
	Directory       string
	S3Bucket        string
	S3Key           string
	SizeBytes       int64
	SHA256          string
	CreatedAt       time.Time
}

type pruneBuiltinSkillsParams struct {
	KeepExternalIDsJSON []byte
	DeletedAt           time.Time
}

type BuiltinSkillMapper interface {
	UpsertSkill(ctx context.Context, params upsertBuiltinSkillParams) (builtinSkillRow, error)
	UpsertVersion(ctx context.Context, params upsertBuiltinSkillVersionParams) (builtinSkillVersionRow, error)
	ListSkillsPage(ctx context.Context, limit, offset int) ([]builtinSkillRow, error)
	CountSkills(ctx context.Context) (int, error)
	FindSkillByExternalID(ctx context.Context, externalID string) (builtinSkillRow, error)
	FindSkillIDByExternalID(ctx context.Context, externalID string) (int64, error)
	ListVersionsPage(ctx context.Context, skillID int64, limit, offset int) ([]builtinSkillVersionRow, error)
	FindVersion(ctx context.Context, skillExternalID, version string) (builtinSkillVersionRow, error)
	ListMissingVersions(ctx context.Context, keepExternalIDsJSON []byte) ([]builtinSkillVersionRow, error)
	SoftDeleteMissingVersions(ctx context.Context, params pruneBuiltinSkillsParams) error
	SoftDeleteMissingSkills(ctx context.Context, params pruneBuiltinSkillsParams) error
}

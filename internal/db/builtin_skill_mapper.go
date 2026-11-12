package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper BuiltinSkillMapper -sql ./builtin_skill_mapper.xml -out ./builtin_skill_mapper.sqlmap.gen.go -dialect postgres

type builtinSkillRow struct {
	IconKey       string     `db:"icon_key"`
	Listed        bool       `db:"listed"`
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
	SkillUUID       string     `db:"skill_uuid"`
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

type BuiltinSkillMapper interface {
	ListSkillsPage(ctx context.Context, limit, offset int) ([]builtinSkillRow, error)
	CountSkills(ctx context.Context) (int, error)
	FindSkillByExternalID(ctx context.Context, externalID string) (builtinSkillRow, error)
	FindSkillUUIDByExternalID(ctx context.Context, externalID string) (string, error)
	ListVersionsPage(ctx context.Context, skillUUID string, limit, offset int, oldestFirst bool) ([]builtinSkillVersionRow, error)
	FindVersion(ctx context.Context, skillExternalID, version string) (builtinSkillVersionRow, error)
}

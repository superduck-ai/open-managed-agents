package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SystemSkillMapper -sql ./system_skill_mapper.xml -out ./system_skill_mapper.sqlmap.gen.go -dialect postgres

type SystemSkill struct {
	BundledVersion *string    `db:"bundled_version"`
	Bundled        bool       `db:"bundled"`
	IconKey        string     `db:"icon_key"`
	ID             int64      `db:"id"`
	UUID           string     `db:"uuid"`
	ExternalID     string     `db:"external_id"`
	DisplayTitle   string     `db:"display_title"`
	LatestVersion  *string    `db:"latest_version"`
	Listed         bool       `db:"listed"`
	CreatedAt      time.Time  `db:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at"`
	DeletedAt      *time.Time `db:"deleted_at"`
}

type SystemSkillVersion struct {
	ID              int64      `db:"id"`
	UUID            string     `db:"uuid"`
	ExternalID      string     `db:"external_id"`
	SkillUUID       string     `db:"skill_uuid"`
	SkillExternalID string     `db:"skill_external_id"`
	Version         string     `db:"version"`
	Name            string     `db:"name"`
	Description     string     `db:"description"`
	Directory       string     `db:"directory"`
	State           string     `db:"state"`
	S3Bucket        string     `db:"s3_bucket"`
	S3Key           string     `db:"s3_key"`
	SizeBytes       int64      `db:"size_bytes"`
	SHA256          string     `db:"sha256"`
	CreatedAt       time.Time  `db:"created_at"`
	DeletedAt       *time.Time `db:"deleted_at"`
}

type SystemSkillQuery struct {
	Query  string
	Status string
	Limit  int
	Offset int
}

type SystemSkillMapper interface {
	List(ctx context.Context, query SystemSkillQuery) ([]SystemSkill, error)
	ListListed(ctx context.Context, limit, offset int) ([]SystemSkill, error)
	CountListed(ctx context.Context) (int, error)
	Find(ctx context.Context, externalID string, lock bool) (SystemSkill, error)
	FindAvailable(ctx context.Context, externalID string) (SystemSkill, error)
	FindUUIDByExternalID(ctx context.Context, externalID string) (string, error)
	InsertSkill(ctx context.Context, skill SystemSkill) (int64, error)
	RegisterBundled(ctx context.Context, skill SystemSkill) (SystemSkill, error)
	UpdateSkill(ctx context.Context, skill SystemSkill) (int64, error)
	DeleteSkill(ctx context.Context, skill SystemSkill) (int64, error)
	Versions(ctx context.Context, skillUUID string, limit, offset int, oldestFirst bool) ([]SystemSkillVersion, error)
	VersionBySHA256(ctx context.Context, skillUUID, sha256 string) (SystemSkillVersion, error)
	Version(ctx context.Context, skillUUID, version string) (SystemSkillVersion, error)
	FindVersionByExternalID(ctx context.Context, skillExternalID, version string) (SystemSkillVersion, error)
	InsertVersion(ctx context.Context, version SystemSkillVersion) error
	HasArchive(ctx context.Context, key string) (bool, error)
}

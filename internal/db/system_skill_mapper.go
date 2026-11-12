package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SystemSkillMapper -sql ./system_skill_mapper.xml -out ./system_skill_mapper.sqlmap.gen.go -dialect postgres

type SystemSkill struct {
	BundledVersion *string   `db:"bundled_version"`
	Bundled        bool      `db:"bundled"`
	IconKey        string    `db:"icon_key"`
	UUID           string    `db:"uuid"`
	ExternalID     string    `db:"external_id"`
	DisplayTitle   string    `db:"display_title"`
	LatestVersion  *string   `db:"latest_version"`
	Listed         bool      `db:"listed"`
	Revision       int64     `db:"revision"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

type SystemSkillVersion struct {
	UUID            string    `db:"uuid"`
	ExternalID      string    `db:"external_id"`
	SkillUUID       string    `db:"skill_uuid"`
	SkillExternalID string    `db:"skill_external_id"`
	Version         string    `db:"version"`
	Name            string    `db:"name"`
	Description     string    `db:"description"`
	Directory       string    `db:"directory"`
	State           string    `db:"state"`
	S3Bucket        string    `db:"s3_bucket"`
	S3Key           string    `db:"s3_key"`
	SizeBytes       int64     `db:"size_bytes"`
	SHA256          string    `db:"sha256"`
	CreatedAt       time.Time `db:"created_at"`
}

type SystemSkillEvent struct {
	ActorID   string    `db:"actor_id"`
	ActorName string    `db:"actor_name"`
	UUID      string    `db:"uuid"`
	SkillUUID string    `db:"skill_uuid"`
	ActorUUID string    `db:"actor_uuid"`
	Action    string    `db:"action"`
	Version   string    `db:"version"`
	CreatedAt time.Time `db:"created_at"`
}

type SystemSkillQuery struct {
	Query  string
	Status string
	Limit  int
	Offset int
}

type SystemSkillMapper interface {
	List(ctx context.Context, query SystemSkillQuery) ([]SystemSkill, error)
	Find(ctx context.Context, externalID string, lock bool) (SystemSkill, error)
	InsertSkill(ctx context.Context, skill SystemSkill) (int64, error)
	RegisterBundled(ctx context.Context, skill SystemSkill) (SystemSkill, error)
	UpdateSkill(ctx context.Context, skill SystemSkill) (int64, error)
	DeleteSkill(ctx context.Context, skill SystemSkill) (int64, error)
	Versions(ctx context.Context, skillUUID string, limit, offset int) ([]SystemSkillVersion, error)
	Version(ctx context.Context, skillUUID, version string) (SystemSkillVersion, error)
	InsertVersion(ctx context.Context, version SystemSkillVersion) error
	Events(ctx context.Context, skillUUID string, limit, offset int) ([]SystemSkillEvent, error)
	Record(ctx context.Context, event SystemSkillEvent) error
	HasArchive(ctx context.Context, key string) (bool, error)
}

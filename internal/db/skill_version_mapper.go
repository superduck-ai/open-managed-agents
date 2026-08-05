package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper SkillVersionMapper -sql ./skill_version_mapper.xml -out ./skill_version_mapper.sqlmap.gen.go -dialect postgres

type SkillVersionMapper interface {
	ValidateSkillArchiveVersion(ctx context.Context, params sessionSkillArchiveValidationParams) (bool, error)
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

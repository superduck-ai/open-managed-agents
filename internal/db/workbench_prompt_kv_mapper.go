package db

import (
	"context"
	"database/sql"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper WorkbenchPromptKVMapper -sql ./workbench_prompt_kv_mapper.xml -out ./workbench_prompt_kv_mapper.sqlmap.gen.go -dialect postgres

type workbenchKVRow struct {
	OrgUUID     string         `db:"organization_uuid"`
	PromptUUID  string         `db:"prompt_uuid"`
	Key         string         `db:"key"`
	Value       string         `db:"value"`
	VersionJSON sql.NullString `db:"version_json"`
	CreatedAt   time.Time      `db:"created_at"`
	UpdatedAt   time.Time      `db:"updated_at"`
}

type upsertWorkbenchKVParams struct {
	OrganizationUUID string
	PromptUUID       string
	Key              string
	Value            string
	VersionJSON      *string
}

type WorkbenchPromptKVMapper interface {
	Find(ctx context.Context, organizationUUID, promptUUID, key string) (workbenchKVRow, error)
	Upsert(ctx context.Context, params upsertWorkbenchKVParams) (int64, error)
	Delete(ctx context.Context, organizationUUID, promptUUID, key string) error
	DeleteByPromptRefUUID(ctx context.Context, promptRefUUID string) error
}

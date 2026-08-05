package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper WorkbenchPromptRevisionMapper -sql ./workbench_prompt_revision_mapper.xml -out ./workbench_prompt_revision_mapper.sqlmap.gen.go -dialect postgres

type workbenchRevisionRow struct {
	OrgUUID      string    `db:"organization_uuid"`
	PromptUUID   string    `db:"prompt_uuid"`
	RevisionUUID string    `db:"revision_uuid"`
	PayloadJSON  string    `db:"payload_json"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

type upsertWorkbenchRevisionParams struct {
	OrganizationUUID string
	PromptUUID       string
	RevisionUUID     string
	PayloadJSON      string
}

type WorkbenchPromptRevisionMapper interface {
	Find(ctx context.Context, organizationUUID, promptUUID, revisionUUID string) (workbenchRevisionRow, error)
	Upsert(ctx context.Context, params upsertWorkbenchRevisionParams) (int64, error)
	DeleteByPromptRefUUID(ctx context.Context, promptRefUUID string) error
}

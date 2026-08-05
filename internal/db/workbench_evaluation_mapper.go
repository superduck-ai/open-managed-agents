package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper WorkbenchEvaluationMapper -sql ./workbench_evaluation_mapper.xml -out ./workbench_evaluation_mapper.sqlmap.gen.go -dialect postgres

type workbenchEvaluationRow struct {
	OrgUUID        string    `db:"organization_uuid"`
	RevisionUUID   string    `db:"revision_uuid"`
	EvaluationUUID string    `db:"evaluation_uuid"`
	PayloadJSON    string    `db:"payload_json"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

type workbenchEvaluationRevisionRow struct {
	RevisionUUID string `db:"revision_uuid"`
}

type upsertWorkbenchEvaluationParams struct {
	OrganizationUUID string
	RevisionUUID     string
	EvaluationUUID   string
	PayloadJSON      string
}

type WorkbenchEvaluationMapper interface {
	ListRevisionIDs(ctx context.Context, organizationUUID string) ([]workbenchEvaluationRevisionRow, error)
	ListByRevision(ctx context.Context, organizationUUID, revisionUUID string) ([]workbenchEvaluationRow, error)
	Find(ctx context.Context, organizationUUID, evaluationUUID string) (workbenchEvaluationRow, error)
	Upsert(ctx context.Context, params upsertWorkbenchEvaluationParams) (int64, error)
	Delete(ctx context.Context, organizationUUID, evaluationUUID string) (workbenchEvaluationRow, error)
	DeleteByOrganization(ctx context.Context, organizationUUID string) error
}

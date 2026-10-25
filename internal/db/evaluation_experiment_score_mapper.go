package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EvaluationExperimentScoreMapper -sql ./evaluation_experiment_score_mapper.xml -out ./evaluation_experiment_score_mapper.sqlmap.gen.go -dialect postgres

type evaluationExperimentScoreRow struct {
	UUID                string    `db:"uuid"`
	ExternalID          string    `db:"external_id"`
	OrganizationUUID    string    `db:"organization_uuid"`
	WorkspaceUUID       string    `db:"workspace_uuid"`
	ExperimentUUID      string    `db:"experiment_uuid"`
	ItemUUID            string    `db:"item_uuid"`
	ItemExternalID      string    `db:"item_external_id"`
	EvaluatorUUID       string    `db:"evaluator_uuid"`
	EvaluatorExternalID string    `db:"evaluator_external_id"`
	VersionUUID         string    `db:"version_uuid"`
	VersionExternalID   string    `db:"version_external_id"`
	Position            int       `db:"position"`
	Status              string    `db:"status"`
	Score               *float64  `db:"score"`
	Explanation         string    `db:"explanation"`
	DetailsJSON         *string   `db:"details_json"`
	ErrorMessage        string    `db:"error_message"`
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
}

type evaluationExperimentScoreWriteParams struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	ExperimentUUID      string
	ItemUUID            string
	ItemExternalID      string
	EvaluatorUUID       string
	EvaluatorExternalID string
	VersionUUID         string
	VersionExternalID   string
	Position            int
	Status              string
	Score               *float64
	Explanation         string
	DetailsJSON         *string
	ErrorMessage        string
	CreatedAt           time.Time
}

type EvaluationExperimentScoreMapper interface {
	Insert(ctx context.Context, params evaluationExperimentScoreWriteParams) (evaluationExperimentScoreRow, error)
	ListByExperiment(ctx context.Context, organizationUUID, workspaceUUID, experimentUUID string) ([]evaluationExperimentScoreRow, error)
}

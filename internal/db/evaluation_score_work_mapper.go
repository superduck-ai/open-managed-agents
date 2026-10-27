package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper EvaluationScoreWorkMapper -sql ./evaluation_score_work_mapper.xml -out ./evaluation_score_work_mapper.sqlmap.gen.go -dialect postgres

type evaluationScoreWorkRow struct {
	OrganizationUUID     string  `db:"organization_uuid"`
	WorkspaceUUID        string  `db:"workspace_uuid"`
	ExperimentExternalID string  `db:"experiment_external_id"`
	ExternalID           string  `db:"external_id"`
	SessionExternalID    *string `db:"session_external_id"`
}

type evaluationAgentScoreSessionRow struct {
	ExternalID string `db:"external_id"`
}

type EvaluationScoreWorkMapper interface {
	ListItemsMissingScores(ctx context.Context, limit int) ([]evaluationScoreWorkRow, error)
	FindAgentScoreSession(ctx context.Context, organizationUUID, workspaceUUID, experimentID, itemID, versionID string) ([]evaluationAgentScoreSessionRow, error)
}

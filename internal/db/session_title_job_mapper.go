package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper SessionTitleJobMapper -sql ./session_title_job_mapper.xml -out ./session_title_job_mapper.sqlmap.gen.go -dialect postgres

type SessionTitleJobMapper interface {
	Enqueue(ctx context.Context, organizationUUID, workspaceUUID, sessionUUID, input string) error
	Lease(ctx context.Context, workerID string, limit int) ([]sessionTitleJobRow, error)
	Complete(ctx context.Context, params sessionTitleCompletionParams) error
}

type sessionTitleJobRow struct {
	UUID          string `db:"uuid"`
	WorkspaceUUID string `db:"workspace_uuid"`
	SessionUUID   string `db:"session_uuid"`
	SessionID     string `db:"session_id"`
	Input         string `db:"input"`
}

type sessionTitleCompletionParams struct {
	JobUUID       string
	WorkspaceUUID string
	WorkerID      string
	Title         string
}

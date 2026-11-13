package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SessionPortMapper -sql ./session_port_mapper.xml -out ./session_port_mapper.sqlmap.gen.go -dialect postgres

type sessionPortTargetRow struct {
	SessionUUID           string `db:"session_uuid"`
	SessionExternalID     string `db:"session_external_id"`
	OrganizationUUID      string `db:"organization_uuid"`
	WorkspaceUUID         string `db:"workspace_uuid"`
	Removed               bool   `db:"removed"`
	CodeSessionUUID       string `db:"code_session_uuid"`
	CodeSessionExternalID string `db:"code_session_external_id"`
	ProviderSandboxID     string `db:"provider_sandbox_id"`
	SandboxState          string `db:"sandbox_state"`
}

type sessionPortAccessParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	CodeSessionUUID  string
	Now              time.Time
	Threshold        time.Time
}

type SessionPortMapper interface {
	FindTarget(ctx context.Context, sessionUUID string) (sessionPortTargetRow, bool, error)
	TouchPortLastAccess(ctx context.Context, params sessionPortAccessParams) error
}

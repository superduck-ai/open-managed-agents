package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper DeploymentRunMapper -sql ./deployment_run_mapper.xml -out ./deployment_run_mapper.sqlmap.gen.go -dialect postgres

type deploymentRunMapperRow struct {
	UUID                 string     `db:"uuid"`
	ExternalID           string     `db:"external_id"`
	OrganizationUUID     string     `db:"organization_uuid"`
	WorkspaceUUID        string     `db:"workspace_uuid"`
	CreatedByAPIKeyUUID  *string    `db:"created_by_api_key_uuid"`
	DeploymentUUID       string     `db:"deployment_uuid"`
	DeploymentExternalID string     `db:"deployment_external_id"`
	AgentUUID            string     `db:"agent_uuid"`
	AgentExternalID      string     `db:"agent_external_id"`
	AgentVersion         int        `db:"agent_version"`
	AgentSnapshot        []byte     `db:"agent_snapshot"`
	SessionExternalID    *string    `db:"session_external_id"`
	Error                []byte     `db:"error"`
	TriggerType          string     `db:"trigger_type"`
	ScheduledAt          *time.Time `db:"scheduled_at"`
	CreatedAt            time.Time  `db:"created_at"`
	DeletedAt            *time.Time `db:"deleted_at"`
}

type deploymentRunWriteParams struct {
	UUID                 string
	ExternalID           string
	OrganizationUUID     string
	WorkspaceUUID        string
	CreatedByAPIKeyUUID  *string
	DeploymentUUID       string
	DeploymentExternalID string
	AgentUUID            string
	AgentExternalID      string
	AgentVersion         int
	AgentSnapshot        []byte
	SessionExternalID    *string
	Error                []byte
	TriggerType          string
	ScheduledAt          *time.Time
	CreatedAt            time.Time
}

type deploymentRunPageMapperParams struct {
	WorkspaceUUID        string
	FetchLimit           int
	Cursor               *DeploymentRunPageCursor
	DeploymentExternalID string
	TriggerType          string
	HasErrorFilter       bool
	HasError             bool
	CreatedAtGT          *time.Time
	CreatedAtGTE         *time.Time
	CreatedAtLT          *time.Time
	CreatedAtLTE         *time.Time
}

type DeploymentRunMapper interface {
	Insert(ctx context.Context, params deploymentRunWriteParams) (deploymentRunMapperRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, externalID string) (deploymentRunMapperRow, error)
	ListPage(ctx context.Context, params deploymentRunPageMapperParams) ([]deploymentRunMapperRow, error)
}

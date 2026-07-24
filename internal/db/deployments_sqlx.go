package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	deploymentSQLXColumns = `id, cast(uuid as text) as uuid, external_id, organization_id,
		workspace_id, created_by_api_key_id, environment_id, environment_external_id,
		agent_id, agent_external_id, agent_version, agent_snapshot, name, description,
		metadata, initial_events, resources, resource_secrets, vault_ids, schedule,
		last_run_at, status, paused_reason, created_at, updated_at, archived_at, deleted_at`
	deploymentRunSQLXColumns = `id, cast(uuid as text) as uuid, external_id, organization_id,
		workspace_id, created_by_api_key_id, deployment_id, deployment_external_id,
		agent_id, agent_external_id, agent_version, agent_snapshot, session_external_id,
		error, trigger_type, trigger_context, created_at, deleted_at`
	lockDeploymentForManualRunQuery = `
		select ` + deploymentSQLXColumns + `
		from deployments
		where workspace_id = :workspace_id
			and external_id = :deployment_external_id
			and deleted_at is null
		for update
	`
	createDeploymentRunQuery = `
		insert into deployment_runs (
			uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
			deployment_id, deployment_external_id, agent_id, agent_external_id,
			agent_version, agent_snapshot, session_external_id, error, trigger_type,
			trigger_context, created_at
		)
		values (
			:run_uuid, :run_external_id, :organization_id, :workspace_id,
			:created_by_api_key_id, :deployment_id, :deployment_external_id,
			:agent_id, :agent_external_id, :agent_version,
			CAST(:agent_snapshot AS jsonb), :session_external_id,
			CAST(:run_error AS jsonb), :trigger_type,
			CAST(:trigger_context AS jsonb), :created_at
		)
		returning ` + deploymentRunSQLXColumns + `
	`
	updateDeploymentLastRunQuery = `
		update deployments
		set last_run_at = :last_run_at,
			updated_at = :last_run_at
		where workspace_id = :workspace_id
			and external_id = :deployment_external_id
	`
)

// deploymentRow / deploymentRunRow 只承载 sqlx 扫描结果；与领域模型分离后，
// 可以把 JSONB、nullable 字段和列别名约束收敛在 DB 边界内。
type deploymentRow struct {
	ID                    int64      `db:"id"`
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationID        int64      `db:"organization_id"`
	WorkspaceID           int64      `db:"workspace_id"`
	CreatedByAPIKeyID     int64      `db:"created_by_api_key_id"`
	EnvironmentID         int64      `db:"environment_id"`
	EnvironmentExternalID string     `db:"environment_external_id"`
	AgentID               int64      `db:"agent_id"`
	AgentExternalID       string     `db:"agent_external_id"`
	AgentVersion          int        `db:"agent_version"`
	AgentSnapshot         []byte     `db:"agent_snapshot"`
	Name                  string     `db:"name"`
	Description           *string    `db:"description"`
	Metadata              []byte     `db:"metadata"`
	InitialEvents         []byte     `db:"initial_events"`
	Resources             []byte     `db:"resources"`
	ResourceSecrets       []byte     `db:"resource_secrets"`
	VaultIDs              []byte     `db:"vault_ids"`
	Schedule              []byte     `db:"schedule"`
	LastRunAt             *time.Time `db:"last_run_at"`
	Status                string     `db:"status"`
	PausedReason          []byte     `db:"paused_reason"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
	ArchivedAt            *time.Time `db:"archived_at"`
	DeletedAt             *time.Time `db:"deleted_at"`
}

type deploymentRunRow struct {
	ID                   int64      `db:"id"`
	UUID                 string     `db:"uuid"`
	ExternalID           string     `db:"external_id"`
	OrganizationID       int64      `db:"organization_id"`
	WorkspaceID          int64      `db:"workspace_id"`
	CreatedByAPIKeyID    int64      `db:"created_by_api_key_id"`
	DeploymentID         int64      `db:"deployment_id"`
	DeploymentExternalID string     `db:"deployment_external_id"`
	AgentID              int64      `db:"agent_id"`
	AgentExternalID      string     `db:"agent_external_id"`
	AgentVersion         int        `db:"agent_version"`
	AgentSnapshot        []byte     `db:"agent_snapshot"`
	SessionExternalID    *string    `db:"session_external_id"`
	Error                []byte     `db:"error"`
	TriggerType          string     `db:"trigger_type"`
	TriggerContext       []byte     `db:"trigger_context"`
	CreatedAt            time.Time  `db:"created_at"`
	DeletedAt            *time.Time `db:"deleted_at"`
}

func getDeploymentSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (Deployment, error) {
	var row deploymentRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Deployment{}, ErrNotFound
		}
		return Deployment{}, err
	}
	return row.deployment(), nil
}

func insertDeploymentRunSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	run DeploymentRun,
) (DeploymentRun, error) {
	var row deploymentRunRow
	if err := namedGetContext(ctx, database, &row, createDeploymentRunQuery, deploymentRunArguments(run)); err != nil {
		return DeploymentRun{}, err
	}
	return row.run(), nil
}

func updateDeploymentLastRunSQLX(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceID int64,
	deploymentExternalID string,
	lastRunAt time.Time,
) error {
	_, err := namedExecContext(ctx, tx, updateDeploymentLastRunQuery, map[string]any{
		"workspace_id":           workspaceID,
		"deployment_external_id": deploymentExternalID,
		"last_run_at":            lastRunAt,
	})
	return err
}

func deploymentRunArguments(run DeploymentRun) map[string]any {
	return map[string]any{
		"run_uuid":               run.UUID,
		"run_external_id":        run.ExternalID,
		"organization_id":        run.OrganizationID,
		"workspace_id":           run.WorkspaceID,
		"created_by_api_key_id":  run.CreatedByAPIKeyID,
		"deployment_id":          run.DeploymentID,
		"deployment_external_id": run.DeploymentExternalID,
		"agent_id":               run.AgentID,
		"agent_external_id":      run.AgentExternalID,
		"agent_version":          run.AgentVersion,
		"agent_snapshot":         jsonArg(run.AgentSnapshot),
		"session_external_id":    run.SessionExternalID,
		"run_error":              jsonArg(run.Error),
		"trigger_type":           run.TriggerType,
		"trigger_context":        jsonArg(run.TriggerContext),
		"created_at":             run.CreatedAt,
	}
}

func (r deploymentRow) deployment() Deployment {
	return Deployment{
		ID:                    r.ID,
		UUID:                  r.UUID,
		ExternalID:            r.ExternalID,
		OrganizationID:        r.OrganizationID,
		WorkspaceID:           r.WorkspaceID,
		CreatedByAPIKeyID:     r.CreatedByAPIKeyID,
		EnvironmentID:         r.EnvironmentID,
		EnvironmentExternalID: r.EnvironmentExternalID,
		AgentID:               r.AgentID,
		AgentExternalID:       r.AgentExternalID,
		AgentVersion:          r.AgentVersion,
		AgentSnapshot:         copyRaw(r.AgentSnapshot),
		Name:                  r.Name,
		Description:           r.Description,
		Metadata:              copyRaw(r.Metadata),
		InitialEvents:         copyRaw(r.InitialEvents),
		Resources:             copyRaw(r.Resources),
		ResourceSecrets:       copyRaw(r.ResourceSecrets),
		VaultIDs:              copyRaw(r.VaultIDs),
		Schedule:              copyRaw(r.Schedule),
		LastRunAt:             r.LastRunAt,
		Status:                r.Status,
		PausedReason:          copyRaw(r.PausedReason),
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
		ArchivedAt:            r.ArchivedAt,
		DeletedAt:             r.DeletedAt,
	}
}

func (r deploymentRunRow) run() DeploymentRun {
	return DeploymentRun{
		ID:                   r.ID,
		UUID:                 r.UUID,
		ExternalID:           r.ExternalID,
		OrganizationID:       r.OrganizationID,
		WorkspaceID:          r.WorkspaceID,
		CreatedByAPIKeyID:    r.CreatedByAPIKeyID,
		DeploymentID:         r.DeploymentID,
		DeploymentExternalID: r.DeploymentExternalID,
		AgentID:              r.AgentID,
		AgentExternalID:      r.AgentExternalID,
		AgentVersion:         r.AgentVersion,
		AgentSnapshot:        copyRaw(r.AgentSnapshot),
		SessionExternalID:    r.SessionExternalID,
		Error:                copyRaw(r.Error),
		TriggerType:          r.TriggerType,
		TriggerContext:       copyRaw(r.TriggerContext),
		CreatedAt:            r.CreatedAt,
		DeletedAt:            r.DeletedAt,
	}
}

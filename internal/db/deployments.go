package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Deployment struct {
	ID                    int64
	UUID                  string
	ExternalID            string
	OrganizationID        int64
	WorkspaceID           int64
	CreatedByAPIKeyID     int64
	EnvironmentID         int64
	EnvironmentExternalID string
	AgentID               int64
	AgentExternalID       string
	AgentVersion          int
	AgentSnapshot         json.RawMessage
	Name                  string
	Description           *string
	Metadata              json.RawMessage
	InitialEvents         json.RawMessage
	Resources             json.RawMessage
	ResourceSecrets       json.RawMessage
	VaultIDs              json.RawMessage
	Schedule              json.RawMessage
	LastRunAt             *time.Time
	Status                string
	PausedReason          json.RawMessage
	CreatedAt             time.Time
	UpdatedAt             time.Time
	ArchivedAt            *time.Time
	DeletedAt             *time.Time
}

type DeploymentRun struct {
	ID                   int64
	UUID                 string
	ExternalID           string
	OrganizationID       int64
	WorkspaceID          int64
	CreatedByAPIKeyID    int64
	DeploymentID         int64
	DeploymentExternalID string
	AgentID              int64
	AgentExternalID      string
	AgentVersion         int
	AgentSnapshot        json.RawMessage
	SessionExternalID    *string
	Error                json.RawMessage
	TriggerType          string
	TriggerContext       json.RawMessage
	CreatedAt            time.Time
	DeletedAt            *time.Time
}

type DeploymentPageCursor struct {
	CreatedAt time.Time
	ID        int64
}

type DeploymentRunPageCursor struct {
	CreatedAt time.Time
	ID        int64
}

type ListDeploymentsPageParams struct {
	WorkspaceID     int64
	Limit           int
	Cursor          *DeploymentPageCursor
	IncludeArchived bool
	AgentExternalID string
	Status          string
	CreatedAtGTE    *time.Time
	CreatedAtLTE    *time.Time
}

type ListDeploymentRunsPageParams struct {
	WorkspaceID          int64
	Limit                int
	Cursor               *DeploymentRunPageCursor
	DeploymentExternalID string
	TriggerType          string
	HasError             *bool
	CreatedAtGT          *time.Time
	CreatedAtGTE         *time.Time
	CreatedAtLT          *time.Time
	CreatedAtLTE         *time.Time
}

type CreateManualDeploymentRunInput struct {
	DeploymentExternalID string
	Session              CreateSessionInput
	Events               []SessionEvent
	Run                  DeploymentRun
	Now                  time.Time
}

func (d *DB) CreateDeployment(ctx context.Context, deployment Deployment) (Deployment, error) {
	return scanDeployment(d.Pool.QueryRow(ctx, `
		insert into deployments (
			uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
			environment_id, environment_external_id, agent_id, agent_external_id,
			agent_version, agent_snapshot, name, description, metadata, initial_events,
			resources, resource_secrets, vault_ids, schedule, last_run_at, status,
			paused_reason, created_at, updated_at
		)
		values (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11::jsonb, $12, $13, $14::jsonb, $15::jsonb,
			$16::jsonb, $17::jsonb, $18::jsonb, $19::jsonb, $20, $21,
			$22::jsonb, $23, $23
		)
		returning `+deploymentColumns()+`
	`, deployment.UUID, deployment.ExternalID, deployment.OrganizationID, deployment.WorkspaceID,
		deployment.CreatedByAPIKeyID, deployment.EnvironmentID, deployment.EnvironmentExternalID,
		deployment.AgentID, deployment.AgentExternalID, deployment.AgentVersion,
		jsonArg(deployment.AgentSnapshot), deployment.Name, deployment.Description, jsonArg(deployment.Metadata),
		jsonArg(deployment.InitialEvents), jsonArg(deployment.Resources), jsonArg(deployment.ResourceSecrets),
		jsonArg(deployment.VaultIDs), jsonArg(deployment.Schedule), deployment.LastRunAt, deployment.Status,
		jsonArg(deployment.PausedReason), deployment.CreatedAt))
}

func (d *DB) GetDeployment(ctx context.Context, workspaceID int64, externalID string) (Deployment, error) {
	return scanDeployment(d.Pool.QueryRow(ctx, `
		select `+deploymentColumns()+`
		from deployments
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, externalID))
}

func (d *DB) UpdateDeployment(ctx context.Context, workspaceID int64, externalID string, next Deployment) (Deployment, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return Deployment{}, err
	}
	defer tx.Rollback(ctx)

	current, err := scanDeployment(tx.QueryRow(ctx, `
		select `+deploymentColumns()+`
		from deployments
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		for update
	`, workspaceID, externalID))
	if err != nil {
		return Deployment{}, err
	}
	if current.ArchivedAt != nil {
		return Deployment{}, ErrInvalidState
	}
	updated, err := scanDeployment(tx.QueryRow(ctx, `
		update deployments
		set environment_id = $3,
			environment_external_id = $4,
			agent_id = $5,
			agent_external_id = $6,
			agent_version = $7,
			agent_snapshot = $8::jsonb,
			name = $9,
			description = $10,
			metadata = $11::jsonb,
			initial_events = $12::jsonb,
			resources = $13::jsonb,
			resource_secrets = $14::jsonb,
			vault_ids = $15::jsonb,
			schedule = $16::jsonb,
			updated_at = $17
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning `+deploymentColumns()+`
	`, workspaceID, externalID, next.EnvironmentID, next.EnvironmentExternalID, next.AgentID,
		next.AgentExternalID, next.AgentVersion, jsonArg(next.AgentSnapshot), next.Name,
		next.Description, jsonArg(next.Metadata), jsonArg(next.InitialEvents), jsonArg(next.Resources),
		jsonArg(next.ResourceSecrets), jsonArg(next.VaultIDs), jsonArg(next.Schedule), next.UpdatedAt))
	if err != nil {
		return Deployment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Deployment{}, err
	}
	return updated, nil
}

func (d *DB) ArchiveDeployment(ctx context.Context, workspaceID int64, externalID string) (Deployment, error) {
	return scanDeployment(d.Pool.QueryRow(ctx, `
		update deployments
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning `+deploymentColumns()+`
	`, workspaceID, externalID))
}

func (d *DB) PauseDeployment(ctx context.Context, workspaceID int64, externalID string, pausedReason json.RawMessage) (Deployment, error) {
	return scanDeployment(d.Pool.QueryRow(ctx, `
		update deployments
		set status = 'paused',
			paused_reason = $3::jsonb,
			updated_at = now()
		where workspace_id = $1 and external_id = $2 and deleted_at is null and archived_at is null
		returning `+deploymentColumns()+`
	`, workspaceID, externalID, jsonArg(pausedReason)))
}

func (d *DB) UnpauseDeployment(ctx context.Context, workspaceID int64, externalID string) (Deployment, error) {
	return scanDeployment(d.Pool.QueryRow(ctx, `
		update deployments
		set status = 'active',
			paused_reason = null,
			updated_at = now()
		where workspace_id = $1 and external_id = $2 and deleted_at is null and archived_at is null
		returning `+deploymentColumns()+`
	`, workspaceID, externalID))
}

func (d *DB) ListDeploymentsPage(ctx context.Context, params ListDeploymentsPageParams) ([]Deployment, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query := `
		select ` + deploymentColumns() + `
		from deployments
		where workspace_id = $1 and deleted_at is null
	`
	args := []any{params.WorkspaceID}
	nextArg := 2
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.AgentExternalID != "" {
		query += fmt.Sprintf(" and agent_external_id = $%d", nextArg)
		args = append(args, params.AgentExternalID)
		nextArg++
	}
	if params.Status != "" {
		query += fmt.Sprintf(" and status = $%d", nextArg)
		args = append(args, params.Status)
		nextArg++
	}
	if params.CreatedAtGTE != nil {
		query += fmt.Sprintf(" and created_at >= $%d", nextArg)
		args = append(args, *params.CreatedAtGTE)
		nextArg++
	}
	if params.CreatedAtLTE != nil {
		query += fmt.Sprintf(" and created_at <= $%d", nextArg)
		args = append(args, *params.CreatedAtLTE)
		nextArg++
	}
	if params.Cursor != nil {
		query += fmt.Sprintf(" and (created_at < $%d or (created_at = $%d and id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, params.Cursor.CreatedAt, params.Cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	deployments, err := scanDeploymentRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(deployments) > params.Limit
	if hasMore {
		deployments = deployments[:params.Limit]
	}
	return deployments, hasMore, nil
}

func (d *DB) CreateManualDeploymentRun(ctx context.Context, input CreateManualDeploymentRunInput) (DeploymentRun, Session, SessionThread, []SessionEvent, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	defer tx.Rollback(ctx)

	deployment, err := scanDeployment(tx.QueryRow(ctx, `
		select `+deploymentColumns()+`
		from deployments
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		for update
	`, input.Run.WorkspaceID, input.DeploymentExternalID))
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	if deployment.ArchivedAt != nil || deployment.Status != "active" {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, ErrInvalidState
	}

	session, thread, _, _, err := insertSessionTx(ctx, tx, input.Session)
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	events, err := insertSessionEventsTx(ctx, tx, session, input.Events, false)
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}

	run := input.Run
	run.DeploymentID = deployment.ID
	run.DeploymentExternalID = deployment.ExternalID
	run.AgentID = deployment.AgentID
	run.AgentExternalID = deployment.AgentExternalID
	run.AgentVersion = deployment.AgentVersion
	run.AgentSnapshot = deployment.AgentSnapshot
	run.SessionExternalID = &session.ExternalID
	run.Error = nil
	createdRun, err := insertDeploymentRunTx(ctx, tx, run)
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	if _, err := tx.Exec(ctx, `
		update deployments
		set last_run_at = $3,
			updated_at = $3
		where workspace_id = $1 and external_id = $2
	`, deployment.WorkspaceID, deployment.ExternalID, input.Now); err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	return createdRun, session, thread, events, nil
}

func (d *DB) CreateDeploymentRunFailure(ctx context.Context, deployment Deployment, run DeploymentRun) (DeploymentRun, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return DeploymentRun{}, err
	}
	defer tx.Rollback(ctx)
	run.DeploymentID = deployment.ID
	run.DeploymentExternalID = deployment.ExternalID
	run.AgentID = deployment.AgentID
	run.AgentExternalID = deployment.AgentExternalID
	run.AgentVersion = deployment.AgentVersion
	run.AgentSnapshot = deployment.AgentSnapshot
	run.SessionExternalID = nil
	created, err := insertDeploymentRunTx(ctx, tx, run)
	if err != nil {
		return DeploymentRun{}, err
	}
	if _, err := tx.Exec(ctx, `
		update deployments
		set last_run_at = $3,
			updated_at = $3
		where workspace_id = $1 and external_id = $2
	`, deployment.WorkspaceID, deployment.ExternalID, run.CreatedAt); err != nil {
		return DeploymentRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeploymentRun{}, err
	}
	return created, nil
}

func (d *DB) GetDeploymentRun(ctx context.Context, workspaceID int64, externalID string) (DeploymentRun, error) {
	return scanDeploymentRun(d.Pool.QueryRow(ctx, `
		select `+deploymentRunColumns()+`
		from deployment_runs
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, externalID))
}

func (d *DB) ListDeploymentRunsPage(ctx context.Context, params ListDeploymentRunsPageParams) ([]DeploymentRun, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query := `
		select ` + deploymentRunColumns() + `
		from deployment_runs
		where workspace_id = $1 and deleted_at is null
	`
	args := []any{params.WorkspaceID}
	nextArg := 2
	if params.DeploymentExternalID != "" {
		query += fmt.Sprintf(" and deployment_external_id = $%d", nextArg)
		args = append(args, params.DeploymentExternalID)
		nextArg++
	}
	if params.TriggerType != "" {
		query += fmt.Sprintf(" and trigger_type = $%d", nextArg)
		args = append(args, params.TriggerType)
		nextArg++
	}
	if params.HasError != nil {
		if *params.HasError {
			query += " and error is not null"
		} else {
			query += " and error is null"
		}
	}
	if params.CreatedAtGT != nil {
		query += fmt.Sprintf(" and created_at > $%d", nextArg)
		args = append(args, *params.CreatedAtGT)
		nextArg++
	}
	if params.CreatedAtGTE != nil {
		query += fmt.Sprintf(" and created_at >= $%d", nextArg)
		args = append(args, *params.CreatedAtGTE)
		nextArg++
	}
	if params.CreatedAtLT != nil {
		query += fmt.Sprintf(" and created_at < $%d", nextArg)
		args = append(args, *params.CreatedAtLT)
		nextArg++
	}
	if params.CreatedAtLTE != nil {
		query += fmt.Sprintf(" and created_at <= $%d", nextArg)
		args = append(args, *params.CreatedAtLTE)
		nextArg++
	}
	if params.Cursor != nil {
		query += fmt.Sprintf(" and (created_at < $%d or (created_at = $%d and id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, params.Cursor.CreatedAt, params.Cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	runs, err := scanDeploymentRunRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(runs) > params.Limit
	if hasMore {
		runs = runs[:params.Limit]
	}
	return runs, hasMore, nil
}

func insertDeploymentRunTx(ctx context.Context, tx pgx.Tx, run DeploymentRun) (DeploymentRun, error) {
	return scanDeploymentRun(tx.QueryRow(ctx, `
		insert into deployment_runs (
			uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
			deployment_id, deployment_external_id, agent_id, agent_external_id,
			agent_version, agent_snapshot, session_external_id, error, trigger_type,
			trigger_context, created_at
		)
		values (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11::jsonb, $12, $13::jsonb, $14,
			$15::jsonb, $16
		)
		returning `+deploymentRunColumns()+`
	`, run.UUID, run.ExternalID, run.OrganizationID, run.WorkspaceID, run.CreatedByAPIKeyID,
		run.DeploymentID, run.DeploymentExternalID, run.AgentID, run.AgentExternalID,
		run.AgentVersion, jsonArg(run.AgentSnapshot), run.SessionExternalID, jsonArg(run.Error),
		run.TriggerType, jsonArg(run.TriggerContext), run.CreatedAt))
}

func deploymentColumns() string {
	return `id, uuid::text, external_id, organization_id, workspace_id, created_by_api_key_id,
		environment_id, environment_external_id, agent_id, agent_external_id, agent_version,
		agent_snapshot, name, description, metadata, initial_events, resources,
		resource_secrets, vault_ids, schedule, last_run_at, status, paused_reason,
		created_at, updated_at, archived_at, deleted_at`
}

func deploymentRunColumns() string {
	return `id, uuid::text, external_id, organization_id, workspace_id, created_by_api_key_id,
		deployment_id, deployment_external_id, agent_id, agent_external_id, agent_version,
		agent_snapshot, session_external_id, error, trigger_type, trigger_context,
		created_at, deleted_at`
}

func scanDeployment(row rowScanner) (Deployment, error) {
	var deployment Deployment
	var agentSnapshot, metadata, initialEvents, resources, resourceSecrets, vaultIDs, schedule, pausedReason []byte
	err := row.Scan(&deployment.ID, &deployment.UUID, &deployment.ExternalID, &deployment.OrganizationID,
		&deployment.WorkspaceID, &deployment.CreatedByAPIKeyID, &deployment.EnvironmentID,
		&deployment.EnvironmentExternalID, &deployment.AgentID, &deployment.AgentExternalID,
		&deployment.AgentVersion, &agentSnapshot, &deployment.Name, &deployment.Description,
		&metadata, &initialEvents, &resources, &resourceSecrets, &vaultIDs, &schedule,
		&deployment.LastRunAt, &deployment.Status, &pausedReason, &deployment.CreatedAt,
		&deployment.UpdatedAt, &deployment.ArchivedAt, &deployment.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Deployment{}, ErrNotFound
	}
	if err != nil {
		return Deployment{}, err
	}
	deployment.AgentSnapshot = copyRaw(agentSnapshot)
	deployment.Metadata = copyRaw(metadata)
	deployment.InitialEvents = copyRaw(initialEvents)
	deployment.Resources = copyRaw(resources)
	deployment.ResourceSecrets = copyRaw(resourceSecrets)
	deployment.VaultIDs = copyRaw(vaultIDs)
	deployment.Schedule = copyRaw(schedule)
	deployment.PausedReason = copyRaw(pausedReason)
	return deployment, nil
}

func scanDeploymentRows(rows rowsScanner) ([]Deployment, error) {
	var deployments []Deployment
	for rows.Next() {
		deployment, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, deployment)
	}
	return deployments, rows.Err()
}

func scanDeploymentRun(row rowScanner) (DeploymentRun, error) {
	var run DeploymentRun
	var agentSnapshot, runError, triggerContext []byte
	err := row.Scan(&run.ID, &run.UUID, &run.ExternalID, &run.OrganizationID, &run.WorkspaceID,
		&run.CreatedByAPIKeyID, &run.DeploymentID, &run.DeploymentExternalID, &run.AgentID,
		&run.AgentExternalID, &run.AgentVersion, &agentSnapshot, &run.SessionExternalID,
		&runError, &run.TriggerType, &triggerContext, &run.CreatedAt, &run.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeploymentRun{}, ErrNotFound
	}
	if err != nil {
		return DeploymentRun{}, err
	}
	run.AgentSnapshot = copyRaw(agentSnapshot)
	run.Error = copyRaw(runError)
	run.TriggerContext = copyRaw(triggerContext)
	return run, nil
}

func scanDeploymentRunRows(rows rowsScanner) ([]DeploymentRun, error) {
	var runs []DeploymentRun
	for rows.Next() {
		run, err := scanDeploymentRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

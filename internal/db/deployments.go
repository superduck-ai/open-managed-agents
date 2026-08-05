package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type Deployment struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	CreatedByAPIKeyUUID   string
	EnvironmentUUID       string
	EnvironmentExternalID string
	AgentUUID             string
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
	UUID                 string
	ExternalID           string
	OrganizationUUID     string
	WorkspaceUUID        string
	CreatedByAPIKeyUUID  string
	DeploymentUUID       string
	DeploymentExternalID string
	AgentUUID            string
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
	UUID      string
}

type DeploymentRunPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type ListDeploymentsPageParams struct {
	WorkspaceUUID   string
	Limit           int
	Cursor          *DeploymentPageCursor
	IncludeArchived bool
	AgentExternalID string
	Status          string
	CreatedAtGTE    *time.Time
	CreatedAtLTE    *time.Time
}

type ListDeploymentRunsPageParams struct {
	WorkspaceUUID        string
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

const (
	createDeploymentQuery = `
		insert into deployments (
			uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
			environment_uuid, environment_external_id, agent_uuid, agent_external_id,
			agent_version, agent_snapshot, name, description, metadata, initial_events,
			resources, resource_secrets, vault_ids, schedule, last_run_at, status,
			paused_reason, created_at, updated_at
		)
		values (
			:uuid, :external_id, :organization_uuid, :workspace_uuid, :created_by_api_key_uuid,
			:environment_uuid, :environment_external_id, :agent_uuid, :agent_external_id,
			:agent_version, CAST(:agent_snapshot AS jsonb), :name, :description,
			CAST(:metadata AS jsonb), CAST(:initial_events AS jsonb),
			CAST(:resources AS jsonb), CAST(:resource_secrets AS jsonb),
			CAST(:vault_ids AS jsonb), CAST(:schedule AS jsonb), :last_run_at, :status,
			CAST(:paused_reason AS jsonb), :created_at, :created_at
		)
		returning ` + deploymentSQLXColumns + `
	`
	getDeploymentQuery = `
		select ` + deploymentSQLXColumns + `
		from deployments
		where workspace_uuid = :workspace_uuid
			and external_id = :external_id
			and deleted_at is null
	`
	lockDeploymentForUpdateQuery = getDeploymentQuery + ` for update`
	updateDeploymentQuery        = `
		update deployments
		set environment_uuid = :environment_uuid,
			environment_external_id = :environment_external_id,
			agent_uuid = :agent_uuid,
			agent_external_id = :agent_external_id,
			agent_version = :agent_version,
			agent_snapshot = CAST(:agent_snapshot AS jsonb),
			name = :name,
			description = :description,
			metadata = CAST(:metadata AS jsonb),
			initial_events = CAST(:initial_events AS jsonb),
			resources = CAST(:resources AS jsonb),
			resource_secrets = CAST(:resource_secrets AS jsonb),
			vault_ids = CAST(:vault_ids AS jsonb),
			schedule = CAST(:schedule AS jsonb),
			updated_at = :updated_at
		where workspace_uuid = :workspace_uuid
			and external_id = :external_id
			and deleted_at is null
		returning ` + deploymentSQLXColumns + `
	`
	archiveDeploymentQuery = `
		update deployments
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and external_id = :external_id
			and deleted_at is null
		returning ` + deploymentSQLXColumns + `
	`
	pauseDeploymentQuery = `
		update deployments
		set status = 'paused',
			paused_reason = CAST(:paused_reason AS jsonb),
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and external_id = :external_id
			and deleted_at is null
			and archived_at is null
		returning ` + deploymentSQLXColumns + `
	`
	unpauseDeploymentQuery = `
		update deployments
		set status = 'active',
			paused_reason = null,
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and external_id = :external_id
			and deleted_at is null
			and archived_at is null
		returning ` + deploymentSQLXColumns + `
	`
	getDeploymentRunQuery = `
		select ` + deploymentRunSQLXColumns + `
		from deployment_runs
		where workspace_uuid = :workspace_uuid
			and external_id = :external_id
			and deleted_at is null
	`
)

func (d *DB) CreateDeployment(ctx context.Context, deployment Deployment) (Deployment, error) {
	return getDeploymentSQLX(ctx, d.sql, createDeploymentQuery, deploymentArguments(deployment))
}

func (d *DB) GetDeployment(ctx context.Context, workspaceUUID string, externalID string) (Deployment, error) {
	return getDeploymentSQLX(ctx, d.sql, getDeploymentQuery, deploymentLookupArguments(workspaceUUID, externalID))
}

func (d *DB) UpdateDeployment(ctx context.Context, workspaceUUID string, externalID string, next Deployment) (Deployment, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return Deployment{}, err
	}
	defer tx.Rollback()

	arguments := deploymentArguments(next)
	arguments["workspace_uuid"] = dbUUID(workspaceUUID)
	arguments["external_id"] = externalID
	current, err := getDeploymentSQLX(ctx, tx, lockDeploymentForUpdateQuery, arguments)
	if err != nil {
		return Deployment{}, err
	}
	if current.ArchivedAt != nil {
		return Deployment{}, ErrInvalidState
	}
	updated, err := getDeploymentSQLX(ctx, tx, updateDeploymentQuery, arguments)
	if err != nil {
		return Deployment{}, err
	}
	if err := tx.Commit(); err != nil {
		return Deployment{}, err
	}
	return updated, nil
}

func (d *DB) ArchiveDeployment(ctx context.Context, workspaceUUID string, externalID string) (Deployment, error) {
	return getDeploymentSQLX(ctx, d.sql, archiveDeploymentQuery, deploymentLookupArguments(workspaceUUID, externalID))
}

func (d *DB) PauseDeployment(ctx context.Context, workspaceUUID string, externalID string, pausedReason json.RawMessage) (Deployment, error) {
	arguments := deploymentLookupArguments(workspaceUUID, externalID)
	arguments["paused_reason"] = jsonArg(pausedReason)
	return getDeploymentSQLX(ctx, d.sql, pauseDeploymentQuery, arguments)
}

func (d *DB) UnpauseDeployment(ctx context.Context, workspaceUUID string, externalID string) (Deployment, error) {
	return getDeploymentSQLX(ctx, d.sql, unpauseDeploymentQuery, deploymentLookupArguments(workspaceUUID, externalID))
}

func (d *DB) ListDeploymentsPage(ctx context.Context, params ListDeploymentsPageParams) ([]Deployment, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query, arguments := listDeploymentsQuery(params)
	deployments, err := selectDeploymentsSQLX(ctx, d.sql, query, arguments)
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
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	defer tx.Rollback()

	deployment, err := getDeploymentSQLX(ctx, tx, lockDeploymentForManualRunQuery, map[string]any{
		"workspace_uuid":         dbUUID(input.Run.WorkspaceUUID),
		"deployment_external_id": input.DeploymentExternalID,
	})
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	if deployment.ArchivedAt != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, ErrInvalidState
	}

	session, thread, _, _, err := insertSessionSQLXTx(ctx, tx, input.Session)
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	events, err := insertSessionEventsSQLXTx(ctx, tx, session, input.Events, false)
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}

	run := input.Run
	run.DeploymentUUID = deployment.UUID
	run.DeploymentExternalID = deployment.ExternalID
	run.AgentUUID = deployment.AgentUUID
	run.AgentExternalID = deployment.AgentExternalID
	run.AgentVersion = deployment.AgentVersion
	run.AgentSnapshot = deployment.AgentSnapshot
	run.SessionExternalID = &session.ExternalID
	run.Error = nil
	createdRun, err := insertDeploymentRunSQLX(ctx, tx, run)
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	if err := updateDeploymentLastRunSQLX(ctx, tx, deployment.WorkspaceUUID, deployment.ExternalID, input.Now); err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	return createdRun, session, thread, events, nil
}

func (d *DB) CreateDeploymentRunFailure(ctx context.Context, deployment Deployment, run DeploymentRun) (DeploymentRun, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return DeploymentRun{}, err
	}
	defer tx.Rollback()
	run.DeploymentUUID = deployment.UUID
	run.DeploymentExternalID = deployment.ExternalID
	run.AgentUUID = deployment.AgentUUID
	run.AgentExternalID = deployment.AgentExternalID
	run.AgentVersion = deployment.AgentVersion
	run.AgentSnapshot = deployment.AgentSnapshot
	run.SessionExternalID = nil
	created, err := insertDeploymentRunSQLX(ctx, tx, run)
	if err != nil {
		return DeploymentRun{}, err
	}
	if err := updateDeploymentLastRunSQLX(ctx, tx, deployment.WorkspaceUUID, deployment.ExternalID, run.CreatedAt); err != nil {
		return DeploymentRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return DeploymentRun{}, err
	}
	return created, nil
}

func (d *DB) GetDeploymentRun(ctx context.Context, workspaceUUID string, externalID string) (DeploymentRun, error) {
	return getDeploymentRunSQLX(ctx, d.sql, getDeploymentRunQuery, deploymentLookupArguments(workspaceUUID, externalID))
}

func (d *DB) ListDeploymentRunsPage(ctx context.Context, params ListDeploymentRunsPageParams) ([]DeploymentRun, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query, arguments := listDeploymentRunsQuery(params)
	runs, err := selectDeploymentRunsSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(runs) > params.Limit
	if hasMore {
		runs = runs[:params.Limit]
	}
	return runs, hasMore, nil
}

func listDeploymentsQuery(params ListDeploymentsPageParams) (string, map[string]any) {
	query := `
		select ` + deploymentSQLXColumns + `
		from deployments
		where workspace_uuid = :workspace_uuid and deleted_at is null
	`
	arguments := map[string]any{
		"workspace_uuid": dbUUID(params.WorkspaceUUID),
		"limit":          params.Limit + 1,
	}
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.AgentExternalID != "" {
		query += " and agent_external_id = :agent_external_id"
		arguments["agent_external_id"] = params.AgentExternalID
	}
	if params.Status != "" {
		query += " and status = :status"
		arguments["status"] = params.Status
	}
	if params.CreatedAtGTE != nil {
		query += " and created_at >= :created_at_gte"
		arguments["created_at_gte"] = *params.CreatedAtGTE
	}
	if params.CreatedAtLTE != nil {
		query += " and created_at <= :created_at_lte"
		arguments["created_at_lte"] = *params.CreatedAtLTE
	}
	if params.Cursor != nil {
		query += " and (created_at < :cursor_created_at or (created_at = :cursor_created_at and uuid < :cursor_uuid))"
		arguments["cursor_created_at"] = params.Cursor.CreatedAt
		arguments["cursor_uuid"] = dbUUID(params.Cursor.UUID)
	}
	query += " order by created_at desc, uuid desc limit :limit"
	return query, arguments
}

func listDeploymentRunsQuery(params ListDeploymentRunsPageParams) (string, map[string]any) {
	query := `
		select ` + deploymentRunSQLXColumns + `
		from deployment_runs
		where workspace_uuid = :workspace_uuid and deleted_at is null
	`
	arguments := map[string]any{
		"workspace_uuid": dbUUID(params.WorkspaceUUID),
		"limit":          params.Limit + 1,
	}
	if params.DeploymentExternalID != "" {
		query += " and deployment_external_id = :deployment_external_id"
		arguments["deployment_external_id"] = params.DeploymentExternalID
	}
	if params.TriggerType != "" {
		query += " and trigger_type = :trigger_type"
		arguments["trigger_type"] = params.TriggerType
	}
	if params.HasError != nil {
		if *params.HasError {
			query += " and error is not null"
		} else {
			query += " and error is null"
		}
	}
	query, arguments = deploymentRunTimeFilters(query, arguments, params)
	if params.Cursor != nil {
		query += " and (created_at < :cursor_created_at or (created_at = :cursor_created_at and uuid < :cursor_uuid))"
		arguments["cursor_created_at"] = params.Cursor.CreatedAt
		arguments["cursor_uuid"] = dbUUID(params.Cursor.UUID)
	}
	query += " order by created_at desc, uuid desc limit :limit"
	return query, arguments
}

func deploymentRunTimeFilters(
	query string,
	arguments map[string]any,
	params ListDeploymentRunsPageParams,
) (string, map[string]any) {
	if params.CreatedAtGT != nil {
		query += " and created_at > :created_at_gt"
		arguments["created_at_gt"] = *params.CreatedAtGT
	}
	if params.CreatedAtGTE != nil {
		query += " and created_at >= :created_at_gte"
		arguments["created_at_gte"] = *params.CreatedAtGTE
	}
	if params.CreatedAtLT != nil {
		query += " and created_at < :created_at_lt"
		arguments["created_at_lt"] = *params.CreatedAtLT
	}
	if params.CreatedAtLTE != nil {
		query += " and created_at <= :created_at_lte"
		arguments["created_at_lte"] = *params.CreatedAtLTE
	}
	return query, arguments
}

func deploymentLookupArguments(workspaceUUID string, externalID string) map[string]any {
	return map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"external_id":    externalID,
	}
}

func deploymentArguments(deployment Deployment) map[string]any {
	return map[string]any{
		"uuid":                    dbUUID(deployment.UUID),
		"external_id":             deployment.ExternalID,
		"organization_uuid":       dbUUID(deployment.OrganizationUUID),
		"workspace_uuid":          dbUUID(deployment.WorkspaceUUID),
		"created_by_api_key_uuid": dbUUID(deployment.CreatedByAPIKeyUUID),
		"environment_uuid":        dbUUID(deployment.EnvironmentUUID),
		"environment_external_id": deployment.EnvironmentExternalID,
		"agent_uuid":              dbUUID(deployment.AgentUUID),
		"agent_external_id":       deployment.AgentExternalID,
		"agent_version":           deployment.AgentVersion,
		"agent_snapshot":          jsonArg(deployment.AgentSnapshot),
		"name":                    deployment.Name,
		"description":             deployment.Description,
		"metadata":                jsonArg(deployment.Metadata),
		"initial_events":          jsonArg(deployment.InitialEvents),
		"resources":               jsonArg(deployment.Resources),
		"resource_secrets":        jsonArg(deployment.ResourceSecrets),
		"vault_ids":               jsonArg(deployment.VaultIDs),
		"schedule":                jsonArg(deployment.Schedule),
		"last_run_at":             deployment.LastRunAt,
		"status":                  deployment.Status,
		"paused_reason":           jsonArg(deployment.PausedReason),
		"created_at":              deployment.CreatedAt,
		"updated_at":              deployment.UpdatedAt,
	}
}

func selectDeploymentsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]Deployment, error) {
	var rows []deploymentRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	deployments := make([]Deployment, len(rows))
	for index := range rows {
		deployments[index] = rows[index].deployment()
	}
	return deployments, nil
}

func getDeploymentRunSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (DeploymentRun, error) {
	var row deploymentRunRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DeploymentRun{}, ErrNotFound
		}
		return DeploymentRun{}, err
	}
	return row.run(), nil
}

func selectDeploymentRunsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]DeploymentRun, error) {
	var rows []deploymentRunRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	runs := make([]DeploymentRun, len(rows))
	for index := range rows {
		runs[index] = rows[index].run()
	}
	return runs, nil
}

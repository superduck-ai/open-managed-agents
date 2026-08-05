package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/yourbatis"
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

func (d *DB) CreateDeployment(ctx context.Context, deployment Deployment) (Deployment, error) {
	mapper := NewDeploymentMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, deploymentWriteParamsFrom(deployment))
	if err != nil {
		return Deployment{}, err
	}
	return row.deployment(), nil
}

func (d *DB) GetDeployment(ctx context.Context, workspaceUUID string, externalID string) (Deployment, error) {
	mapper := NewDeploymentMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return Deployment{}, mapNoRows(err)
	}
	return row.deployment(), nil
}

func (d *DB) UpdateDeployment(ctx context.Context, workspaceUUID string, externalID string, next Deployment) (Deployment, error) {
	var updated Deployment
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewDeploymentMapper(executor)
		current, err := mapper.LockByExternalID(ctx, workspaceUUID, externalID)
		if err != nil {
			return mapNoRows(err)
		}
		if current.ArchivedAt != nil {
			return ErrInvalidState
		}

		params := deploymentWriteParamsFrom(next)
		params.WorkspaceUUID = workspaceUUID
		params.ExternalID = externalID
		row, err := mapper.UpdateByExternalID(ctx, params)
		if err != nil {
			return mapNoRows(err)
		}
		updated = row.deployment()
		return nil
	})
	return updated, err
}

func (d *DB) ArchiveDeployment(ctx context.Context, workspaceUUID string, externalID string) (Deployment, error) {
	mapper := NewDeploymentMapper(d.mapperDB)
	row, err := mapper.ArchiveByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return Deployment{}, mapNoRows(err)
	}
	return row.deployment(), nil
}

func (d *DB) PauseDeployment(ctx context.Context, workspaceUUID string, externalID string, pausedReason json.RawMessage) (Deployment, error) {
	mapper := NewDeploymentMapper(d.mapperDB)
	row, err := mapper.PauseByExternalID(ctx, workspaceUUID, externalID, agentJSONArg(pausedReason))
	if err != nil {
		return Deployment{}, mapNoRows(err)
	}
	return row.deployment(), nil
}

func (d *DB) UnpauseDeployment(ctx context.Context, workspaceUUID string, externalID string) (Deployment, error) {
	mapper := NewDeploymentMapper(d.mapperDB)
	row, err := mapper.UnpauseByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return Deployment{}, mapNoRows(err)
	}
	return row.deployment(), nil
}

func (d *DB) ListDeploymentsPage(ctx context.Context, params ListDeploymentsPageParams) ([]Deployment, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	mapper := NewDeploymentMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, deploymentPageParams(params))
	if err != nil {
		return nil, false, err
	}
	deployments := deploymentsFromRows(rows)
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

	executor := newSQLXTxExecutor(tx)
	deploymentMapper := NewDeploymentMapper(executor)
	runMapper := NewDeploymentRunMapper(executor)
	deploymentRow, err := deploymentMapper.LockByExternalID(ctx, input.Run.WorkspaceUUID, input.DeploymentExternalID)
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, mapNoRows(err)
	}
	deployment := deploymentRow.deployment()
	if deployment.ArchivedAt != nil || deployment.Status != "active" {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, ErrInvalidState
	}

	session, thread, _, _, err := insertSessionTx(ctx, executor, input.Session)
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	events, err := insertSessionEventsTx(ctx, executor, session, input.Events, false)
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}

	run := deploymentRunFromDeployment(input.Run, deployment)
	run.SessionExternalID = &session.ExternalID
	run.Error = nil
	createdRow, err := runMapper.Insert(ctx, deploymentRunWriteParamsFrom(run))
	if err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	if err := updateDeploymentLastRun(ctx, deploymentMapper, deployment.WorkspaceUUID, deployment.ExternalID, input.Now); err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return DeploymentRun{}, Session{}, SessionThread{}, nil, err
	}
	return createdRow.run(), session, thread, events, nil
}

func (d *DB) CreateDeploymentRunFailure(ctx context.Context, deployment Deployment, run DeploymentRun) (DeploymentRun, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return DeploymentRun{}, err
	}
	defer tx.Rollback()

	executor := newSQLXTxExecutor(tx)
	runMapper := NewDeploymentRunMapper(executor)
	deploymentMapper := NewDeploymentMapper(executor)
	run = deploymentRunFromDeployment(run, deployment)
	run.SessionExternalID = nil
	createdRow, err := runMapper.Insert(ctx, deploymentRunWriteParamsFrom(run))
	if err != nil {
		return DeploymentRun{}, err
	}
	if err := updateDeploymentLastRun(ctx, deploymentMapper, deployment.WorkspaceUUID, deployment.ExternalID, run.CreatedAt); err != nil {
		return DeploymentRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return DeploymentRun{}, err
	}
	return createdRow.run(), nil
}

func (d *DB) GetDeploymentRun(ctx context.Context, workspaceUUID string, externalID string) (DeploymentRun, error) {
	mapper := NewDeploymentRunMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return DeploymentRun{}, mapNoRows(err)
	}
	return row.run(), nil
}

func (d *DB) ListDeploymentRunsPage(ctx context.Context, params ListDeploymentRunsPageParams) ([]DeploymentRun, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	mapper := NewDeploymentRunMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, deploymentRunPageParams(params))
	if err != nil {
		return nil, false, err
	}
	runs := deploymentRunsFromRows(rows)
	hasMore := len(runs) > params.Limit
	if hasMore {
		runs = runs[:params.Limit]
	}
	return runs, hasMore, nil
}

func updateDeploymentLastRun(ctx context.Context, mapper DeploymentMapper, workspaceUUID, externalID string, lastRunAt time.Time) error {
	rowsAffected, err := mapper.UpdateLastRun(ctx, workspaceUUID, externalID, lastRunAt)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func deploymentRunFromDeployment(run DeploymentRun, deployment Deployment) DeploymentRun {
	run.DeploymentUUID = deployment.UUID
	run.DeploymentExternalID = deployment.ExternalID
	run.AgentUUID = deployment.AgentUUID
	run.AgentExternalID = deployment.AgentExternalID
	run.AgentVersion = deployment.AgentVersion
	run.AgentSnapshot = deployment.AgentSnapshot
	return run
}

func deploymentWriteParamsFrom(deployment Deployment) deploymentWriteParams {
	return deploymentWriteParams{
		UUID: deployment.UUID, ExternalID: deployment.ExternalID,
		OrganizationUUID: deployment.OrganizationUUID, WorkspaceUUID: deployment.WorkspaceUUID,
		CreatedByAPIKeyUUID: deployment.CreatedByAPIKeyUUID, EnvironmentUUID: deployment.EnvironmentUUID,
		EnvironmentExternalID: deployment.EnvironmentExternalID, AgentUUID: deployment.AgentUUID,
		AgentExternalID: deployment.AgentExternalID, AgentVersion: deployment.AgentVersion,
		AgentSnapshot: agentJSONArg(deployment.AgentSnapshot), Name: deployment.Name, Description: deployment.Description,
		Metadata: agentJSONArg(deployment.Metadata), InitialEvents: agentJSONArg(deployment.InitialEvents),
		Resources: agentJSONArg(deployment.Resources), ResourceSecrets: agentJSONArg(deployment.ResourceSecrets),
		VaultIDs: agentJSONArg(deployment.VaultIDs), Schedule: agentJSONArg(deployment.Schedule),
		LastRunAt: deployment.LastRunAt, Status: deployment.Status, PausedReason: agentJSONArg(deployment.PausedReason),
		CreatedAt: deployment.CreatedAt, UpdatedAt: deployment.UpdatedAt,
	}
}

func deploymentRunWriteParamsFrom(run DeploymentRun) deploymentRunWriteParams {
	return deploymentRunWriteParams{
		UUID: run.UUID, ExternalID: run.ExternalID, OrganizationUUID: run.OrganizationUUID,
		WorkspaceUUID: run.WorkspaceUUID, CreatedByAPIKeyUUID: run.CreatedByAPIKeyUUID,
		DeploymentUUID: run.DeploymentUUID, DeploymentExternalID: run.DeploymentExternalID,
		AgentUUID: run.AgentUUID, AgentExternalID: run.AgentExternalID, AgentVersion: run.AgentVersion,
		AgentSnapshot: agentJSONArg(run.AgentSnapshot), SessionExternalID: run.SessionExternalID,
		Error: agentJSONArg(run.Error), TriggerType: run.TriggerType,
		TriggerContext: agentJSONArg(run.TriggerContext), CreatedAt: run.CreatedAt,
	}
}

func deploymentPageParams(params ListDeploymentsPageParams) deploymentPageMapperParams {
	return deploymentPageMapperParams{
		WorkspaceUUID: params.WorkspaceUUID, FetchLimit: params.Limit + 1, Cursor: params.Cursor,
		IncludeArchived: params.IncludeArchived, AgentExternalID: params.AgentExternalID,
		Status: params.Status, CreatedAtGTE: params.CreatedAtGTE, CreatedAtLTE: params.CreatedAtLTE,
	}
}

func deploymentRunPageParams(params ListDeploymentRunsPageParams) deploymentRunPageMapperParams {
	mapperParams := deploymentRunPageMapperParams{
		WorkspaceUUID: params.WorkspaceUUID, FetchLimit: params.Limit + 1, Cursor: params.Cursor,
		DeploymentExternalID: params.DeploymentExternalID, TriggerType: params.TriggerType,
		CreatedAtGT: params.CreatedAtGT, CreatedAtGTE: params.CreatedAtGTE,
		CreatedAtLT: params.CreatedAtLT, CreatedAtLTE: params.CreatedAtLTE,
	}
	if params.HasError != nil {
		mapperParams.HasErrorFilter = true
		mapperParams.HasError = *params.HasError
	}
	return mapperParams
}

func deploymentsFromRows(rows []deploymentMapperRow) []Deployment {
	deployments := make([]Deployment, len(rows))
	for index := range rows {
		deployments[index] = rows[index].deployment()
	}
	return deployments
}

func deploymentRunsFromRows(rows []deploymentRunMapperRow) []DeploymentRun {
	runs := make([]DeploymentRun, len(rows))
	for index := range rows {
		runs[index] = rows[index].run()
	}
	return runs
}

func (r deploymentMapperRow) deployment() Deployment {
	return Deployment{
		UUID: r.UUID, ExternalID: r.ExternalID, OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID: r.WorkspaceUUID, CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID,
		EnvironmentUUID: r.EnvironmentUUID, EnvironmentExternalID: r.EnvironmentExternalID,
		AgentUUID: r.AgentUUID, AgentExternalID: r.AgentExternalID, AgentVersion: r.AgentVersion,
		AgentSnapshot: copyRaw(r.AgentSnapshot), Name: r.Name, Description: r.Description,
		Metadata: copyRaw(r.Metadata), InitialEvents: copyRaw(r.InitialEvents), Resources: copyRaw(r.Resources),
		ResourceSecrets: copyRaw(r.ResourceSecrets), VaultIDs: copyRaw(r.VaultIDs), Schedule: copyRaw(r.Schedule),
		LastRunAt: r.LastRunAt, Status: r.Status, PausedReason: copyRaw(r.PausedReason), CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt, ArchivedAt: r.ArchivedAt, DeletedAt: r.DeletedAt,
	}
}

func (r deploymentRunMapperRow) run() DeploymentRun {
	return DeploymentRun{
		UUID: r.UUID, ExternalID: r.ExternalID, OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID: r.WorkspaceUUID, CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID,
		DeploymentUUID: r.DeploymentUUID, DeploymentExternalID: r.DeploymentExternalID,
		AgentUUID: r.AgentUUID, AgentExternalID: r.AgentExternalID, AgentVersion: r.AgentVersion,
		AgentSnapshot: copyRaw(r.AgentSnapshot), SessionExternalID: r.SessionExternalID,
		Error: copyRaw(r.Error), TriggerType: r.TriggerType, TriggerContext: copyRaw(r.TriggerContext),
		CreatedAt: r.CreatedAt, DeletedAt: r.DeletedAt,
	}
}

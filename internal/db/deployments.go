package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/samber/lo"
	"github.com/superduck-ai/yourbatis"
)

// MaxScheduledDeploymentsPerOrganization is the org-level ceiling for non-archived
// deployments that still have a schedule.
const MaxScheduledDeploymentsPerOrganization = 1000

type Deployment struct {
	// RuntimeUserUUID is inherited by scheduled sessions, independently of API keys.
	RuntimeUserUUID       string
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
	ScheduledAt          *time.Time
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

type UpdateDeploymentInput struct {
	Deployment       Deployment
	ScheduleProvided bool
}

type DeploymentSchedule struct {
	WorkspaceUUID string
	ExternalID    string
	Schedule      []byte
}

type ApplyScheduledOccurrenceInput struct {
	Deployment        Deployment
	ScheduledAt       time.Time
	Session           *CreateSessionInput
	Events            []SessionEvent
	Run               DeploymentRun
	AutoPauseReason   json.RawMessage
	ArchiveDeployment bool
	Now               time.Time
}

type DeploymentScheduleTxHook func(context.Context, *sql.Tx, Deployment) error

func (d *DB) SetDeploymentScheduleTxHook(hook DeploymentScheduleTxHook) {
	d.deploymentScheduleTxHook = hook
}

func (d *DB) applyDeploymentScheduleTxHook(ctx context.Context, executor yourbatis.Executor, deployment Deployment) error {
	if d.deploymentScheduleTxHook == nil {
		return nil
	}
	tx, ok := executor.(*yourbatis.Tx)
	if !ok {
		return errors.New("deployment schedule hook requires a transaction")
	}
	return d.deploymentScheduleTxHook(ctx, tx.SQLTx(), deployment)
}

func (d *DB) ReconcileDeploymentSchedule(
	ctx context.Context,
	workspaceUUID string,
	externalID string,
	reconcile DeploymentScheduleTxHook,
) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		deployment := Deployment{WorkspaceUUID: workspaceUUID, ExternalID: externalID}
		row, err := NewDeploymentMapper(executor).LockByExternalID(ctx, workspaceUUID, externalID)
		if err == nil {
			deployment = row.deployment()
		} else if !errors.Is(mapNoRows(err), ErrNotFound) {
			return err
		}
		tx, ok := executor.(*yourbatis.Tx)
		if !ok {
			return errors.New("deployment schedule reconcile requires a transaction")
		}
		return reconcile(ctx, tx.SQLTx(), deployment)
	})
}

func (d *DB) CreateDeployment(ctx context.Context, deployment Deployment) (Deployment, error) {
	if len(deployment.Schedule) == 0 {
		row, err := NewDeploymentMapper(d.mapperDB).Insert(ctx, deploymentWriteParamsFrom(deployment))
		if err != nil {
			return Deployment{}, err
		}
		return row.deployment(), nil
	}

	var created Deployment
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewDeploymentMapper(executor)
		if err := checkScheduledDeploymentQuota(ctx, mapper, deployment.OrganizationUUID); err != nil {
			return err
		}
		row, err := mapper.Insert(ctx, deploymentWriteParamsFrom(deployment))
		if err != nil {
			return err
		}
		created = row.deployment()
		return d.applyDeploymentScheduleTxHook(ctx, executor, created)
	})
	return created, err
}

func (d *DB) GetDeployment(ctx context.Context, workspaceUUID string, externalID string) (Deployment, error) {
	mapper := NewDeploymentMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return Deployment{}, mapNoRows(err)
	}
	return row.deployment(), nil
}

func (d *DB) UpdateDeployment(ctx context.Context, workspaceUUID string, externalID string, input UpdateDeploymentInput) (Deployment, error) {
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
		next := input.Deployment
		scheduleChanged := input.ScheduleProvided && !sameJSON(current.Schedule, next.Schedule)
		if scheduleChanged && len(current.Schedule) == 0 && len(next.Schedule) > 0 {
			if err := checkScheduledDeploymentQuota(ctx, mapper, current.OrganizationUUID); err != nil {
				return err
			}
		}

		params := deploymentWriteParamsFrom(next)
		params.WorkspaceUUID = workspaceUUID
		params.ExternalID = externalID
		params.ScheduleChanged = scheduleChanged
		row, err := mapper.UpdateByExternalID(ctx, params)
		if err != nil {
			return mapNoRows(err)
		}
		updated = row.deployment()
		if scheduleChanged {
			return d.applyDeploymentScheduleTxHook(ctx, executor, updated)
		}
		return nil
	})
	return updated, err
}

func checkScheduledDeploymentQuota(ctx context.Context, mapper DeploymentMapper, organizationUUID string) error {
	count, err := mapper.CountScheduledByOrganization(ctx, organizationUUID)
	if err != nil {
		return err
	}
	if count >= MaxScheduledDeploymentsPerOrganization {
		return ErrLimitExceeded
	}
	return nil
}

func (d *DB) ArchiveDeployment(ctx context.Context, workspaceUUID string, externalID string) (Deployment, error) {
	var archived Deployment
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, err := NewDeploymentMapper(executor).ArchiveByExternalID(ctx, workspaceUUID, externalID)
		if err != nil {
			return mapNoRows(err)
		}
		archived = row.deployment()
		return d.applyDeploymentScheduleTxHook(ctx, executor, archived)
	})
	return archived, err
}

func (d *DB) PauseDeployment(ctx context.Context, workspaceUUID string, externalID string, pausedReason json.RawMessage) (Deployment, error) {
	var paused Deployment
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, err := NewDeploymentMapper(executor).PauseByExternalID(ctx, workspaceUUID, externalID, agentJSONArg(pausedReason))
		if err != nil {
			return mapNoRows(err)
		}
		paused = row.deployment()
		return d.applyDeploymentScheduleTxHook(ctx, executor, paused)
	})
	return paused, err
}

func (d *DB) UnpauseDeployment(ctx context.Context, workspaceUUID string, externalID string) (Deployment, error) {
	var unpaused Deployment
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, err := NewDeploymentMapper(executor).UnpauseByExternalID(ctx, workspaceUUID, externalID)
		if err != nil {
			return mapNoRows(err)
		}
		unpaused = row.deployment()
		return d.applyDeploymentScheduleTxHook(ctx, executor, unpaused)
	})
	return unpaused, err
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

func (d *DB) ListDeploymentSchedules(ctx context.Context) ([]DeploymentSchedule, error) {
	return NewDeploymentMapper(d.mapperDB).ListActiveSchedules(ctx)
}

func (d *DB) CreateManualDeploymentRun(ctx context.Context, input CreateManualDeploymentRunInput) (DeploymentRun, Session, SessionThread, []SessionEvent, error) {
	var created DeploymentRun
	var session Session
	var thread SessionThread
	var events []SessionEvent
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		deploymentMapper := NewDeploymentMapper(executor)
		runMapper := NewDeploymentRunMapper(executor)
		deploymentRow, err := deploymentMapper.LockByExternalID(ctx, input.Session.Session.WorkspaceUUID, input.DeploymentExternalID)
		if err != nil {
			return mapNoRows(err)
		}
		deployment := deploymentRow.deployment()
		if deployment.ArchivedAt != nil {
			return ErrInvalidState
		}

		session, thread, _, _, err = insertSessionTx(ctx, executor, input.Session)
		if err != nil {
			return err
		}
		events, err = insertSessionEventsTx(ctx, executor, session, input.Events, false)
		if err != nil {
			return err
		}

		run := deploymentRunFromDeployment(input.Run, deployment)
		run.SessionExternalID = &session.ExternalID
		run.Error = nil
		createdRow, err := runMapper.Insert(ctx, deploymentRunWriteParamsFrom(run))
		if err != nil {
			return err
		}
		if err := updateDeploymentLastRun(ctx, deploymentMapper, deployment.WorkspaceUUID, deployment.ExternalID, input.Now); err != nil {
			return err
		}
		created = createdRow.run()
		return nil
	})
	return created, session, thread, events, err
}

func (d *DB) ApplyScheduledOccurrence(ctx context.Context, input ApplyScheduledOccurrenceInput) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		deploymentMapper := NewDeploymentMapper(executor)
		row, err := deploymentMapper.LockByExternalID(ctx, input.Deployment.WorkspaceUUID, input.Deployment.ExternalID)
		if err != nil {
			return mapNoRows(err)
		}
		deployment := row.deployment()
		if deployment.ArchivedAt != nil || deployment.Status != "active" ||
			!sameJSON(deployment.Schedule, input.Deployment.Schedule) ||
			!sameDeploymentExecution(deployment, input.Deployment) {
			return ErrStaleSchedule
		}
		if input.ArchiveDeployment {
			archivedRow, err := deploymentMapper.ArchiveByExternalID(ctx, deployment.WorkspaceUUID, deployment.ExternalID)
			if err != nil {
				return err
			}
			return d.applyDeploymentScheduleTxHook(ctx, executor, archivedRow.deployment())
		}
		if input.Session != nil {
			workspace, err := NewAdminWorkspaceMapper(executor).FindByIdentifier(
				ctx, deployment.OrganizationUUID, "", deployment.WorkspaceUUID,
			)
			if err != nil {
				return mapNoRows(err)
			}
			if workspace.ArchivedAt != nil {
				return ErrWorkspaceArchived
			}
		}

		runMapper := NewDeploymentRunMapper(executor)
		run := deploymentRunFromDeployment(input.Run, deployment)
		run.CreatedByAPIKeyUUID = deployment.CreatedByAPIKeyUUID
		run.TriggerType = "schedule"
		run.ScheduledAt = &input.ScheduledAt
		run.CreatedAt = input.Now
		if input.Session != nil {
			session, _, _, _, err := insertSessionTx(ctx, executor, *input.Session)
			if err != nil {
				return err
			}
			if _, err = insertSessionEventsTx(ctx, executor, session, input.Events, false); err != nil {
				return err
			}
			run.SessionExternalID = &session.ExternalID
			run.Error = nil
		} else {
			run.SessionExternalID = nil
		}
		_, err = runMapper.Insert(ctx, deploymentRunWriteParamsFrom(run))
		if err != nil {
			if isUniqueViolationOnConstraint(err, "deployment_runs_schedule_occurrence_idx") {
				return ErrStaleSchedule
			}
			return err
		}

		if len(input.AutoPauseReason) > 0 {
			_, err = deploymentMapper.PauseAfterScheduledRun(ctx, pauseScheduledDeploymentParams{
				WorkspaceUUID: deployment.WorkspaceUUID, ExternalID: deployment.ExternalID,
				PausedReason: agentJSONArg(input.AutoPauseReason), LastRunAt: input.Now,
			})
			if err == nil {
				deployment.Status = "paused"
				err = d.applyDeploymentScheduleTxHook(ctx, executor, deployment)
			}
		} else {
			_, err = deploymentMapper.UpdateLastRun(
				ctx, deployment.WorkspaceUUID, deployment.ExternalID, input.Now,
			)
		}
		return err
	})
}

func sameDeploymentExecution(left Deployment, right Deployment) bool {
	return left.AgentUUID == right.AgentUUID &&
		left.AgentExternalID == right.AgentExternalID &&
		left.AgentVersion == right.AgentVersion &&
		sameJSON(left.AgentSnapshot, right.AgentSnapshot) &&
		left.EnvironmentUUID == right.EnvironmentUUID &&
		left.EnvironmentExternalID == right.EnvironmentExternalID &&
		sameJSON(left.Metadata, right.Metadata) &&
		sameJSON(left.InitialEvents, right.InitialEvents) &&
		sameJSON(left.Resources, right.Resources) &&
		sameJSON(left.ResourceSecrets, right.ResourceSecrets) &&
		sameJSON(left.VaultIDs, right.VaultIDs)
}

func (d *DB) CreateDeploymentRunFailure(ctx context.Context, deployment Deployment, run DeploymentRun) (DeploymentRun, error) {
	var created DeploymentRun
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		runMapper := NewDeploymentRunMapper(executor)
		deploymentMapper := NewDeploymentMapper(executor)
		run = deploymentRunFromDeployment(run, deployment)
		run.SessionExternalID = nil
		createdRow, err := runMapper.Insert(ctx, deploymentRunWriteParamsFrom(run))
		if err != nil {
			return err
		}
		if err := updateDeploymentLastRun(ctx, deploymentMapper, deployment.WorkspaceUUID, deployment.ExternalID, run.CreatedAt); err != nil {
			return err
		}
		created = createdRow.run()
		return nil
	})
	return created, err
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
	run.OrganizationUUID = deployment.OrganizationUUID
	run.WorkspaceUUID = deployment.WorkspaceUUID
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
		RuntimeUserUUID:     nullableString(deployment.RuntimeUserUUID),
		CreatedByAPIKeyUUID: nullableString(deployment.CreatedByAPIKeyUUID), EnvironmentUUID: deployment.EnvironmentUUID,
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
		WorkspaceUUID: run.WorkspaceUUID, CreatedByAPIKeyUUID: nullableString(run.CreatedByAPIKeyUUID),
		DeploymentUUID: run.DeploymentUUID, DeploymentExternalID: run.DeploymentExternalID,
		AgentUUID: run.AgentUUID, AgentExternalID: run.AgentExternalID, AgentVersion: run.AgentVersion,
		AgentSnapshot: agentJSONArg(run.AgentSnapshot), SessionExternalID: run.SessionExternalID,
		Error: agentJSONArg(run.Error), TriggerType: run.TriggerType, ScheduledAt: run.ScheduledAt, CreatedAt: run.CreatedAt,
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
	return lo.Map(rows, func(row deploymentMapperRow, _ int) Deployment { return row.deployment() })
}

func deploymentRunsFromRows(rows []deploymentRunMapperRow) []DeploymentRun {
	return lo.Map(rows, func(row deploymentRunMapperRow, _ int) DeploymentRun { return row.run() })
}

func (r deploymentMapperRow) deployment() Deployment {
	return Deployment{
		UUID: r.UUID, ExternalID: r.ExternalID, OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID: r.WorkspaceUUID, CreatedByAPIKeyUUID: stringFromNullable(r.CreatedByAPIKeyUUID),
		RuntimeUserUUID: stringFromNullable(r.RuntimeUserUUID),
		EnvironmentUUID: r.EnvironmentUUID, EnvironmentExternalID: r.EnvironmentExternalID,
		AgentUUID: r.AgentUUID, AgentExternalID: r.AgentExternalID, AgentVersion: r.AgentVersion,
		AgentSnapshot: bytes.Clone(r.AgentSnapshot), Name: r.Name, Description: r.Description,
		Metadata: bytes.Clone(r.Metadata), InitialEvents: bytes.Clone(r.InitialEvents), Resources: bytes.Clone(r.Resources),
		ResourceSecrets: bytes.Clone(r.ResourceSecrets), VaultIDs: bytes.Clone(r.VaultIDs), Schedule: bytes.Clone(r.Schedule),
		LastRunAt: r.LastRunAt, Status: r.Status, PausedReason: bytes.Clone(r.PausedReason), CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt, ArchivedAt: r.ArchivedAt, DeletedAt: r.DeletedAt,
	}
}

func (r deploymentRunMapperRow) run() DeploymentRun {
	return DeploymentRun{
		UUID: r.UUID, ExternalID: r.ExternalID, OrganizationUUID: r.OrganizationUUID,
		WorkspaceUUID: r.WorkspaceUUID, CreatedByAPIKeyUUID: stringFromNullable(r.CreatedByAPIKeyUUID),
		DeploymentUUID: r.DeploymentUUID, DeploymentExternalID: r.DeploymentExternalID,
		AgentUUID: r.AgentUUID, AgentExternalID: r.AgentExternalID, AgentVersion: r.AgentVersion,
		AgentSnapshot: bytes.Clone(r.AgentSnapshot), SessionExternalID: r.SessionExternalID,
		Error: bytes.Clone(r.Error), TriggerType: r.TriggerType, ScheduledAt: r.ScheduledAt,
		CreatedAt: r.CreatedAt, DeletedAt: r.DeletedAt,
	}
}

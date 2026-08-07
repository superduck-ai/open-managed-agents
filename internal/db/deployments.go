package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/yourbatis"
)

// MaxScheduledDeploymentsPerOrganization is the org-level ceiling for non-archived
// deployments that still have a schedule. Enforced under LockOrganization so concurrent
// creates cannot race past the limit.
const MaxScheduledDeploymentsPerOrganization = 1000

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
	ScheduleRevision      int64
	NextScheduledAt       *time.Time
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
	Deployment      Deployment
	ScheduleChanged bool
}

type DeploymentScheduleState struct {
	WorkspaceUUID    string
	ExternalID       string
	Schedule         json.RawMessage
	ScheduleRevision int64
	NextScheduledAt  *time.Time
}

type ApplyScheduledOccurrenceInput struct {
	WorkspaceUUID        string
	DeploymentExternalID string
	ScheduleRevision     int64
	ScheduledAt          time.Time
	NextScheduledAt      *time.Time
	Session              *CreateSessionInput
	Events               []SessionEvent
	Run                  DeploymentRun
	AutoPauseReason      json.RawMessage
	WebhookEvents        []WebhookDeliveryEvent
	ArchiveDeployment    bool
	Now                  time.Time
}

type ScheduledOccurrenceResult struct {
	Run     DeploymentRun
	Session Session
	Thread  SessionThread
	Events  []SessionEvent
}

func (d *DB) CreateDeployment(ctx context.Context, deployment Deployment) (Deployment, error) {
	var created Deployment
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		var err error
		created, err = createDeployment(ctx, executor, deployment)
		return err
	})
	return created, err
}

func (d *DB) CreateDeploymentTx(ctx context.Context, tx *yourbatis.Tx, deployment Deployment) (Deployment, error) {
	return createDeployment(ctx, tx, deployment)
}

func createDeployment(ctx context.Context, executor yourbatis.Executor, deployment Deployment) (Deployment, error) {
	deploymentMapper := NewDeploymentMapper(executor)
	if len(deployment.Schedule) > 0 {
		if err := checkScheduledDeploymentQuota(ctx, deploymentMapper, deployment.OrganizationUUID); err != nil {
			return Deployment{}, err
		}
	}
	row, err := deploymentMapper.Insert(ctx, deploymentWriteParamsFrom(deployment))
	if err != nil {
		return Deployment{}, err
	}
	return row.deployment(), nil
}

func (d *DB) GetDeployment(ctx context.Context, workspaceUUID string, externalID string) (Deployment, error) {
	deploymentMapper := NewDeploymentMapper(d.mapperDB)
	row, err := deploymentMapper.FindByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return Deployment{}, mapNoRows(err)
	}
	return row.deployment(), nil
}

func (d *DB) UpdateDeployment(ctx context.Context, workspaceUUID string, externalID string, input UpdateDeploymentInput) (Deployment, error) {
	var updated Deployment
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		var err error
		updated, err = updateDeployment(ctx, executor, workspaceUUID, externalID, input)
		return err
	})
	return updated, err
}

func (d *DB) UpdateDeploymentTx(ctx context.Context, tx *yourbatis.Tx, workspaceUUID string, externalID string, input UpdateDeploymentInput) (Deployment, error) {
	return updateDeployment(ctx, tx, workspaceUUID, externalID, input)
}

func updateDeployment(ctx context.Context, executor yourbatis.Executor, workspaceUUID string, externalID string, input UpdateDeploymentInput) (Deployment, error) {
	deploymentMapper := NewDeploymentMapper(executor)
	current, err := deploymentMapper.LockByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return Deployment{}, mapNoRows(err)
	}
	if current.ArchivedAt != nil {
		return Deployment{}, ErrInvalidState
	}
	next := input.Deployment
	if input.ScheduleChanged && len(current.Schedule) == 0 && len(next.Schedule) > 0 {
		if err := checkScheduledDeploymentQuota(ctx, deploymentMapper, current.OrganizationUUID); err != nil {
			return Deployment{}, err
		}
	}

	params := deploymentWriteParamsFrom(next)
	params.WorkspaceUUID = workspaceUUID
	params.ExternalID = externalID
	params.ScheduleChanged = input.ScheduleChanged
	row, err := deploymentMapper.UpdateByExternalID(ctx, params)
	if err != nil {
		return Deployment{}, mapNoRows(err)
	}
	return row.deployment(), nil
}

func checkScheduledDeploymentQuota(ctx context.Context, mapper DeploymentMapper, organizationUUID string) error {
	if _, err := mapper.LockOrganization(ctx, organizationUUID); err != nil {
		return mapNoRows(err)
	}
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
	deploymentMapper := NewDeploymentMapper(d.mapperDB)
	row, err := deploymentMapper.ArchiveByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return Deployment{}, mapNoRows(err)
	}
	return row.deployment(), nil
}

func (d *DB) PauseDeployment(ctx context.Context, workspaceUUID string, externalID string, pausedReason json.RawMessage) (Deployment, error) {
	deploymentMapper := NewDeploymentMapper(d.mapperDB)
	row, err := deploymentMapper.PauseByExternalID(ctx, workspaceUUID, externalID, agentJSONArg(pausedReason))
	if err != nil {
		return Deployment{}, mapNoRows(err)
	}
	return row.deployment(), nil
}

func (d *DB) UnpauseDeployment(ctx context.Context, workspaceUUID string, externalID string, nextScheduledAt *time.Time) (Deployment, error) {
	return unpauseDeployment(ctx, d.mapperDB, workspaceUUID, externalID, nextScheduledAt)
}

func (d *DB) UnpauseDeploymentTx(ctx context.Context, tx *yourbatis.Tx, workspaceUUID string, externalID string, nextScheduledAt *time.Time) (Deployment, error) {
	return unpauseDeployment(ctx, tx, workspaceUUID, externalID, nextScheduledAt)
}

func unpauseDeployment(ctx context.Context, executor yourbatis.Executor, workspaceUUID string, externalID string, nextScheduledAt *time.Time) (Deployment, error) {
	mapper := NewDeploymentMapper(executor)
	row, err := mapper.UnpauseByExternalID(ctx, unpauseDeploymentParams{
		WorkspaceUUID: workspaceUUID, ExternalID: externalID, NextScheduledAt: nextScheduledAt,
	})
	if err != nil {
		return Deployment{}, mapNoRows(err)
	}
	return row.deployment(), nil
}

func (d *DB) ListDeploymentsPage(ctx context.Context, params ListDeploymentsPageParams) ([]Deployment, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	deploymentMapper := NewDeploymentMapper(d.mapperDB)
	rows, err := deploymentMapper.ListPage(ctx, deploymentPageParams(params))
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

func (d *DB) ListDeploymentSchedules(ctx context.Context) ([]DeploymentScheduleState, error) {
	deploymentMapper := NewDeploymentMapper(d.mapperDB)
	rows, err := deploymentMapper.ListActiveSchedules(ctx)
	if err != nil {
		return nil, err
	}
	return deploymentScheduleStates(rows), nil
}

func (d *DB) ListDeploymentSchedulesMissingNextScheduledAt(ctx context.Context) ([]DeploymentScheduleState, error) {
	deploymentMapper := NewDeploymentMapper(d.mapperDB)
	rows, err := deploymentMapper.ListSchedulesMissingNextScheduledAt(ctx)
	if err != nil {
		return nil, err
	}
	return deploymentScheduleStates(rows), nil
}

func deploymentScheduleStates(rows []deploymentScheduleRow) []DeploymentScheduleState {
	states := make([]DeploymentScheduleState, len(rows))
	for index, row := range rows {
		states[index] = DeploymentScheduleState{
			WorkspaceUUID: row.WorkspaceUUID, ExternalID: row.ExternalID, Schedule: copyRaw(row.Schedule),
			ScheduleRevision: row.ScheduleRevision, NextScheduledAt: row.NextScheduledAt,
		}
	}
	return states
}

func (d *DB) SetInitialDeploymentNextScheduledAt(ctx context.Context, state DeploymentScheduleState, nextScheduledAt time.Time) error {
	deploymentMapper := NewDeploymentMapper(d.mapperDB)
	_, err := deploymentMapper.SetInitialNextScheduledAt(ctx, setInitialNextScheduledAtParams{
		WorkspaceUUID: state.WorkspaceUUID, ExternalID: state.ExternalID,
		ScheduleRevision: state.ScheduleRevision, NextScheduledAt: nextScheduledAt,
	})
	return err
}

func (d *DB) CreateManualDeploymentRun(ctx context.Context, input CreateManualDeploymentRunInput) (DeploymentRun, Session, SessionThread, []SessionEvent, error) {
	var created DeploymentRun
	var session Session
	var thread SessionThread
	var events []SessionEvent
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		deploymentMapper := NewDeploymentMapper(executor)
		runMapper := NewDeploymentRunMapper(executor)
		deploymentRow, err := deploymentMapper.LockByExternalID(ctx, input.Run.WorkspaceUUID, input.DeploymentExternalID)
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

func (d *DB) ApplyScheduledOccurrence(ctx context.Context, input ApplyScheduledOccurrenceInput) (ScheduledOccurrenceResult, error) {
	var result ScheduledOccurrenceResult
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		deploymentMapper := NewDeploymentMapper(executor)
		runMapper := NewDeploymentRunMapper(executor)
		row, err := deploymentMapper.LockByExternalID(ctx, input.WorkspaceUUID, input.DeploymentExternalID)
		if err != nil {
			return mapNoRows(err)
		}
		deployment := row.deployment()
		if deployment.ArchivedAt != nil || deployment.Status != "active" ||
			deployment.ScheduleRevision != input.ScheduleRevision || deployment.NextScheduledAt == nil ||
			!deployment.NextScheduledAt.Equal(input.ScheduledAt) {
			return ErrStaleSchedule
		}
		if input.ArchiveDeployment {
			if _, err := deploymentMapper.ArchiveByExternalID(ctx, input.WorkspaceUUID, input.DeploymentExternalID); err != nil {
				return err
			}
			return enqueueWebhookDeliveryEventsTx(ctx, executor, input.WorkspaceUUID, input.WebhookEvents)
		}

		run := deploymentRunFromDeployment(input.Run, deployment)
		if input.Session != nil {
			result.Session, result.Thread, _, _, err = insertSessionTx(ctx, executor, *input.Session)
			if err != nil {
				return err
			}
			result.Events, err = insertSessionEventsTx(ctx, executor, result.Session, input.Events, false)
			if err != nil {
				return err
			}
			run.SessionExternalID = &result.Session.ExternalID
			run.Error = nil
		} else {
			run.SessionExternalID = nil
		}
		created, err := runMapper.Insert(ctx, deploymentRunWriteParamsFrom(run))
		if err != nil {
			if isUniqueViolationOnConstraint(err, "deployment_runs_schedule_occurrence_idx") {
				return ErrStaleSchedule
			}
			return err
		}
		result.Run = created.run()

		var rowsAffected int64
		if len(input.AutoPauseReason) > 0 {
			rowsAffected, err = deploymentMapper.PauseAfterScheduledRun(ctx, pauseScheduledDeploymentParams{
				WorkspaceUUID: input.WorkspaceUUID, ExternalID: input.DeploymentExternalID,
				ScheduleRevision: input.ScheduleRevision, ScheduledAt: input.ScheduledAt,
				PausedReason: agentJSONArg(input.AutoPauseReason), LastRunAt: input.Now,
			})
		} else {
			rowsAffected, err = deploymentMapper.AdvanceSchedule(ctx, advanceDeploymentScheduleParams{
				WorkspaceUUID: input.WorkspaceUUID, ExternalID: input.DeploymentExternalID,
				ScheduleRevision: input.ScheduleRevision, ScheduledAt: input.ScheduledAt,
				NextScheduledAt: input.NextScheduledAt, LastRunAt: input.Now,
			})
		}
		if err != nil {
			return err
		}
		if rowsAffected != 1 {
			return ErrStaleSchedule
		}
		return enqueueWebhookDeliveryEventsTx(ctx, executor, input.WorkspaceUUID, input.WebhookEvents)
	})
	return result, err
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
		ScheduleRevision: deployment.ScheduleRevision, NextScheduledAt: deployment.NextScheduledAt,
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
		ScheduleRevision: r.ScheduleRevision, NextScheduledAt: r.NextScheduledAt,
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
		Error: copyRaw(r.Error), TriggerType: r.TriggerType, ScheduledAt: r.ScheduledAt,
		CreatedAt: r.CreatedAt, DeletedAt: r.DeletedAt,
	}
}

package deployments

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const (
	DeploymentScheduleQueue = "deployment_schedules"
)

type scheduledDeploymentArgs struct {
	WorkspaceUUID        string             `json:"workspace_uuid"`
	DeploymentExternalID string             `json:"deployment_id"`
	Schedule             deploymentSchedule `json:"schedule"`
	ScheduledAt          time.Time          `json:"scheduled_at"`
}

func (scheduledDeploymentArgs) Kind() string { return "scheduled_deployment" }

func (scheduledDeploymentArgs) Hooks() []rivertype.Hook {
	return []rivertype.Hook{river.HookInsertBeginFunc(stampScheduledDeploymentOccurrence)}
}

var _ river.JobArgsWithHooks = scheduledDeploymentArgs{}

// River overwrites river_job.scheduled_at on retry, so the Cron occurrence is stored in job args.
func stampScheduledDeploymentOccurrence(_ context.Context, params *rivertype.JobInsertParams) error {
	if params.ScheduledAt == nil {
		return errors.New("scheduled_deployment job requires scheduled_at")
	}
	args, err := jsonx.Decode[scheduledDeploymentArgs](json.RawMessage(params.EncodedArgs))
	if err != nil {
		return err
	}
	args.ScheduledAt = params.ScheduledAt.UTC()
	encoded, err := jsonx.Encode(args)
	if err != nil {
		return err
	}
	params.EncodedArgs = encoded
	params.Args = args
	return nil
}

func occurrenceTime(job *river.Job[scheduledDeploymentArgs]) (time.Time, error) {
	if job.Args.ScheduledAt.IsZero() {
		return time.Time{}, errors.New("scheduled_deployment occurrence is missing")
	}
	return job.Args.ScheduledAt.UTC(), nil
}

type DeploymentScheduler struct {
	database *db.DB
	client   *river.Client[*sql.Tx]
	logger   *slog.Logger
}

// RegisterScheduledWorkers keeps deployment behavior separate from River assembly.
func RegisterScheduledWorkers(workers *river.Workers, database *db.DB) {
	river.AddWorker(workers, &scheduledDeploymentWorker{database: database})
}

func NewDeploymentScheduler(database *db.DB, client *river.Client[*sql.Tx], logger *slog.Logger) *DeploymentScheduler {
	return &DeploymentScheduler{database: database, client: client, logger: logging.LoggerOrDefault(logger)}
}

func (s *DeploymentScheduler) Start(ctx context.Context) error {
	s.database.SetDeploymentScheduleTxHook(s.updateTx)
	if err := s.reconcile(ctx); err != nil {
		s.database.SetDeploymentScheduleTxHook(nil)
		return fmt.Errorf("reconcile deployment schedules: %w", err)
	}
	if err := s.client.Start(ctx); err != nil {
		s.database.SetDeploymentScheduleTxHook(nil)
		return err
	}
	return nil
}

func (s *DeploymentScheduler) Stop(ctx context.Context) error {
	if err := s.client.Stop(ctx); err != nil {
		return err
	}
	s.database.SetDeploymentScheduleTxHook(nil)
	return nil
}

func (s *DeploymentScheduler) updateTx(ctx context.Context, tx *sql.Tx, deployment db.Deployment) error {
	if deployment.ArchivedAt != nil || deployment.Status != "active" || len(deployment.Schedule) == 0 {
		return s.deleteTx(ctx, tx, deployment.ExternalID)
	}
	opts, err := durableDeploymentScheduleOpts(deployment)
	if err != nil {
		return err
	}
	_, err = s.client.DurablePeriodicJobUpsertTx(ctx, tx, opts)
	return err
}

func (s *DeploymentScheduler) deleteTx(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := s.client.DurablePeriodicJobDeleteTx(ctx, tx, id)
	if errors.Is(err, rivertype.ErrNotFound) {
		return nil
	}
	return err
}

func (s *DeploymentScheduler) reconcile(ctx context.Context) error {
	states, err := s.database.ListDeploymentSchedules(ctx)
	if err != nil {
		return err
	}
	targets := make(map[string]string, len(states))
	for _, state := range states {
		targets[state.ExternalID] = state.WorkspaceUUID
	}
	rows, err := s.client.DurablePeriodicJobList(ctx, nil)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Kind != (scheduledDeploymentArgs{}).Kind() {
			continue
		}
		if _, ok := targets[row.ID]; ok {
			continue
		}
		args, err := jsonx.Decode[scheduledDeploymentArgs](json.RawMessage(row.Args))
		if err != nil {
			s.logger.WarnContext(ctx, "skip invalid durable deployment schedule", "deployment_id", row.ID, "error", err)
			continue
		}
		targets[row.ID] = args.WorkspaceUUID
	}
	for id, workspaceUUID := range targets {
		if err := s.database.ReconcileDeploymentSchedule(ctx, workspaceUUID, id, s.reconcileTx); err != nil {
			return fmt.Errorf("deployment %s: %w", id, err)
		}
	}
	return nil
}

func (s *DeploymentScheduler) reconcileTx(ctx context.Context, tx *sql.Tx, deployment db.Deployment) error {
	if deployment.ArchivedAt != nil || deployment.Status != "active" || len(deployment.Schedule) == 0 {
		return s.deleteTx(ctx, tx, deployment.ExternalID)
	}
	opts, err := durableDeploymentScheduleOpts(deployment)
	if err != nil {
		s.logger.WarnContext(ctx, "skip invalid stored deployment schedule", "deployment_id", deployment.ExternalID, "error", err)
		return s.deleteTx(ctx, tx, deployment.ExternalID)
	}
	row, err := s.client.DurablePeriodicJobGetTx(ctx, tx, deployment.ExternalID)
	if err == nil && durableDeploymentScheduleMatches(row, opts) {
		return nil
	}
	if err != nil && !errors.Is(err, rivertype.ErrNotFound) {
		return err
	}
	_, err = s.client.DurablePeriodicJobUpsertTx(ctx, tx, opts)
	return err
}

func durableDeploymentScheduleOpts(deployment db.Deployment) (*river.DurablePeriodicJobUpsertOpts, error) {
	parsed, err := parseDeploymentSchedule(deployment.Schedule)
	if err != nil {
		return nil, err
	}
	args, err := jsonx.Encode(scheduledDeploymentArgs{
		WorkspaceUUID:        deployment.WorkspaceUUID,
		DeploymentExternalID: deployment.ExternalID,
		Schedule:             parsed.config,
	})
	if err != nil {
		return nil, err
	}
	return &river.DurablePeriodicJobUpsertOpts{
		Args:        args,
		ID:          deployment.ExternalID,
		Kind:        scheduledDeploymentArgs{}.Kind(),
		MaxAttempts: river.MaxAttemptsDefault,
		Priority:    river.PriorityDefault,
		Queue:       DeploymentScheduleQueue,
		Schedule: &river.DurablePeriodicJobSchedule{
			CronExpression: mapSundaySeven(parsed.config.Expression),
			CronTimezone:   parsed.config.Timezone,
		},
	}, nil
}

func durableDeploymentScheduleMatches(row *rivertype.DurablePeriodicJob, opts *river.DurablePeriodicJobUpsertOpts) bool {
	persistedArgs, err := jsonx.Decode[scheduledDeploymentArgs](json.RawMessage(row.Args))
	if err != nil {
		return false
	}
	desiredArgs, err := jsonx.Decode[scheduledDeploymentArgs](json.RawMessage(opts.Args))
	return row.ID == opts.ID &&
		err == nil && persistedArgs == desiredArgs &&
		row.CronExpression != nil &&
		*row.CronExpression == opts.Schedule.CronExpression &&
		row.CronTimezone == opts.Schedule.CronTimezone &&
		row.Kind == opts.Kind &&
		row.MaxAttempts == opts.MaxAttempts &&
		row.PausedAt == nil &&
		row.Priority == opts.Priority &&
		row.Queue == opts.Queue &&
		len(row.Tags) == 0
}

type scheduledDeploymentWorker struct {
	river.WorkerDefaults[scheduledDeploymentArgs]
	database *db.DB
}

func (w *scheduledDeploymentWorker) Work(ctx context.Context, job *river.Job[scheduledDeploymentArgs]) error {
	args := job.Args
	scheduledAt, err := occurrenceTime(job)
	if err != nil {
		return river.JobCancel(err)
	}
	deployment, err := w.database.GetDeployment(ctx, args.WorkspaceUUID, args.DeploymentExternalID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	if deployment.ArchivedAt != nil || deployment.Status != "active" {
		return nil
	}
	currentSchedule, err := parseDeploymentSchedule(deployment.Schedule)
	if err != nil || currentSchedule.config != args.Schedule {
		return nil
	}
	now := time.Now().UTC()

	agent, agentErr := w.database.GetAgent(ctx, deployment.WorkspaceUUID, deployment.AgentExternalID)
	if errors.Is(agentErr, db.ErrNotFound) || agent.ArchivedAt != nil {
		return w.applyOccurrence(ctx, db.ApplyScheduledOccurrenceInput{
			Deployment: deployment, ScheduledAt: scheduledAt, ArchiveDeployment: true,
		})
	}
	if agentErr != nil {
		return agentErr
	}

	referenceFailure, err := validateRunDependencies(ctx, w.database, deployment.WorkspaceUUID, deployment)
	if err != nil {
		return err
	}
	if referenceFailure != nil {
		return w.recordFailure(ctx, deployment, referenceFailure, scheduledAt, now)
	}
	preparedRun, err := prepareDeploymentExecution(deployment, deployment.CreatedByAPIKeyUUID, deployment.RuntimeUserUUID, now)
	if err != nil {
		if errors.Is(err, errRetryableRunPreparation) {
			return err
		}
		return w.recordFailure(ctx, deployment, runError("session_resource_not_found_error", err.Error()), scheduledAt, now)
	}
	err = w.applyOccurrence(ctx, db.ApplyScheduledOccurrenceInput{
		Deployment: deployment, ScheduledAt: scheduledAt,
		Session: &preparedRun.Session, Events: preparedRun.Events,
		Run: db.DeploymentRun{
			UUID: uuid.NewV4().String(), ExternalID: preparedRun.RunID,
		},
		Now: now,
	})
	if errors.Is(err, db.ErrWorkspaceArchived) {
		return w.recordFailure(ctx, deployment, runError("workspace_archived_error", "Workspace is archived"), scheduledAt, now)
	}
	if errors.Is(err, db.ErrFileReferenceNotFound) {
		return w.recordFailure(ctx, deployment, runErrorForReference("file", db.ErrNotFound, false), scheduledAt, now)
	}
	if errors.Is(err, db.ErrFilestorePathExists) {
		return w.recordFailure(ctx, deployment, runError("session_creation_rejected_error", "Session resource paths conflict"), scheduledAt, now)
	}
	return err
}

func (w *scheduledDeploymentWorker) recordFailure(
	ctx context.Context,
	deployment db.Deployment,
	failure *deploymentRunError,
	scheduledAt time.Time,
	now time.Time,
) error {
	runID, err := ids.New("drun_")
	if err != nil {
		return err
	}
	runErrorJSON, err := jsonx.Encode(failure)
	if err != nil {
		return err
	}
	var pausedReasonJSON json.RawMessage
	if shouldAutoPause(failure) {
		pausedReasonJSON, err = jsonx.Encode(deploymentPausedReason{Type: "error", Error: failure})
		if err != nil {
			return err
		}
	}
	return w.applyOccurrence(ctx, db.ApplyScheduledOccurrenceInput{
		Deployment: deployment, ScheduledAt: scheduledAt,
		Run: db.DeploymentRun{
			UUID: uuid.NewV4().String(), ExternalID: runID, Error: runErrorJSON,
		},
		AutoPauseReason: pausedReasonJSON, Now: now,
	})
}

func (w *scheduledDeploymentWorker) applyOccurrence(
	ctx context.Context,
	input db.ApplyScheduledOccurrenceInput,
) error {
	err := w.database.ApplyScheduledOccurrence(ctx, input)
	if errors.Is(err, db.ErrStaleSchedule) {
		return nil
	}
	return err
}

func shouldAutoPause(runError *deploymentRunError) bool {
	switch runError.Type {
	case "environment_archived_error",
		"agent_archived_error",
		"environment_not_found_error",
		"vault_not_found_error",
		"file_not_found_error",
		"session_resource_not_found_error",
		"workspace_archived_error",
		"organization_disabled_error",
		"memory_store_archived_error",
		"skill_not_found_error",
		"vault_archived_error",
		"unknown_error",
		"self_hosted_resources_unsupported_error",
		"mcp_egress_blocked_error":
		return true
	default:
		return false
	}
}

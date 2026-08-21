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
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const (
	riverSchema                    = "public"
	deploymentScheduleQueue        = "deployment_schedules"
	deploymentScheduleSyncInterval = 10 * time.Second
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
	database   *db.DB
	client     *river.Client[*sql.Tx]
	logger     *slog.Logger
	registered map[string]deploymentSchedule
	cancel     context.CancelFunc
	done       chan struct{}
}

func MigrateRiver(ctx context.Context, database *db.DB, logger *slog.Logger) error {
	migrator, err := rivermigrate.New(riverdatabasesql.New(database.SQLDB()), &rivermigrate.Config{
		Schema: riverSchema, Logger: logging.LoggerOrDefault(logger),
	})
	if err != nil {
		return err
	}
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}

func NewDeploymentScheduler(database *db.DB, logger *slog.Logger) (*DeploymentScheduler, error) {
	logger = logging.LoggerOrDefault(logger)
	workers := river.NewWorkers()
	worker := &scheduledDeploymentWorker{database: database}
	river.AddWorker(workers, worker)
	client, err := river.NewClient(riverdatabasesql.New(database.SQLDB()), &river.Config{
		Schema: riverSchema,
		Queues: map[string]river.QueueConfig{
			deploymentScheduleQueue: {MaxWorkers: 10},
		},
		Workers:         workers,
		Logger:          logger,
		PollOnly:        true,
		JobTimeout:      2 * time.Minute,
		SoftStopTimeout: 10 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return &DeploymentScheduler{
		database: database, client: client, logger: logger, registered: make(map[string]deploymentSchedule),
	}, nil
}

func (s *DeploymentScheduler) Start(ctx context.Context) error {
	if err := s.sync(ctx); err != nil {
		return fmt.Errorf("load deployment schedules: %w", err)
	}
	ctx, s.cancel = context.WithCancel(ctx)
	if err := s.client.Start(ctx); err != nil {
		s.cancel()
		return err
	}
	s.done = make(chan struct{})
	go s.syncLoop(ctx)
	return nil
}

func (s *DeploymentScheduler) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.done != nil {
		select {
		case <-s.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.client.Stop(ctx)
}

func (s *DeploymentScheduler) sync(ctx context.Context) error {
	states, err := s.database.ListDeploymentSchedules(ctx)
	if err != nil {
		return err
	}
	desired := make(map[string]struct{}, len(states))
	for _, state := range states {
		schedule, err := parseDeploymentSchedule(state.Schedule)
		if err != nil {
			s.client.PeriodicJobs().RemoveByID(state.ExternalID)
			delete(s.registered, state.ExternalID)
			s.logger.WarnContext(ctx, "skip invalid stored deployment schedule", "deployment_id", state.ExternalID, "error", err)
			continue
		}
		desired[state.ExternalID] = struct{}{}
		if registeredSchedule, ok := s.registered[state.ExternalID]; ok && registeredSchedule == schedule.config {
			continue
		}
		job := river.NewPeriodicJob(schedule.cron, func() (river.JobArgs, *river.InsertOpts) {
			return scheduledDeploymentArgs{
				WorkspaceUUID: state.WorkspaceUUID, DeploymentExternalID: state.ExternalID,
				Schedule: schedule.config,
			}, &river.InsertOpts{Queue: deploymentScheduleQueue}
		}, &river.PeriodicJobOpts{ID: state.ExternalID})
		s.client.PeriodicJobs().RemoveByID(state.ExternalID)
		if _, err := s.client.PeriodicJobs().AddSafely(job); err != nil {
			delete(s.registered, state.ExternalID)
			return fmt.Errorf("deployment %s: %w", state.ExternalID, err)
		}
		s.registered[state.ExternalID] = schedule.config
	}
	for id := range s.registered {
		if _, ok := desired[id]; !ok {
			s.client.PeriodicJobs().RemoveByID(id)
			delete(s.registered, id)
		}
	}
	return nil
}

func (s *DeploymentScheduler) syncLoop(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(deploymentScheduleSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.sync(ctx); err != nil {
				s.logger.ErrorContext(ctx, "sync deployment periodic jobs", "error", err)
			}
		}
	}
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
	preparedRun, err := prepareDeploymentExecution(deployment, deployment.CreatedByAPIKeyUUID, now)
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

package deployments

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
	"github.com/superduck-ai/yourbatis"
)

const (
	riverSchema               = "public"
	deploymentScheduleQueue   = "deployment_schedules"
	scheduleReconcileInterval = 30 * time.Second
)

var errInvalidDeploymentSchedule = errors.New("invalid deployment schedule")

type scheduledDeploymentArgs struct {
	WorkspaceUUID        string    `json:"workspace_uuid" river:"unique"`
	DeploymentExternalID string    `json:"deployment_id" river:"unique"`
	ScheduleRevision     int64     `json:"schedule_revision" river:"unique"`
	ScheduledAt          time.Time `json:"scheduled_at" river:"unique"`
}

func (scheduledDeploymentArgs) Kind() string { return "scheduled_deployment" }

type DeploymentScheduler struct {
	database *db.DB
	client   *river.Client[*sql.Tx]
	logger   *slog.Logger
}

func MigrateRiver(ctx context.Context, database *db.DB, logger *slog.Logger) error {
	migrator, err := rivermigrate.New(riverdatabasesql.New(database.RiverSQLDB()), &rivermigrate.Config{
		Schema: riverSchema, Logger: logging.LoggerOrDefault(logger),
	})
	if err != nil {
		return err
	}
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}

func NewDeploymentScheduler(database *db.DB, webhookEvents *webhooks.Enqueuer, logger *slog.Logger) (*DeploymentScheduler, error) {
	logger = logging.LoggerOrDefault(logger)
	workers := river.NewWorkers()
	worker := &scheduledDeploymentWorker{database: database, webhooks: webhookEvents, logger: logger}
	if err := river.AddWorkerSafely(workers, worker); err != nil {
		return nil, err
	}
	client, err := river.NewClient(riverdatabasesql.New(database.RiverSQLDB()), &river.Config{
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
	return &DeploymentScheduler{database: database, client: client, logger: logger}, nil
}

func (s *DeploymentScheduler) Start(ctx context.Context) error {
	if err := s.backfillNextScheduledAt(ctx); err != nil {
		return fmt.Errorf("backfill deployment next scheduled time: %w", err)
	}
	if err := s.reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile deployment schedules: %w", err)
	}
	if err := s.client.Start(ctx); err != nil {
		return err
	}
	go s.reconcileLoop(ctx)
	return nil
}

func (s *DeploymentScheduler) Stop(ctx context.Context) error {
	return s.client.Stop(ctx)
}

func (s *DeploymentScheduler) Enqueue(ctx context.Context, deployment db.Deployment) error {
	args, opts, err := scheduledDeploymentJob(deployment)
	if err != nil || opts == nil {
		return err
	}
	_, err = s.client.Insert(ctx, args, opts)
	return err
}

func (s *DeploymentScheduler) EnqueueTx(ctx context.Context, yourbatisTx *yourbatis.Tx, deployment db.Deployment) error {
	if yourbatisTx == nil {
		return errors.New("yourbatis transaction is nil")
	}
	args, opts, err := scheduledDeploymentJob(deployment)
	if err != nil || opts == nil {
		return err
	}
	_, err = s.client.InsertTx(ctx, yourbatisTx.SQLTx(), args, opts)
	return err
}

func scheduledDeploymentJob(deployment db.Deployment) (scheduledDeploymentArgs, *river.InsertOpts, error) {
	if deployment.NextScheduledAt == nil {
		return scheduledDeploymentArgs{}, nil, nil
	}
	triggerAt, err := jitteredTriggerAt(deployment.ExternalID, deployment.Schedule, *deployment.NextScheduledAt)
	if err != nil {
		return scheduledDeploymentArgs{}, nil, fmt.Errorf("%w: %v", errInvalidDeploymentSchedule, err)
	}
	return scheduledDeploymentArgs{
			WorkspaceUUID: deployment.WorkspaceUUID, DeploymentExternalID: deployment.ExternalID,
			ScheduleRevision: deployment.ScheduleRevision, ScheduledAt: *deployment.NextScheduledAt,
		}, &river.InsertOpts{
			Queue: deploymentScheduleQueue, ScheduledAt: triggerAt,
			UniqueOpts: river.UniqueOpts{ByArgs: true},
		}, nil
}

func (s *DeploymentScheduler) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(scheduleReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.reconcile(ctx); err != nil {
				s.logger.ErrorContext(ctx, "reconcile deployment schedules", "error", err)
			}
		}
	}
}

func (s *DeploymentScheduler) reconcile(ctx context.Context) error {
	states, err := s.database.ListDeploymentSchedules(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, state := range states {
		err := s.Enqueue(ctx, db.Deployment{
			WorkspaceUUID: state.WorkspaceUUID, ExternalID: state.ExternalID, Schedule: state.Schedule,
			ScheduleRevision: state.ScheduleRevision, NextScheduledAt: state.NextScheduledAt,
		})
		if err != nil {
			if errors.Is(err, errInvalidDeploymentSchedule) {
				s.logger.ErrorContext(ctx, "skip invalid stored deployment schedule", "deployment_id", state.ExternalID, "error", err)
				continue
			}
			errs = append(errs, fmt.Errorf("deployment %s: %w", state.ExternalID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *DeploymentScheduler) backfillNextScheduledAt(ctx context.Context) error {
	states, err := s.database.ListDeploymentSchedulesMissingNextScheduledAt(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, state := range states {
		next, err := nextScheduledAt(state.Schedule, now)
		if err != nil {
			s.logger.ErrorContext(ctx, "skip invalid stored deployment schedule", "deployment_id", state.ExternalID, "error", err)
			continue
		}
		if next != nil {
			if err := s.database.SetInitialDeploymentNextScheduledAt(ctx, state, *next); err != nil {
				return err
			}
		}
	}
	return nil
}

type scheduledDeploymentWorker struct {
	river.WorkerDefaults[scheduledDeploymentArgs]
	database *db.DB
	webhooks *webhooks.Enqueuer
	logger   *slog.Logger
}

func (w *scheduledDeploymentWorker) Work(ctx context.Context, job *river.Job[scheduledDeploymentArgs]) error {
	args := job.Args
	deployment, err := w.database.GetDeployment(ctx, args.WorkspaceUUID, args.DeploymentExternalID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	if deployment.ArchivedAt != nil || deployment.Status != "active" ||
		deployment.ScheduleRevision != args.ScheduleRevision || deployment.NextScheduledAt == nil ||
		!deployment.NextScheduledAt.Equal(args.ScheduledAt) {
		return nil
	}
	nextScheduledAt, err := nextAfterScheduled(deployment.Schedule, args.ScheduledAt)
	if err != nil {
		return err
	}

	agent, agentErr := w.database.GetAgent(ctx, deployment.WorkspaceUUID, deployment.AgentExternalID)
	if errors.Is(agentErr, db.ErrNotFound) || (agentErr == nil && agent.ArchivedAt != nil) {
		webhookEvents, err := w.prepareWebhookEvents(ctx, deployment, time.Now().UTC(), []webhooks.EnqueueInput{{
			EventType: "deployment.archived", ResourceID: deployment.ExternalID,
		}})
		if err != nil {
			return err
		}
		_, err = w.database.ApplyScheduledOccurrence(ctx, db.ApplyScheduledOccurrenceInput{
			WorkspaceUUID: deployment.WorkspaceUUID, DeploymentExternalID: deployment.ExternalID,
			ScheduleRevision: args.ScheduleRevision, ScheduledAt: args.ScheduledAt, ArchiveDeployment: true,
			WebhookEvents: webhookEvents,
		})
		if errors.Is(err, db.ErrStaleSchedule) {
			return nil
		}
		return err
	}
	if agentErr != nil {
		return agentErr
	}

	now := time.Now().UTC()
	referenceFailure, err := validateRunReferences(ctx, w.database, deployment.WorkspaceUUID, deployment)
	if err != nil {
		return err
	}
	if referenceFailure != nil {
		return w.recordFailure(ctx, deployment, args, nextScheduledAt, referenceFailure, now)
	}
	preparedRun, err := prepareDeploymentRun(deployment, now)
	if err != nil {
		if errors.Is(err, errRetryableRunPreparation) {
			return err
		}
		return w.recordFailure(ctx, deployment, args, nextScheduledAt, runError("session_resource_not_found_error", err.Error()), now)
	}
	webhookEvents, err := w.prepareRunWebhookEvents(ctx, deployment, preparedRun, now)
	if err != nil {
		return err
	}
	_, err = w.database.ApplyScheduledOccurrence(ctx, db.ApplyScheduledOccurrenceInput{
		WorkspaceUUID: deployment.WorkspaceUUID, DeploymentExternalID: deployment.ExternalID,
		ScheduleRevision: args.ScheduleRevision, ScheduledAt: args.ScheduledAt, NextScheduledAt: nextScheduledAt,
		Session: &preparedRun.Session, Events: preparedRun.Events,
		Run: db.DeploymentRun{
			UUID: uuid.NewString(), ExternalID: preparedRun.RunID,
			OrganizationUUID: deployment.OrganizationUUID, WorkspaceUUID: deployment.WorkspaceUUID,
			CreatedByAPIKeyUUID: deployment.CreatedByAPIKeyUUID, TriggerType: "schedule",
			ScheduledAt: &args.ScheduledAt, CreatedAt: now,
		},
		WebhookEvents: webhookEvents, Now: now,
	})
	if errors.Is(err, db.ErrStaleSchedule) {
		return nil
	}
	if errors.Is(err, db.ErrWorkspaceArchived) {
		return w.recordFailure(ctx, deployment, args, nextScheduledAt, runError("workspace_archived_error", "Workspace is archived"), now)
	}
	if errors.Is(err, db.ErrFileReferenceNotFound) {
		return w.recordFailure(ctx, deployment, args, nextScheduledAt, runErrorForReference("file", db.ErrNotFound, false), now)
	}
	if errors.Is(err, db.ErrFilestorePathExists) {
		return w.recordFailure(ctx, deployment, args, nextScheduledAt, runError("session_creation_rejected_error", "Session resource paths conflict"), now)
	}
	if err != nil {
		return err
	}
	return nil
}

func (w *scheduledDeploymentWorker) recordFailure(ctx context.Context, deployment db.Deployment, args scheduledDeploymentArgs, nextScheduledAt *time.Time, runErrorJSON json.RawMessage, now time.Time) error {
	runID, err := ids.New("drun_")
	if err != nil {
		return err
	}
	var pausedReason json.RawMessage
	if shouldAutoPause(runErrorJSON) {
		pausedReason, err = httpapi.MarshalRaw(map[string]any{"type": "error", "error": runErrorJSON})
		if err != nil {
			return err
		}
	}
	webhookEvents, err := w.prepareWebhookEvents(ctx, deployment, now, scheduledFailureWebhookInputs(
		runID, deployment.ExternalID, len(pausedReason) > 0,
	))
	if err != nil {
		return err
	}
	_, err = w.database.ApplyScheduledOccurrence(ctx, db.ApplyScheduledOccurrenceInput{
		WorkspaceUUID: deployment.WorkspaceUUID, DeploymentExternalID: deployment.ExternalID,
		ScheduleRevision: args.ScheduleRevision, ScheduledAt: args.ScheduledAt, NextScheduledAt: nextScheduledAt,
		Run: db.DeploymentRun{
			UUID: uuid.NewString(), ExternalID: runID,
			OrganizationUUID: deployment.OrganizationUUID, WorkspaceUUID: deployment.WorkspaceUUID,
			CreatedByAPIKeyUUID: deployment.CreatedByAPIKeyUUID, Error: runErrorJSON, TriggerType: "schedule",
			ScheduledAt: &args.ScheduledAt, CreatedAt: now,
		},
		AutoPauseReason: pausedReason, WebhookEvents: webhookEvents, Now: now,
	})
	if errors.Is(err, db.ErrStaleSchedule) {
		return nil
	}
	if err != nil {
		return err
	}
	return nil
}

func scheduledFailureWebhookInputs(runID, deploymentID string, autoPause bool) []webhooks.EnqueueInput {
	inputs := []webhooks.EnqueueInput{
		{EventType: "deployment_run.started", ResourceID: runID},
		{EventType: "deployment_run.failed", ResourceID: runID},
	}
	if autoPause {
		inputs = append(inputs, webhooks.EnqueueInput{EventType: "deployment.paused", ResourceID: deploymentID})
	}
	return inputs
}

func shouldAutoPause(raw json.RawMessage) bool {
	var value struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	switch value.Type {
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

func (w *scheduledDeploymentWorker) prepareRunWebhookEvents(ctx context.Context, deployment db.Deployment, run preparedDeploymentRun, createdAt time.Time) ([]db.WebhookDeliveryEvent, error) {
	threadID := run.Session.Thread.ExternalID
	inputs := []webhooks.EnqueueInput{
		{EventType: "deployment_run.started", ResourceID: run.RunID},
		{EventType: "deployment_run.succeeded", ResourceID: run.RunID},
		{EventType: "session.created", ResourceID: run.Session.Session.ExternalID},
		{EventType: "session.pending", ResourceID: run.Session.Session.ExternalID},
		{EventType: "session.status_idled", ResourceID: run.Session.Session.ExternalID},
		{EventType: "session.thread_created", ResourceID: run.Session.Session.ExternalID, Options: webhooks.EventOptions{SessionThreadID: &threadID}},
		{EventType: "session.thread_idled", ResourceID: run.Session.Session.ExternalID, Options: webhooks.EventOptions{SessionThreadID: &threadID}},
	}
	if outcomesChanged(run.Events) {
		inputs = append(inputs, webhooks.EnqueueInput{EventType: "session.outcome_evaluation_ended", ResourceID: run.Session.Session.ExternalID})
	}
	return w.prepareWebhookEvents(ctx, deployment, createdAt, inputs)
}

func (w *scheduledDeploymentWorker) prepareWebhookEvents(ctx context.Context, deployment db.Deployment, createdAt time.Time, inputs []webhooks.EnqueueInput) ([]db.WebhookDeliveryEvent, error) {
	if w.webhooks == nil {
		return nil, nil
	}
	identifiers, err := w.database.GetWorkspaceIdentifiers(ctx, deployment.WorkspaceUUID)
	if err != nil {
		return nil, err
	}
	events := make([]db.WebhookDeliveryEvent, 0, len(inputs))
	for _, input := range inputs {
		input.WorkspaceUUID = deployment.WorkspaceUUID
		input.OrganizationUUID = deployment.OrganizationUUID
		input.WorkspaceExternalID = identifiers.WorkspaceExternalID
		event, err := w.webhooks.PrepareDeliveryEvent(input, createdAt)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

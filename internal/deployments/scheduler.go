package deployments

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
	riverSchema                    = "public"
	deploymentScheduleQueue        = "deployment_schedules"
	deploymentScheduleSyncInterval = 10 * time.Second
)

var errInvalidDeploymentSchedule = errors.New("invalid deployment schedule")

type scheduledDeploymentArgs struct {
	WorkspaceUUID        string `json:"workspace_uuid"`
	DeploymentExternalID string `json:"deployment_id"`
	ScheduleRevision     int64  `json:"schedule_revision"`
}

func (scheduledDeploymentArgs) Kind() string { return "scheduled_deployment" }

type DeploymentScheduler struct {
	database   *db.DB
	client     *river.Client[*sql.Tx]
	logger     *slog.Logger
	mu         sync.Mutex
	registered map[string]int64
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

func NewDeploymentScheduler(database *db.DB, webhookEvents *webhooks.Enqueuer, logger *slog.Logger) (*DeploymentScheduler, error) {
	logger = logging.LoggerOrDefault(logger)
	workers := river.NewWorkers()
	worker := &scheduledDeploymentWorker{database: database, webhooks: webhookEvents}
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
		database: database, client: client, logger: logger, registered: make(map[string]int64),
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

func (s *DeploymentScheduler) Update(ctx context.Context, deployment db.Deployment) {
	state := db.DeploymentSchedule{
		WorkspaceUUID: deployment.WorkspaceUUID, ExternalID: deployment.ExternalID,
		Schedule: deployment.Schedule, ScheduleRevision: deployment.ScheduleRevision,
	}
	if deployment.ArchivedAt != nil || deployment.Status != "active" {
		state.Schedule = nil
	}
	s.mu.Lock()
	err := s.updateLocked(state)
	s.mu.Unlock()
	if err != nil {
		s.logger.ErrorContext(ctx, "update deployment periodic job", "deployment_id", deployment.ExternalID, "error", err)
	}
}

func (s *DeploymentScheduler) sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	states, err := s.database.ListDeploymentSchedules(ctx)
	if err != nil {
		return err
	}
	desired := make(map[string]struct{}, len(states))
	for _, state := range states {
		if err := s.updateLocked(state); err != nil {
			if errors.Is(err, errInvalidDeploymentSchedule) {
				state.Schedule = nil
				_ = s.updateLocked(state)
				s.logger.ErrorContext(ctx, "skip invalid stored deployment schedule", "deployment_id", state.ExternalID, "error", err)
				continue
			}
			return fmt.Errorf("deployment %s: %w", state.ExternalID, err)
		}
		desired[state.ExternalID] = struct{}{}
	}
	for id := range s.registered {
		if _, ok := desired[id]; !ok {
			s.client.PeriodicJobs().RemoveByID(id)
			delete(s.registered, id)
		}
	}
	return nil
}

func (s *DeploymentScheduler) updateLocked(state db.DeploymentSchedule) error {
	if len(state.Schedule) == 0 {
		s.client.PeriodicJobs().RemoveByID(state.ExternalID)
		delete(s.registered, state.ExternalID)
		return nil
	}
	if revision, ok := s.registered[state.ExternalID]; ok && revision == state.ScheduleRevision {
		return nil
	}
	schedule, err := parseDeploymentSchedule(state.Schedule)
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidDeploymentSchedule, err)
	}
	job := river.NewPeriodicJob(schedule.cron, func() (river.JobArgs, *river.InsertOpts) {
		return scheduledDeploymentArgs{
			WorkspaceUUID: state.WorkspaceUUID, DeploymentExternalID: state.ExternalID,
			ScheduleRevision: state.ScheduleRevision,
		}, &river.InsertOpts{Queue: deploymentScheduleQueue}
	}, &river.PeriodicJobOpts{ID: state.ExternalID})
	s.client.PeriodicJobs().RemoveByID(state.ExternalID)
	if _, err := s.client.PeriodicJobs().AddSafely(job); err != nil {
		delete(s.registered, state.ExternalID)
		return err
	}
	s.registered[state.ExternalID] = state.ScheduleRevision
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
	webhooks *webhooks.Enqueuer
}

func (w *scheduledDeploymentWorker) Work(ctx context.Context, job *river.Job[scheduledDeploymentArgs]) error {
	args := job.Args
	scheduledAt := job.ScheduledAt.UTC()
	deployment, err := w.database.GetDeployment(ctx, args.WorkspaceUUID, args.DeploymentExternalID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	if deployment.ArchivedAt != nil || deployment.Status != "active" ||
		deployment.ScheduleRevision != args.ScheduleRevision {
		return nil
	}
	now := time.Now().UTC()

	agent, agentErr := w.database.GetAgent(ctx, deployment.WorkspaceUUID, deployment.AgentExternalID)
	if errors.Is(agentErr, db.ErrNotFound) || (agentErr == nil && agent.ArchivedAt != nil) {
		webhookEvents, err := w.prepareWebhookEvents(ctx, deployment, now, []webhooks.EnqueueInput{{
			EventType: "deployment.archived", ResourceID: deployment.ExternalID,
		}})
		if err != nil {
			return err
		}
		err = w.applyOccurrence(ctx, job, db.ApplyScheduledOccurrenceInput{
			WorkspaceUUID: deployment.WorkspaceUUID, DeploymentExternalID: deployment.ExternalID,
			ScheduleRevision: args.ScheduleRevision, ScheduledAt: scheduledAt, ArchiveDeployment: true,
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

	referenceFailure, err := validateRunReferences(ctx, w.database, deployment.WorkspaceUUID, deployment)
	if err != nil {
		return err
	}
	if referenceFailure != nil {
		return w.recordFailure(ctx, job, deployment, referenceFailure, now)
	}
	preparedRun, err := prepareDeploymentRun(deployment, now)
	if err != nil {
		if errors.Is(err, errRetryableRunPreparation) {
			return err
		}
		return w.recordFailure(ctx, job, deployment, runError("session_resource_not_found_error", err.Error()), now)
	}
	webhookEvents, err := w.prepareRunWebhookEvents(ctx, deployment, preparedRun, now)
	if err != nil {
		return err
	}
	err = w.applyOccurrence(ctx, job, db.ApplyScheduledOccurrenceInput{
		WorkspaceUUID: deployment.WorkspaceUUID, DeploymentExternalID: deployment.ExternalID,
		ScheduleRevision: args.ScheduleRevision, ScheduledAt: scheduledAt,
		Session: &preparedRun.Session, Events: preparedRun.Events,
		Run: db.DeploymentRun{
			UUID: uuid.NewString(), ExternalID: preparedRun.RunID,
		},
		WebhookEvents: webhookEvents, Now: now,
	})
	if errors.Is(err, db.ErrStaleSchedule) {
		return nil
	}
	if errors.Is(err, db.ErrWorkspaceArchived) {
		return w.recordFailure(ctx, job, deployment, runError("workspace_archived_error", "Workspace is archived"), now)
	}
	if errors.Is(err, db.ErrFileReferenceNotFound) {
		return w.recordFailure(ctx, job, deployment, runErrorForReference("file", db.ErrNotFound, false), now)
	}
	if errors.Is(err, db.ErrFilestorePathExists) {
		return w.recordFailure(ctx, job, deployment, runError("session_creation_rejected_error", "Session resource paths conflict"), now)
	}
	return err
}

func (w *scheduledDeploymentWorker) recordFailure(
	ctx context.Context,
	job *river.Job[scheduledDeploymentArgs],
	deployment db.Deployment,
	runErrorJSON json.RawMessage,
	now time.Time,
) error {
	args := job.Args
	scheduledAt := job.ScheduledAt.UTC()
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
	webhookInputs := []webhooks.EnqueueInput{
		{EventType: "deployment_run.started", ResourceID: runID},
		{EventType: "deployment_run.failed", ResourceID: runID},
	}
	if len(pausedReason) > 0 {
		webhookInputs = append(webhookInputs, webhooks.EnqueueInput{EventType: "deployment.paused", ResourceID: deployment.ExternalID})
	}
	webhookEvents, err := w.prepareWebhookEvents(ctx, deployment, now, webhookInputs)
	if err != nil {
		return err
	}
	err = w.applyOccurrence(ctx, job, db.ApplyScheduledOccurrenceInput{
		WorkspaceUUID: deployment.WorkspaceUUID, DeploymentExternalID: deployment.ExternalID,
		ScheduleRevision: args.ScheduleRevision, ScheduledAt: scheduledAt,
		Run: db.DeploymentRun{
			UUID: uuid.NewString(), ExternalID: runID, Error: runErrorJSON,
		},
		AutoPauseReason: pausedReason, WebhookEvents: webhookEvents, Now: now,
	})
	if errors.Is(err, db.ErrStaleSchedule) {
		return nil
	}
	return err
}

func (w *scheduledDeploymentWorker) applyOccurrence(
	ctx context.Context,
	job *river.Job[scheduledDeploymentArgs],
	input db.ApplyScheduledOccurrenceInput,
) error {
	return w.database.Transaction(ctx, func(tx *yourbatis.Tx) error {
		if err := w.database.ApplyScheduledOccurrenceTx(ctx, tx, input); err != nil {
			return err
		}
		_, err := river.JobCompleteTx[*riverdatabasesql.Driver](ctx, tx.SQLTx(), job)
		return err
	})
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

package evaluations

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/riverjobs"
)

const (
	JobQueue          = "evaluations"
	settleJobKind     = "evaluation_task_settle"
	sweepJobID        = "evaluation_task_sweep"
	sweepCron         = "* * * * *"
	sweepTimezone     = "UTC"
	sweepLimit        = 100
	staleItemAge      = 2 * time.Minute
	settleTaskAge     = time.Minute
	settleJobTimeout  = 10 * time.Minute
	settleMaxAttempts = 3
)

var errJobsUnconfigured = errors.New("evaluation jobs are not configured")

type settleArgs struct {
	OrganizationUUID string `json:"organization_uuid"`
	WorkspaceUUID    string `json:"workspace_uuid"`
	TaskID           string `json:"task_id"`
}

func (settleArgs) Kind() string { return settleJobKind }

func (settleArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: JobQueue, MaxAttempts: settleMaxAttempts,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
			rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateScheduled,
		}},
	}
}

type sweepArgs struct{}

func (sweepArgs) Kind() string { return sweepJobID }

type Jobs struct {
	handler *Handler
	client  *river.Client[*sql.Tx]
}

func NewJobs(handler *Handler) *Jobs {
	jobs := &Jobs{handler: handler}
	handler.WithSettleQueue(jobs)
	return jobs
}

func (j *Jobs) Register(workers *river.Workers) {
	river.AddWorker(workers, &settleWorker{handler: j.handler})
	river.AddWorker(workers, &sweepWorker{handler: j.handler})
}

func (j *Jobs) Configure(ctx context.Context, client *river.Client[*sql.Tx]) error {
	j.client = client
	existing, err := client.DurablePeriodicJobGet(ctx, sweepJobID)
	if err != nil && !errors.Is(err, river.ErrNotFound) {
		return err
	}
	if err == nil && riverjobs.MatchesActiveCron(existing, sweepJobID, JobQueue, sweepCron, sweepTimezone) {
		return nil
	}
	_, err = client.DurablePeriodicJobUpsert(ctx, &river.DurablePeriodicJobUpsertOpts{
		ID: sweepJobID, Kind: sweepJobID, Queue: JobQueue,
		Schedule: &river.DurablePeriodicJobSchedule{CronExpression: sweepCron, CronTimezone: sweepTimezone},
	})
	return err
}

func (j *Jobs) EnqueueSettle(ctx context.Context, organizationUUID, workspaceUUID, taskID string) error {
	if j.client == nil {
		return errJobsUnconfigured
	}
	_, err := j.client.Insert(ctx, settleArgs{OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, TaskID: taskID}, nil)
	return err
}

type settleWorker struct {
	river.WorkerDefaults[settleArgs]
	handler *Handler
}

func (w *settleWorker) Work(ctx context.Context, job *river.Job[settleArgs]) error {
	args := job.Args
	done, err := w.handler.settleTask(ctx, args.OrganizationUUID, args.WorkspaceUUID, args.TaskID, job.Attempt >= job.MaxAttempts)
	if err != nil {
		return err
	}
	if !done {
		return river.JobSnooze(0)
	}
	return nil
}

func (*settleWorker) Timeout(*river.Job[settleArgs]) time.Duration { return settleJobTimeout }

type sweepWorker struct {
	river.WorkerDefaults[sweepArgs]
	handler *Handler
}

func (w *sweepWorker) Work(ctx context.Context, _ *river.Job[sweepArgs]) error {
	return w.handler.sweep(ctx, time.Now().UTC())
}

func (h *Handler) sweep(ctx context.Context, now time.Time) error {
	items, err := h.store.ListStaleRunningEvaluationItems(ctx, now.Add(-staleItemAge), sweepLimit)
	if err != nil {
		return err
	}
	for _, item := range items {
		if h.reconcileItem(ctx, item, now) {
			h.requestSettle(ctx, item.OrganizationUUID, item.WorkspaceUUID, item.TaskExternalID)
		}
	}
	refs, err := h.store.ListSettleableEvaluationTasks(ctx, now.Add(-settleTaskAge), sweepLimit)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		h.requestSettle(ctx, ref.OrganizationUUID, ref.WorkspaceUUID, ref.ExternalID)
	}
	return nil
}

func (h *Handler) reconcileItem(ctx context.Context, item db.EvaluationTaskItem, now time.Time) bool {
	status, message, terminal, err := h.sessionOutcome(ctx, item)
	if err != nil {
		h.logger.WarnContext(ctx, "reconcile evaluation item", "item_id", item.ExternalID, "session_id", item.SessionExternalID, "error", err)
		return false
	}
	if terminal {
		return h.finishItem(ctx, item, status, message)
	}
	if err := h.store.TouchRunningEvaluationItem(ctx, item.OrganizationUUID, item.WorkspaceUUID, item.UUID, now); err != nil {
		h.logger.WarnContext(ctx, "touch evaluation item", "item_id", item.ExternalID, "error", err)
	}
	return false
}

func (h *Handler) sessionOutcome(ctx context.Context, item db.EvaluationTaskItem) (string, string, bool, error) {
	_, ok, err := h.store.GetSession(ctx, item.WorkspaceUUID, item.SessionExternalID)
	if err != nil {
		return "", "", false, err
	}
	if !ok {
		return "failed", sessionTerminatedMessage, true, nil
	}
	events, err := h.sessionEvents(ctx, item.WorkspaceUUID, item.SessionExternalID)
	if errors.Is(err, errSessionTooLarge) {
		return "failed", scoreSessionTooLargeMessage, true, nil
	}
	if err != nil {
		return "", "", false, err
	}
	status, message, terminal := terminalStatus(events)
	return status, message, terminal, nil
}

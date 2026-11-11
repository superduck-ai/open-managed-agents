package evaluations

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/riverjobs"
)

const (
	JobQueue          = "evaluations"
	settleJobKind     = "evaluation_task_settle"
	sweepJobID        = "evaluation_task_sweep"
	sweepCron         = "* * * * *"
	sweepTimezone     = "UTC"
	settleJobTimeout  = 10 * time.Minute
	settleMaxAttempts = 3
	settleListLimit   = 20
)

var (
	errJobsUnconfigured = errors.New("evaluation jobs are not configured")
	settleUniqueStates  = []rivertype.JobState{
		rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateScheduled,
	}
)

type settleArgs struct {
	OrganizationUUID string `json:"organization_uuid"`
	WorkspaceUUID    string `json:"workspace_uuid"`
	TaskID           string `json:"task_id"`
	Supersedes       int64  `json:"supersedes,omitempty"`
}

func (settleArgs) Kind() string { return settleJobKind }

func (settleArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: JobQueue, MaxAttempts: settleMaxAttempts,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: settleUniqueStates},
	}
}

type sweepArgs struct{}

func (sweepArgs) Kind() string { return sweepJobID }

type Jobs struct {
	runner *TaskRunner
	client *river.Client[*sql.Tx]
}

func NewJobs(runner *TaskRunner) *Jobs {
	jobs := &Jobs{runner: runner}
	runner.WithSettleQueue(jobs)
	return jobs
}

func (j *Jobs) Register(workers *river.Workers) {
	river.AddWorker(workers, &settleWorker{runner: j.runner})
	river.AddWorker(workers, &sweepWorker{runner: j.runner})
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
	listed, err := j.client.JobList(ctx, river.NewJobListParams().Kinds(settleJobKind).States(settleUniqueStates...).
		Where("args->>'workspace_uuid' = @workspace_uuid AND args->>'task_id' = @task_id", river.NamedArgs{"workspace_uuid": workspaceUUID, "task_id": taskID}).
		First(settleListLimit))
	if err != nil {
		return err
	}
	insert, stale := settleNeedsInsert(listed.Jobs, time.Now().UTC())
	if !insert {
		return nil
	}
	args := settleArgs{OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, TaskID: taskID}
	if stale {
		args.Supersedes = latestJobID(listed.Jobs)
	}
	_, err = j.client.Insert(ctx, args, nil)
	return err
}

func settleNeedsInsert(jobs []*rivertype.JobRow, now time.Time) (bool, bool) {
	stale := false
	for _, job := range jobs {
		switch job.State {
		case rivertype.JobStateRunning:
			if job.AttemptedAt == nil || now.Sub(*job.AttemptedAt) < settleJobTimeout {
				return false, false
			}
			stale = true
		case rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable, rivertype.JobStateScheduled:
			return false, false
		}
	}
	return true, stale
}

func latestJobID(jobs []*rivertype.JobRow) int64 {
	var latest int64
	for _, job := range jobs {
		latest = max(latest, job.ID)
	}
	return latest
}

type settleWorker struct {
	river.WorkerDefaults[settleArgs]
	runner *TaskRunner
}

func (w *settleWorker) Work(ctx context.Context, job *river.Job[settleArgs]) error {
	args := job.Args
	done, err := w.runner.settleTask(ctx, args.OrganizationUUID, args.WorkspaceUUID, args.TaskID, job.Attempt >= job.MaxAttempts)
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
	runner *TaskRunner
}

func (w *sweepWorker) Work(ctx context.Context, _ *river.Job[sweepArgs]) error {
	return w.runner.sweep(ctx, time.Now().UTC())
}

package db

import (
	"context"
	"errors"

	"github.com/superduck-ai/yourbatis"
)

type SessionTitleJob struct {
	UUID          string
	WorkspaceUUID string
	SessionUUID   string
}

func enqueueSessionTitleTx(ctx context.Context, executor yourbatis.Executor, session Session) error {
	jobs := NewSessionTitleJobMapper(executor)
	if err := jobs.BeginEnqueue(ctx); err != nil {
		return err
	}
	if err := jobs.Enqueue(ctx, session.OrganizationUUID, session.WorkspaceUUID, session.UUID); err != nil {
		if rollbackErr := jobs.RollbackEnqueue(ctx); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
	}
	return jobs.ReleaseEnqueue(ctx)
}

func (d *DB) ListSessionTitleInputs(ctx context.Context, workspaceUUID, sessionUUID, afterUUID string) ([]SessionEvent, error) {
	rows, err := NewSessionEventMapper(d.mapperDB).ListTitleInputs(ctx, workspaceUUID, sessionUUID, afterUUID)
	return sessionEventsFromRows(rows), err
}

func (d *DB) LeaseSessionTitleJobs(ctx context.Context, workerID string, limit int) ([]SessionTitleJob, error) {
	rows, err := NewSessionTitleJobMapper(d.mapperDB).Lease(ctx, workerID, limit)
	if err != nil {
		return nil, err
	}
	jobs := make([]SessionTitleJob, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, SessionTitleJob{UUID: row.UUID, WorkspaceUUID: row.WorkspaceUUID, SessionUUID: row.SessionUUID})
	}
	return jobs, nil
}

func (d *DB) CompleteSessionTitleJob(ctx context.Context, job SessionTitleJob, workerID, title string) error {
	return NewSessionTitleJobMapper(d.mapperDB).Complete(ctx, sessionTitleCompletionParams{
		JobUUID: job.UUID, WorkspaceUUID: job.WorkspaceUUID, WorkerID: workerID, Title: title,
	})
}

package db

import "context"

type SessionTitleJob struct {
	UUID          string
	WorkspaceUUID string
	SessionUUID   string
	SessionID     string
	Input         string
}

func (d *DB) LeaseSessionTitleJobs(ctx context.Context, workerID string, limit int) ([]SessionTitleJob, error) {
	rows, err := NewSessionTitleJobMapper(d.mapperDB).Lease(ctx, workerID, limit)
	if err != nil {
		return nil, err
	}
	jobs := make([]SessionTitleJob, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, SessionTitleJob{UUID: row.UUID, WorkspaceUUID: row.WorkspaceUUID,
			SessionUUID: row.SessionUUID, SessionID: row.SessionID, Input: row.Input})
	}
	return jobs, nil
}

func (d *DB) CompleteSessionTitleJob(ctx context.Context, job SessionTitleJob, workerID, title string) error {
	return NewSessionTitleJobMapper(d.mapperDB).Complete(ctx, sessionTitleCompletionParams{
		JobUUID: job.UUID, WorkspaceUUID: job.WorkspaceUUID, WorkerID: workerID, Title: title,
	})
}

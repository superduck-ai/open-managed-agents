package identities

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const cleanupJobKind = "identity_cleanup"

type cleanupArgs struct {
	WorkspaceUUID string `json:"workspace_uuid"`
	IdentityUUID  string `json:"identity_uuid"`
	CleanupID     string `json:"cleanup_id"`
}

func (cleanupArgs) Kind() string { return cleanupJobKind }

func (cleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: CleanupQueue, MaxAttempts: 3}
}

type cleanupWorker struct {
	river.WorkerDefaults[cleanupArgs]
	service *Service
}

func RegisterWorkers(workers *river.Workers, store Store, logger *slog.Logger) {
	river.AddWorker(workers, &cleanupWorker{service: NewService(store, logger)})
}

func (w *cleanupWorker) Work(ctx context.Context, job *river.Job[cleanupArgs]) error {
	var prior CleanupOutcome
	row, err := w.service.store.FindIdentityCleanupRun(ctx, job.Args.WorkspaceUUID, job.Args.CleanupID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	if err == nil && row.Result != "" {
		if err := json.Unmarshal([]byte(row.Result), &prior); err != nil {
			return err
		}
	}
	outcome := MergeCleanupOutcome(prior, w.service.ApplyCleanup(ctx, job.Args.WorkspaceUUID, job.Args.IdentityUUID, nil))
	state := "completed"
	if !outcome.Completed {
		state = "partial"
	}
	if err := w.service.store.SaveIdentityCleanupRun(ctx, job.Args.WorkspaceUUID, job.Args.CleanupID, state, mustJSON(outcome)); err != nil {
		return err
	}
	if !outcome.Completed && job.Attempt < job.MaxAttempts {
		return unavailable(nil)
	}
	return nil
}

type RiverCleanup struct {
	client *river.Client[*sql.Tx]
}

func NewRiverCleanup(client *river.Client[*sql.Tx]) RiverCleanup {
	return RiverCleanup{client: client}
}

func (q RiverCleanup) EnqueueIdentityCleanup(ctx context.Context, workspaceUUID, identityUUID, cleanupID string) error {
	if q.client == nil {
		return unavailable(nil)
	}
	_, err := q.client.Insert(ctx, cleanupArgs{WorkspaceUUID: workspaceUUID, IdentityUUID: identityUUID, CleanupID: cleanupID}, nil)
	return err
}

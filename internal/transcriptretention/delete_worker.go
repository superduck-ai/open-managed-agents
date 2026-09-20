package transcriptretention

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type deleteArgs archiveArgs

func (deleteArgs) Kind() string                 { return "transcript_archive_delete" }
func (deleteArgs) InsertOpts() river.InsertOpts { return archiveArgs{}.InsertOpts() }

type deleteWorker struct {
	river.WorkerDefaults[deleteArgs]
	service *Service
}

func (w *deleteWorker) Work(ctx context.Context, job *river.Job[deleteArgs]) error {
	args := job.Args
	err := w.service.HardDelete(ctx, db.TranscriptScope{OrganizationUUID: args.OrganizationUUID, WorkspaceUUID: args.WorkspaceUUID, CodeSessionUUID: args.CodeSessionUUID, CodeSessionExternalID: args.CodeSessionExternalID})
	return w.handleResult(ctx, job, err)
}

func (w *deleteWorker) handleResult(ctx context.Context, job *river.Job[deleteArgs], err error) error {
	if deletionNeedsRepair(err) {
		var metadata struct {
			Snoozes int `json:"snoozes"`
		}
		if json.Unmarshal(job.Metadata, &metadata) != nil || metadata.Snoozes == 0 {
			w.service.logger.ErrorContext(ctx, "transcript physical deletion requires repair", "code_session_id", job.Args.CodeSessionExternalID, "workspace_id", job.Args.WorkspaceUUID, "error", err)
		}
		return river.JobSnooze(24 * time.Hour)
	}
	return err
}

func (s *Service) enqueueDeletes(ctx context.Context, client *river.Client[*sql.Tx]) error {
	if !s.policy.HardDeleteEnabled || s.policy.DryRun {
		return nil
	}
	after := ""
	cutoff := time.Now().UTC().Add(-s.policy.SoftDeleteWindow)
	for {
		scopes, err := s.database.ListTranscriptDeletionCandidates(ctx, after, cutoff, 100)
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			_, err := client.Insert(ctx, deleteArgs{OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, CodeSessionUUID: scope.CodeSessionUUID, CodeSessionExternalID: scope.CodeSessionExternalID}, nil)
			if err != nil {
				return err
			}
		}
		if len(scopes) < 100 {
			return nil
		}
		after = scopes[len(scopes)-1].CodeSessionUUID
	}
}

package transcriptretention

import (
	"context"
	"database/sql"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const Queue = "transcript_archive"
const sweepID = "transcript_archive_sweep"

type sweepArgs struct{}

func (sweepArgs) Kind() string { return sweepID }

type archiveArgs struct {
	OrganizationUUID      string `json:"organization_uuid"`
	WorkspaceUUID         string `json:"workspace_uuid"`
	CodeSessionUUID       string `json:"code_session_uuid"`
	CodeSessionExternalID string `json:"code_session_id"`
	Terminal              bool   `json:"terminal"`
}

func (archiveArgs) Kind() string { return "transcript_archive_session" }
func (archiveArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Queue, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateScheduled}}}
}

func (s *Service) Register(workers *river.Workers) {
	river.AddWorker(workers, &sweepWorker{service: s})
	river.AddWorker(workers, &archiveWorker{service: s})
	river.AddWorker(workers, &deleteWorker{service: s})
}

func (s *Service) Configure(ctx context.Context, client *river.Client[*sql.Tx]) error {
	_, err := client.DurablePeriodicJobUpsert(ctx, &river.DurablePeriodicJobUpsertOpts{ID: sweepID, Kind: sweepID, Queue: Queue, Schedule: &river.DurablePeriodicJobSchedule{CronExpression: "*/5 * * * *", CronTimezone: "UTC"}})
	return err
}

type sweepWorker struct {
	river.WorkerDefaults[sweepArgs]
	service *Service
}

func (w *sweepWorker) Work(ctx context.Context, _ *river.Job[sweepArgs]) error {
	s := w.service
	if !s.policy.Enabled {
		return nil
	}
	client := river.ClientFromContext[*sql.Tx](ctx)
	for _, terminal := range []bool{true} {
		if terminal && !s.policy.TerminalSweepEnabled || !terminal && !s.policy.BoundarySweepEnabled {
			continue
		}
		if err := s.enqueue(ctx, client, terminal); err != nil {
			return err
		}
	}
	return s.enqueueDeletes(ctx, client)
}

func (s *Service) enqueue(ctx context.Context, client *river.Client[*sql.Tx], terminal bool) error {
	query := s.query(db.TranscriptScope{}, terminal)
	query.Limit = 100
	for {
		scopes, err := s.database.ListArchivableTranscriptSessions(ctx, query)
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			_, err := client.Insert(ctx, archiveArgs{OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, CodeSessionUUID: scope.CodeSessionUUID, CodeSessionExternalID: scope.CodeSessionExternalID, Terminal: terminal}, nil)
			if err != nil {
				return err
			}
		}
		if len(scopes) < query.Limit {
			return nil
		}
		query.AfterUUID = scopes[len(scopes)-1].CodeSessionUUID
	}
}

type archiveWorker struct {
	river.WorkerDefaults[archiveArgs]
	service *Service
}

func (w *archiveWorker) Work(ctx context.Context, job *river.Job[archiveArgs]) error {
	args := job.Args
	return w.service.Archive(ctx, db.TranscriptScope{OrganizationUUID: args.OrganizationUUID, WorkspaceUUID: args.WorkspaceUUID, CodeSessionUUID: args.CodeSessionUUID, CodeSessionExternalID: args.CodeSessionExternalID}, args.Terminal)
}

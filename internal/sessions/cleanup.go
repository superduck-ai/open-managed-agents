package sessions

import (
	"context"
	"database/sql"
	"slices"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
	"github.com/superduck-ai/yourbatis"
)

const SessionCleanupQueue = "session_archive_cleanup"

type sessionCleanupArgs struct {
	OrganizationUUID     string   `json:"organization_uuid"`
	WorkspaceUUID        string   `json:"workspace_uuid"`
	SessionUUID          string   `json:"session_uuid"`
	CodeSessionIDs       []string `json:"code_session_ids"`
	ClosedSubscriptionID string   `json:"closed_subscription_id,omitempty"`
}

func (sessionCleanupArgs) Kind() string { return "session_archive_cleanup" }

func (sessionCleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: SessionCleanupQueue,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
			rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
			rivertype.JobStateRetryable, rivertype.JobStateScheduled,
		}},
	}
}

type SessionCleanup struct {
	db     *db.DB
	broker workerevents.Broker
	client *river.Client[*sql.Tx]
}

func NewSessionCleanup(database *db.DB, broker workerevents.Broker) *SessionCleanup {
	return &SessionCleanup{db: database, broker: broker}
}

func (c *SessionCleanup) Configure(client *river.Client[*sql.Tx]) {
	c.client = client
	c.db.ConfigureSessionCleanup(c.enqueue)
}

func (c *SessionCleanup) Register(workers *river.Workers) {
	river.AddWorker(workers, &sessionCleanupWorker{cleanup: c})
}

func (c *SessionCleanup) ReclaimClosedSubscription(ctx context.Context, codeSession db.CodeSession, subscriptionID string) error {
	if codeSession.SessionUUID == "" {
		return nil
	}
	retired, err := c.db.IsSessionRetired(ctx, codeSession.OrganizationUUID, codeSession.WorkspaceUUID, codeSession.SessionUUID)
	if err == nil && !retired {
		return nil
	}
	if c.client == nil {
		return errSessionCleanupNotConfigured
	}
	_, err = c.client.Insert(ctx, sessionCleanupArgs{
		OrganizationUUID:     codeSession.OrganizationUUID,
		WorkspaceUUID:        codeSession.WorkspaceUUID,
		SessionUUID:          codeSession.SessionUUID,
		CodeSessionIDs:       []string{codeSession.ExternalID},
		ClosedSubscriptionID: subscriptionID,
	}, nil)
	return err
}

func (c *SessionCleanup) enqueue(ctx context.Context, tx *yourbatis.Tx, removal db.SessionRemoval) error {
	if c.client == nil {
		return errSessionCleanupNotConfigured
	}
	ids := slices.Clone(removal.CodeSessionIDs)
	slices.Sort(ids)
	_, err := c.client.InsertTx(ctx, tx.SQLTx(), sessionCleanupArgs{
		OrganizationUUID: removal.Session.OrganizationUUID,
		WorkspaceUUID:    removal.Session.WorkspaceUUID,
		SessionUUID:      removal.Session.UUID,
		CodeSessionIDs:   ids,
	}, nil)
	return err
}

type sessionCleanupWorker struct {
	river.WorkerDefaults[sessionCleanupArgs]
	cleanup *SessionCleanup
}

func (w *sessionCleanupWorker) Work(ctx context.Context, job *river.Job[sessionCleanupArgs]) error {
	if job.Args.ClosedSubscriptionID != "" {
		retired, err := w.cleanup.db.IsSessionRetired(ctx, job.Args.OrganizationUUID, job.Args.WorkspaceUUID, job.Args.SessionUUID)
		if err != nil {
			return err
		}
		if !retired {
			return nil
		}
	}
	for _, codeSessionID := range job.Args.CodeSessionIDs {
		if err := w.cleanup.broker.PurgeSession(ctx, codeSessionID); err != nil {
			return err
		}
	}
	return nil
}

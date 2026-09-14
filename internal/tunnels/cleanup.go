package tunnels

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const CleanupQueue = "tunnel_cleanup"

// CleanupJobs uses the application's shared River client and database pool.
type CleanupJobs struct{ client *river.Client[*sql.Tx] }

func NewCleanupJobs(client *river.Client[*sql.Tx]) *CleanupJobs { return &CleanupJobs{client: client} }

type controlCleanupArgs struct {
	OrganizationUUID string `json:"organization_uuid"`
	WorkspaceUUID    string `json:"workspace_uuid"`
	TunnelID         string `json:"tunnel_id"`
	TunnelUUID       string `json:"tunnel_uuid"`
}

func (controlCleanupArgs) Kind() string { return "tunnel_control_cleanup" }
func (controlCleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: CleanupQueue, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
		rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateScheduled,
	}}}
}

func (j *CleanupJobs) enqueue(ctx context.Context, tunnel db.MCPTunnel, tx *sql.Tx) error {
	if j == nil || j.client == nil {
		return ErrCleanupUnavailable
	}
	args := controlCleanupArgs{OrganizationUUID: tunnel.OrganizationUUID, WorkspaceUUID: tunnel.WorkspaceUUID, TunnelID: tunnel.ExternalID, TunnelUUID: tunnel.UUID}
	if tx != nil {
		_, err := j.client.InsertTx(ctx, tx, args, nil)
		return err
	}
	_, err := j.client.Insert(ctx, args, nil)
	return err
}

// RegisterCleanupWorker adds a worker to the existing shared River runtime.
func RegisterCleanupWorker(workers *river.Workers, database *db.DB, broker *Broker, logger *slog.Logger) {
	river.AddWorker(workers, &controlCleanupWorker{database: database, broker: broker, logger: logging.LoggerOrDefault(logger)})
}

type controlCleanupDatabase interface {
	GetMCPTunnel(context.Context, string, string, string) (db.MCPTunnel, error)
}

type controlCleanupWorker struct {
	river.WorkerDefaults[controlCleanupArgs]
	database controlCleanupDatabase
	broker   *Broker
	logger   *slog.Logger
}

func (w *controlCleanupWorker) Work(ctx context.Context, job *river.Job[controlCleanupArgs]) error {
	if err := w.cleanup(ctx, job.Args); err != nil {
		w.logger.WarnContext(ctx, "tunnel control cleanup deferred", "tunnel_id", job.Args.TunnelID, "error", err)
		// Infrastructure outages must not exhaust the retry budget and strand capacity.
		return river.JobSnooze(time.Minute)
	}
	return nil
}

func (w *controlCleanupWorker) cleanup(ctx context.Context, args controlCleanupArgs) error {
	tunnel, err := w.database.GetMCPTunnel(ctx, args.OrganizationUUID, args.WorkspaceUUID, args.TunnelID)
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if tunnel.ArchivedAt == nil || tunnel.UUID != args.TunnelUUID {
		return nil
	}
	if w.broker == nil {
		return ErrCleanupUnavailable
	}
	return w.broker.purgeControl(ctx, tunnel.UUID)
}

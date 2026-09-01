package environments

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

const SandboxLifecycleQueue = "sandbox_lifecycle"
const lifecycleBatchSize = 100

type SandboxLifecycle struct {
	database *db.DB
	provider *e2bruntime.E2BProvider
	cfg      config.SandboxLifecycleConfig
	logger   *slog.Logger
}

func NewSandboxLifecycle(database *db.DB, provider *e2bruntime.E2BProvider, cfg config.SandboxLifecycleConfig, logger *slog.Logger) *SandboxLifecycle {
	return &SandboxLifecycle{database: database, provider: provider, cfg: cfg, logger: logging.LoggerOrDefault(logger)}
}

type sandboxSweepArgs struct{}

func (sandboxSweepArgs) Kind() string { return "sandbox_lifecycle_sweep" }

type sandboxReclaimArgs struct {
	OrganizationUUID string `json:"organization_uuid"`
	WorkspaceUUID    string `json:"workspace_uuid"`
	SandboxUUID      string `json:"sandbox_uuid"`
}

func (sandboxReclaimArgs) Kind() string { return "sandbox_reclaim" }
func (sandboxReclaimArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: SandboxLifecycleQueue, UniqueOpts: lifecycleUniqueOpts()}
}

func (l *SandboxLifecycle) Register(workers *river.Workers) {
	river.AddWorker(workers, &sandboxSweepWorker{lifecycle: l})
	river.AddWorker(workers, &sandboxReclaimWorker{lifecycle: l})
}

// Configure upserts a single cluster-wide cron; policy is read by workers at execution time.
func (l *SandboxLifecycle) Configure(ctx context.Context, client *river.Client[*sql.Tx]) error {
	existing, err := client.DurablePeriodicJobGet(ctx, "sandbox_lifecycle_sweep")
	if err != nil && !errors.Is(err, river.ErrNotFound) {
		return err
	}
	if err == nil && existing.CronExpression != nil && *existing.CronExpression == "* * * * *" && existing.CronTimezone == "UTC" && existing.Kind == (sandboxSweepArgs{}).Kind() && existing.Queue == SandboxLifecycleQueue && existing.PausedAt == nil {
		return nil
	}
	_, err = client.DurablePeriodicJobUpsert(ctx, &river.DurablePeriodicJobUpsertOpts{
		ID: "sandbox_lifecycle_sweep", Kind: sandboxSweepArgs{}.Kind(), Queue: SandboxLifecycleQueue,
		Schedule: &river.DurablePeriodicJobSchedule{CronExpression: "* * * * *", CronTimezone: "UTC"},
	})
	return err
}

// Reclaim only contacts the provider after a durable, conditional claim.
func (l *SandboxLifecycle) Reclaim(ctx context.Context, target db.SandboxReclaimTarget) error {
	cutoff := time.Now().UTC().Add(-l.cfg.IdleTimeout)
	// Disabling new reclamation must not abandon already committed deletions.
	if !l.cfg.Enabled || l.cfg.DryRun {
		cutoff = time.Time{}
	}
	claimed, ok, err := l.database.BeginSandboxReclamation(ctx, target, cutoff)
	if err != nil || !ok {
		return err
	}
	if err := l.provider.Kill(ctx, claimed.ProviderSandboxID); err != nil {
		return err
	}
	if err := l.database.CompleteSandboxReclamation(ctx, claimed); err != nil {
		return err
	}
	l.logger.InfoContext(ctx, "idle sandbox reclaimed", "organization_id", claimed.OrganizationUUID,
		"workspace_id", claimed.WorkspaceUUID, "sandbox_id", claimed.SandboxUUID)
	return nil
}

type sandboxSweepWorker struct {
	river.WorkerDefaults[sandboxSweepArgs]
	lifecycle *SandboxLifecycle
}

func (w *sandboxSweepWorker) Work(ctx context.Context, _ *river.Job[sandboxSweepArgs]) error {
	client := river.ClientFromContext[*sql.Tx](ctx)
	return w.lifecycle.enqueueReclaims(ctx, client)
}

func (l *SandboxLifecycle) enqueueReclaims(ctx context.Context, client *river.Client[*sql.Tx]) error {
	after := ""
	for {
		targets, err := l.database.ListSandboxReclaimCandidates(ctx, time.Now().UTC().Add(-l.cfg.IdleTimeout), after, lifecycleBatchSize, l.cfg.Enabled)
		if err != nil {
			return err
		}
		for _, target := range targets {
			if l.cfg.DryRun && !target.Reclaiming {
				l.logger.InfoContext(ctx, "idle sandbox reclaim candidate", "workspace_id", target.WorkspaceUUID, "sandbox_id", target.SandboxUUID)
				continue
			}
			_, err := client.Insert(ctx, sandboxReclaimArgs{OrganizationUUID: target.OrganizationUUID, WorkspaceUUID: target.WorkspaceUUID, SandboxUUID: target.SandboxUUID}, nil)
			if err != nil {
				return err
			}
		}
		if len(targets) < lifecycleBatchSize {
			return nil
		}
		after = targets[len(targets)-1].SandboxUUID
	}
}

type sandboxReclaimWorker struct {
	river.WorkerDefaults[sandboxReclaimArgs]
	lifecycle *SandboxLifecycle
}

func (w *sandboxReclaimWorker) Work(ctx context.Context, job *river.Job[sandboxReclaimArgs]) error {
	return w.lifecycle.Reclaim(ctx, db.SandboxReclaimTarget{OrganizationUUID: job.Args.OrganizationUUID, WorkspaceUUID: job.Args.WorkspaceUUID, SandboxUUID: job.Args.SandboxUUID})
}

// Completed no-op jobs must not suppress a later idle period.
func lifecycleUniqueOpts() river.UniqueOpts {
	return river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
		rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
		rivertype.JobStateRetryable, rivertype.JobStateScheduled,
	}}
}

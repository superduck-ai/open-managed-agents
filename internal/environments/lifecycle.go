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

const (
	SandboxLifecycleQueue = "sandbox_lifecycle"
	lifecycleBatchSize    = 100
	sandboxSweepID        = "sandbox_lifecycle_sweep"
	sandboxSweepCron      = "* * * * *"
	sandboxSweepTimezone  = "UTC"
)

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

func (sandboxSweepArgs) Kind() string { return sandboxSweepID }

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
	existing, err := client.DurablePeriodicJobGet(ctx, sandboxSweepID)
	if err != nil && !errors.Is(err, river.ErrNotFound) {
		return err
	}
	if err == nil && matchesSandboxSweepSchedule(existing) {
		return nil
	}
	_, err = client.DurablePeriodicJobUpsert(ctx, &river.DurablePeriodicJobUpsertOpts{
		ID: sandboxSweepID, Kind: sandboxSweepArgs{}.Kind(), Queue: SandboxLifecycleQueue,
		Schedule: &river.DurablePeriodicJobSchedule{CronExpression: sandboxSweepCron, CronTimezone: sandboxSweepTimezone},
	})
	return err
}

func matchesSandboxSweepSchedule(job *rivertype.DurablePeriodicJob) bool {
	return job != nil && job.CronExpression != nil && *job.CronExpression == sandboxSweepCron &&
		job.CronTimezone == sandboxSweepTimezone && job.Kind == (sandboxSweepArgs{}).Kind() &&
		job.Queue == SandboxLifecycleQueue && job.PausedAt == nil
}

func (l *SandboxLifecycle) allowsNewClaims() bool {
	return l.cfg.Enabled && !l.cfg.DryRun
}

// Reclaim only contacts the provider after a durable, conditional claim.
func (l *SandboxLifecycle) Reclaim(ctx context.Context, target db.SandboxReclaimTarget) error {
	cutoff := time.Now().UTC().Add(-l.cfg.IdleTimeout)
	claimed, ok, err := l.database.BeginSandboxReclamation(ctx, target, cutoff, l.allowsNewClaims())
	if err != nil {
		return err
	}
	if !ok {
		l.logger.DebugContext(ctx, "idle sandbox reclamation skipped", "reason", "target_missing_or_ineligible",
			"organization_id", target.OrganizationUUID,
			"workspace_id", target.WorkspaceUUID, "sandbox_id", target.SandboxUUID)
		return nil
	}
	if err := l.provider.Kill(ctx, claimed.ProviderSandboxID); err != nil {
		return err
	}
	completed, err := l.database.CompleteSandboxReclamation(ctx, claimed)
	if err != nil {
		return err
	}
	if !completed {
		l.logger.DebugContext(ctx, "idle sandbox reclamation completion skipped", "reason", "target_missing_or_already_completed",
			"organization_id", claimed.OrganizationUUID,
			"workspace_id", claimed.WorkspaceUUID, "sandbox_id", claimed.SandboxUUID)
		return nil
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
			if !l.allowsNewClaims() && !target.Reclaiming {
				if l.cfg.Enabled && l.cfg.DryRun {
					l.logger.InfoContext(ctx, "idle sandbox reclaim candidate", "organization_id", target.OrganizationUUID,
						"workspace_id", target.WorkspaceUUID, "sandbox_id", target.SandboxUUID)
				}
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

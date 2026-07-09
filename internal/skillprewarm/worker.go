package skillprewarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

const (
	defaultPollInterval = 5 * time.Second
	defaultLeaseTimeout = time.Minute
	defaultJobLimit     = 5
	defaultFanoutLimit  = 100
	maxAttempts         = 5
)

type JobStore interface {
	LeaseSkillPrewarmJobs(ctx context.Context, workerID string, limit int, leaseDuration time.Duration) ([]db.SkillPrewarmJob, error)
	CompleteSkillPrewarmJob(ctx context.Context, jobID int64) error
	FailSkillPrewarmJob(ctx context.Context, jobID int64, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error
}

type SnapshotJobStore interface {
	EnqueueSkillPrewarmSnapshotJob(ctx context.Context, input db.SkillPrewarmSnapshotJobInput) error
}

type FanoutStore interface {
	EnqueueSkillPrewarmFanoutJob(ctx context.Context, input db.SkillPrewarmFanoutJobInput) error
	ListAgentsForSkillPrewarmFanout(ctx context.Context, workspaceID int64, skillID string, afterID int64, limit int) ([]db.Agent, bool, error)
	ListDeploymentsForSkillPrewarmFanout(ctx context.Context, workspaceID int64, skillID string, afterID int64, limit int) ([]db.Deployment, bool, error)
}

type RuntimeResolver interface {
	ResolveAgentSnapshot(ctx context.Context, workspaceID int64, snapshot json.RawMessage) ([]skillsapi.RuntimeSkill, error)
}

type SkillMountPreparer interface {
	PrepareSkillMount(ctx context.Context, runtimeSkills []skillsapi.RuntimeSkill) (*e2bruntime.SkillMount, error)
}

type Worker struct {
	jobs      JobStore
	snapshots SnapshotJobStore
	fanout    FanoutStore
	resolver  RuntimeResolver
	preparer  SkillMountPreparer
}

type jobPayload struct {
	Kind                string          `json:"kind"`
	AgentSnapshot       json.RawMessage `json:"agent_snapshot"`
	Source              string          `json:"source"`
	SourceID            string          `json:"source_id"`
	Trigger             string          `json:"trigger"`
	TriggerSkillID      string          `json:"trigger_skill_id"`
	TriggerSkillVersion string          `json:"trigger_skill_version"`
	SkillID             string          `json:"skill_id"`
	Version             string          `json:"version"`
	AfterAgentID        int64           `json:"after_agent_id"`
	AfterDeploymentID   int64           `json:"after_deployment_id"`
}

func NewWorker(jobs JobStore, snapshots SnapshotJobStore, fanout FanoutStore, resolver RuntimeResolver, preparer SkillMountPreparer) *Worker {
	return &Worker{jobs: jobs, snapshots: snapshots, fanout: fanout, resolver: resolver, preparer: preparer}
}

func StartWorker(ctx context.Context, database *db.DB, objectStore storage.ObjectStore, cfg config.Config) {
	if database == nil || objectStore == nil || !cfg.EnvironmentRunnerEnabled {
		return
	}
	workerID := fmt.Sprintf("skill-prewarm-%d", os.Getpid())
	resolver := skillsapi.NewRuntimeResolver(cfg, database, objectStore)
	worker := NewWorker(database, database, database, resolver, e2bruntime.NewProvider(cfg))
	go worker.loop(ctx, workerID)
}

func (w *Worker) loop(ctx context.Context, workerID string) {
	ticker := time.NewTicker(defaultPollInterval)
	defer ticker.Stop()
	for {
		if err := w.RunOnce(ctx, workerID); err != nil {
			log.Printf("skill prewarm worker=%s: %v", workerID, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context, workerID string) error {
	if w == nil || w.jobs == nil {
		return nil
	}
	jobs, err := w.jobs.LeaseSkillPrewarmJobs(ctx, workerID, defaultJobLimit, defaultLeaseTimeout)
	if err != nil {
		return err
	}
	var errs []error
	for _, job := range jobs {
		if err := w.processJob(ctx, job); err != nil {
			delay := retryDelay(job.Attempts + 1)
			if markErr := w.jobs.FailSkillPrewarmJob(ctx, job.ID, job.Attempts, err.Error(), delay, maxAttempts); markErr != nil {
				errs = append(errs, fmt.Errorf("mark skill prewarm job %s retry: %w", job.ExternalID, markErr))
			}
			errs = append(errs, fmt.Errorf("process skill prewarm job %s: %w", job.ExternalID, err))
			continue
		}
		if err := w.jobs.CompleteSkillPrewarmJob(ctx, job.ID); err != nil {
			errs = append(errs, fmt.Errorf("complete skill prewarm job %s: %w", job.ExternalID, err))
		}
	}
	return errors.Join(errs...)
}

func (w *Worker) processJob(ctx context.Context, job db.SkillPrewarmJob) error {
	var payload jobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return err
	}
	switch payload.Kind {
	case "snapshot":
		return w.processSnapshot(ctx, job.WorkspaceID, payload)
	case "fanout":
		return w.processFanout(ctx, job.WorkspaceID, payload)
	default:
		return fmt.Errorf("unknown skill prewarm job kind %q", payload.Kind)
	}
}

func (w *Worker) processSnapshot(ctx context.Context, workspaceID int64, payload jobPayload) error {
	if !agentsnapshot.SnapshotHasSkills(payload.AgentSnapshot) {
		return nil
	}
	if w.resolver == nil {
		return errors.New("skill prewarm resolver is unavailable")
	}
	if w.preparer == nil {
		return errors.New("skill mount preparer is unavailable")
	}
	runtimeSkills, err := w.resolver.ResolveAgentSnapshot(ctx, workspaceID, payload.AgentSnapshot)
	if err != nil {
		return err
	}
	if len(runtimeSkills) == 0 {
		return nil
	}
	_, err = w.preparer.PrepareSkillMount(ctx, runtimeSkills)
	return err
}

func (w *Worker) processFanout(ctx context.Context, workspaceID int64, payload jobPayload) error {
	if payload.SkillID == "" || payload.Version == "" {
		return nil
	}
	if w.fanout == nil || w.snapshots == nil {
		return errors.New("skill prewarm fanout store is unavailable")
	}
	agents, hasMoreAgents, err := w.fanout.ListAgentsForSkillPrewarmFanout(ctx, workspaceID, payload.SkillID, payload.AfterAgentID, defaultFanoutLimit)
	if err != nil {
		return err
	}
	deployments, hasMoreDeployments, err := w.fanout.ListDeploymentsForSkillPrewarmFanout(ctx, workspaceID, payload.SkillID, payload.AfterDeploymentID, defaultFanoutLimit)
	if err != nil {
		return err
	}

	nextAfterAgentID := payload.AfterAgentID
	for _, agent := range agents {
		nextAfterAgentID = agent.ID
		snapshot, err := agentsnapshot.FromAgent(agent)
		if err != nil {
			return err
		}
		if err := w.snapshots.EnqueueSkillPrewarmSnapshotJob(ctx, db.SkillPrewarmSnapshotJobInput{
			WorkspaceID:         workspaceID,
			AgentSnapshot:       snapshot,
			Source:              "agent",
			SourceID:            agent.ExternalID,
			Trigger:             "skill_version_create",
			TriggerSkillID:      payload.SkillID,
			TriggerSkillVersion: payload.Version,
		}); err != nil {
			return err
		}
	}

	nextAfterDeploymentID := payload.AfterDeploymentID
	for _, deployment := range deployments {
		nextAfterDeploymentID = deployment.ID
		if err := w.snapshots.EnqueueSkillPrewarmSnapshotJob(ctx, db.SkillPrewarmSnapshotJobInput{
			WorkspaceID:         workspaceID,
			AgentSnapshot:       deployment.AgentSnapshot,
			Source:              "deployment",
			SourceID:            deployment.ExternalID,
			Trigger:             "skill_version_create",
			TriggerSkillID:      payload.SkillID,
			TriggerSkillVersion: payload.Version,
		}); err != nil {
			return err
		}
	}

	if hasMoreAgents || hasMoreDeployments {
		return w.fanout.EnqueueSkillPrewarmFanoutJob(ctx, db.SkillPrewarmFanoutJobInput{
			WorkspaceID:       workspaceID,
			SkillID:           payload.SkillID,
			Version:           payload.Version,
			AfterAgentID:      nextAfterAgentID,
			AfterDeploymentID: nextAfterDeploymentID,
		})
	}
	return nil
}

func retryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 5 {
		attempts = 5
	}
	return time.Duration(attempts*attempts) * time.Minute
}

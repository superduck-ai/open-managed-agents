package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"uuid"

	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/deploymentjobs"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

func occurrenceTime(job *river.Job[deploymentjobs.Args]) (time.Time, error) {
	if job.Args.ScheduledAt.IsZero() {
		return time.Time{}, errors.New("scheduled_deployment occurrence is missing")
	}
	return job.Args.ScheduledAt.UTC(), nil
}

// RegisterWorkers binds workers to the store before its shared client is configured.
func RegisterWorkers(workers *river.Workers, store *Store) {
	river.AddWorker(workers, &scheduledDeploymentWorker{store: store})
}

type scheduledDeploymentWorker struct {
	river.WorkerDefaults[deploymentjobs.Args]
	store *Store
}

func (w *scheduledDeploymentWorker) Work(ctx context.Context, job *river.Job[deploymentjobs.Args]) error {
	args := job.Args
	scheduledAt, err := occurrenceTime(job)
	if err != nil {
		return river.JobCancel(err)
	}
	deployment, err := w.store.database.GetDeployment(ctx, args.WorkspaceUUID, args.DeploymentExternalID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	if deployment.ArchivedAt != nil || deployment.Status != "active" {
		return nil
	}
	currentSchedule, err := deploymentjobs.Parse(deployment.Schedule)
	if err != nil || currentSchedule.Config != args.Schedule {
		return nil
	}
	now := time.Now().UTC()

	agent, agentErr := w.store.database.GetAgent(ctx, deployment.WorkspaceUUID, deployment.AgentExternalID)
	if errors.Is(agentErr, db.ErrNotFound) || agent.ArchivedAt != nil {
		return w.applyOccurrence(ctx, db.ApplyScheduledOccurrenceInput{
			Deployment: deployment, ScheduledAt: scheduledAt, ArchiveDeployment: true,
		})
	}
	if agentErr != nil {
		return agentErr
	}

	referenceFailure, err := validateRunDependencies(ctx, w.store.database, deployment.WorkspaceUUID, deployment)
	if err != nil {
		return err
	}
	if referenceFailure != nil {
		return w.recordFailure(ctx, deployment, referenceFailure, scheduledAt, now)
	}
	memoryStores, err := loadDeploymentMemoryStores(ctx, w.store.database, deployment.WorkspaceUUID, deployment.Resources)
	if err != nil {
		if failure := memoryStoreLoadFailure(err); failure != nil {
			return w.recordFailure(ctx, deployment, failure, scheduledAt, now)
		}
		return err
	}
	preparedRun, err := prepareDeploymentExecution(deployment, deployment.CreatedByAPIKeyUUID, deployment.RuntimeUserUUID, now, memoryStores)
	if err != nil {
		if errors.Is(err, errRetryableRunPreparation) {
			return err
		}
		return w.recordFailure(ctx, deployment, runError("session_resource_not_found_error", err.Error()), scheduledAt, now)
	}
	err = w.applyOccurrence(ctx, db.ApplyScheduledOccurrenceInput{
		Deployment: deployment, ScheduledAt: scheduledAt,
		Session: &preparedRun.Session, Events: preparedRun.Events,
		Run: db.DeploymentRun{
			UUID: uuid.NewV4().String(), ExternalID: preparedRun.RunID,
		},
		Now: now,
	})
	if errors.Is(err, db.ErrWorkspaceArchived) {
		return w.recordFailure(ctx, deployment, runError("workspace_archived_error", "Workspace is archived"), scheduledAt, now)
	}
	if errors.Is(err, db.ErrFileReferenceNotFound) {
		return w.recordFailure(ctx, deployment, runErrorForReference("file", db.ErrNotFound, false), scheduledAt, now)
	}
	if errors.Is(err, db.ErrFilestorePathExists) {
		return w.recordFailure(ctx, deployment, runError("session_creation_rejected_error", "Session resource paths conflict"), scheduledAt, now)
	}
	return err
}

func (w *scheduledDeploymentWorker) recordFailure(
	ctx context.Context,
	deployment db.Deployment,
	failure *deploymentRunError,
	scheduledAt time.Time,
	now time.Time,
) error {
	runID, err := ids.New("drun_")
	if err != nil {
		return err
	}
	runErrorJSON, err := jsonx.Encode(failure)
	if err != nil {
		return err
	}
	var pausedReasonJSON json.RawMessage
	if shouldAutoPause(failure) {
		pausedReasonJSON, err = jsonx.Encode(deploymentPausedReason{Type: "error", Error: failure})
		if err != nil {
			return err
		}
	}
	return w.applyOccurrence(ctx, db.ApplyScheduledOccurrenceInput{
		Deployment: deployment, ScheduledAt: scheduledAt,
		Run: db.DeploymentRun{
			UUID: uuid.NewV4().String(), ExternalID: runID, Error: runErrorJSON,
		},
		AutoPauseReason: pausedReasonJSON, Now: now,
	})
}

func (w *scheduledDeploymentWorker) applyOccurrence(
	ctx context.Context,
	input db.ApplyScheduledOccurrenceInput,
) error {
	err := w.store.ApplyScheduledOccurrence(ctx, input)
	if errors.Is(err, db.ErrStaleSchedule) {
		return nil
	}
	return err
}

func shouldAutoPause(runError *deploymentRunError) bool {
	switch runError.Type {
	case "environment_archived_error",
		"agent_archived_error",
		"environment_not_found_error",
		"vault_not_found_error",
		"file_not_found_error",
		"session_resource_not_found_error",
		"workspace_archived_error",
		"organization_disabled_error",
		"memory_store_archived_error",
		"skill_not_found_error",
		"vault_archived_error",
		"unknown_error",
		"self_hosted_resources_unsupported_error",
		"mcp_egress_blocked_error":
		return true
	default:
		return false
	}
}

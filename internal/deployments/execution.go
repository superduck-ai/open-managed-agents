package deployments

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessioneventfiles"
)

var errRetryableRunPreparation = errors.New("retryable deployment run preparation")

func markRunPreparationRetryable(err error) error {
	return fmt.Errorf("%w: %v", errRetryableRunPreparation, err)
}

type preparedDeploymentExecution struct {
	RunID   string
	Session db.CreateSessionInput
	Events  []db.SessionEvent
}

func prepareDeploymentExecution(
	deployment db.Deployment,
	createdByAPIKeyUUID string,
	storedResources []deploymentRunResource,
	now time.Time,
) (preparedDeploymentExecution, error) {
	sessionID, threadID, workID, runID, err := newRunIDs()
	if err != nil {
		return preparedDeploymentExecution{}, err
	}
	events, outcomes, err := sessionEventsFromInitialEvents(deployment.InitialEvents, now)
	if err != nil {
		return preparedDeploymentExecution{}, err
	}
	resourcePlan, err := planDeploymentSessionResources(deployment, storedResources, now)
	if err != nil {
		return preparedDeploymentExecution{}, err
	}
	for _, event := range events {
		if err := sessioneventfiles.ValidateMountedReferences(event.EventType, event.Payload, resourcePlan.eventBindings); err != nil {
			return preparedDeploymentExecution{}, err
		}
	}
	deploymentID := deployment.ExternalID
	return preparedDeploymentExecution{
		RunID:  runID,
		Events: events,
		Session: db.CreateSessionInput{
			Session: db.Session{
				UUID: uuid.NewV4().String(), ExternalID: sessionID,
				OrganizationUUID: deployment.OrganizationUUID, WorkspaceUUID: deployment.WorkspaceUUID,
				CreatedByAPIKeyUUID: createdByAPIKeyUUID,
				EnvironmentUUID:     deployment.EnvironmentUUID, EnvironmentExternalID: deployment.EnvironmentExternalID,
				AgentUUID: deployment.AgentUUID, AgentExternalID: deployment.AgentExternalID,
				AgentVersion: deployment.AgentVersion, AgentSnapshot: deployment.AgentSnapshot,
				DeploymentUUID: &deployment.UUID, DeploymentID: &deploymentID,
				Metadata: jsonx.Default(deployment.Metadata, `{}`), VaultIDs: jsonx.Default(deployment.VaultIDs, `[]`),
				Status: "idle", Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`),
				OutcomeEvaluations: outcomes, CreatedAt: now, UpdatedAt: now,
			},
			Thread: db.SessionThread{
				UUID: uuid.NewV4().String(), ExternalID: threadID,
				OrganizationUUID: deployment.OrganizationUUID, WorkspaceUUID: deployment.WorkspaceUUID,
				AgentSnapshot: deployment.AgentSnapshot, Status: "idle",
				Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
			},
			Resources: resourcePlan.resources,
			Work: db.EnvironmentWork{
				UUID: uuid.NewV4().String(), ExternalID: workID,
				OrganizationUUID: deployment.OrganizationUUID, WorkspaceUUID: deployment.WorkspaceUUID,
				EnvironmentUUID: deployment.EnvironmentUUID, EnvironmentExternalID: deployment.EnvironmentExternalID,
				Metadata: json.RawMessage(`{}`), State: "queued", CreatedAt: now, UpdatedAt: now,
			},
		},
	}, nil
}

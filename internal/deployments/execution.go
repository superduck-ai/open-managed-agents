package deployments

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var errRetryableRunPreparation = errors.New("retryable deployment run preparation")

func markRunPreparationRetryable(err error) error {
	return fmt.Errorf("%w: %v", errRetryableRunPreparation, err)
}

type preparedDeploymentRun struct {
	RunID   string
	Session db.CreateSessionInput
	Events  []db.SessionEvent
}

type deploymentSessionWork struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

func prepareDeploymentRun(deployment db.Deployment, now time.Time) (preparedDeploymentRun, error) {
	sessionID, threadID, workID, runID, err := newRunIDs()
	if err != nil {
		return preparedDeploymentRun{}, err
	}
	events, outcomes, err := sessionEventsFromInitialEvents(deployment.InitialEvents, now)
	if err != nil {
		return preparedDeploymentRun{}, err
	}
	resources, err := sessionResourcesFromDeployment(deployment, now)
	if err != nil {
		return preparedDeploymentRun{}, err
	}
	deploymentID := deployment.ExternalID
	workData, err := jsonx.Encode(deploymentSessionWork{ID: sessionID, Type: "session"})
	if err != nil {
		return preparedDeploymentRun{}, err
	}
	return preparedDeploymentRun{
		RunID:  runID,
		Events: events,
		Session: db.CreateSessionInput{
			Session: db.Session{
				UUID: uuid.NewString(), ExternalID: sessionID,
				OrganizationUUID: deployment.OrganizationUUID, WorkspaceUUID: deployment.WorkspaceUUID,
				CreatedByAPIKeyUUID: deployment.CreatedByAPIKeyUUID,
				EnvironmentUUID:     deployment.EnvironmentUUID, EnvironmentExternalID: deployment.EnvironmentExternalID,
				AgentUUID: deployment.AgentUUID, AgentExternalID: deployment.AgentExternalID,
				AgentVersion: deployment.AgentVersion, AgentSnapshot: deployment.AgentSnapshot,
				DeploymentUUID: &deployment.UUID, DeploymentID: &deploymentID,
				Metadata: jsonx.Default(deployment.Metadata, `{}`), VaultIDs: jsonx.Default(deployment.VaultIDs, `[]`),
				Status: "idle", Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`),
				OutcomeEvaluations: outcomes, CreatedAt: now, UpdatedAt: now,
			},
			Thread: db.SessionThread{
				UUID: uuid.NewString(), ExternalID: threadID,
				OrganizationUUID: deployment.OrganizationUUID, WorkspaceUUID: deployment.WorkspaceUUID,
				AgentSnapshot: deployment.AgentSnapshot, Status: "idle",
				Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
			},
			Resources: resources,
			Work: db.EnvironmentWork{
				UUID: uuid.NewString(), ExternalID: workID,
				OrganizationUUID: deployment.OrganizationUUID, WorkspaceUUID: deployment.WorkspaceUUID,
				EnvironmentUUID: deployment.EnvironmentUUID, EnvironmentExternalID: deployment.EnvironmentExternalID,
				Data: workData, Metadata: json.RawMessage(`{}`), State: "queued", CreatedAt: now, UpdatedAt: now,
			},
		},
	}, nil
}

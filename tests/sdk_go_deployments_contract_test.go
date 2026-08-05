package tests

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestGoSDKDeploymentsContract(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("deployments-sdk-contract-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"deployments-sdk-contract-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	environment := createEnvironment(t, app, `{"name":"deployments-sdk-contract-environment"}`)
	defer cleanupEnvironmentRows(t, app.db, environment.ID)

	client := anthropic.NewClient(
		option.WithBaseURL(app.baseURL),
		option.WithAPIKey(defaultTestKey),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	created, err := client.Beta.Deployments.New(ctx, anthropic.BetaDeploymentNewParams{
		Agent:         anthropic.BetaDeploymentNewParamsAgentUnion{OfString: anthropic.String(agent.ID)},
		EnvironmentID: environment.ID,
		InitialEvents: []anthropic.BetaManagedAgentsDeploymentInitialEventParamsUnion{{
			OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
				Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
				Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{
					OfText: &anthropic.BetaManagedAgentsTextBlockParam{
						Type: anthropic.BetaManagedAgentsTextBlockTypeText,
						Text: "Run the deployment contract test.",
					},
				}},
			},
		}},
		Name: "SDK deployment contract",
	})
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	defer cleanupDeploymentRows(t, app, created.ID)
	if !strings.HasPrefix(created.ID, "depl_") {
		t.Fatalf("deployment ID = %q, want depl_ prefix", created.ID)
	}

	page, err := client.Beta.Deployments.List(ctx, anthropic.BetaDeploymentListParams{
		AgentID: anthropic.String(agent.ID),
		Limit:   anthropic.Int(20),
	})
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}
	if !slices.ContainsFunc(page.Data, func(deployment anthropic.BetaManagedAgentsDeployment) bool {
		return deployment.ID == created.ID
	}) {
		t.Fatalf("list deployments data = %+v", page.Data)
	}

	retrieved, err := client.Beta.Deployments.Get(ctx, created.ID, anthropic.BetaDeploymentGetParams{})
	if err != nil || retrieved.ID != created.ID {
		t.Fatalf("get deployment: err=%v deployment=%+v", err, retrieved)
	}

	updated, err := client.Beta.Deployments.Update(ctx, created.ID, anthropic.BetaDeploymentUpdateParams{
		Name: anthropic.String("Updated SDK deployment contract"),
	})
	if err != nil || updated.Name != "Updated SDK deployment contract" {
		t.Fatalf("update deployment: err=%v deployment=%+v", err, updated)
	}

	paused, err := client.Beta.Deployments.Pause(ctx, created.ID, anthropic.BetaDeploymentPauseParams{})
	if err != nil || paused.Status != anthropic.BetaManagedAgentsDeploymentStatusPaused {
		t.Fatalf("pause deployment: err=%v deployment=%+v", err, paused)
	}

	run, err := client.Beta.Deployments.Run(ctx, created.ID, anthropic.BetaDeploymentRunParams{})
	if err != nil || !strings.HasPrefix(run.ID, "drun_") || run.DeploymentID != created.ID || run.SessionID == "" {
		t.Fatalf("run paused deployment: err=%v run=%+v", err, run)
	}
	defer deleteSession(t, app, run.SessionID)
	stillPaused, err := client.Beta.Deployments.Get(ctx, created.ID, anthropic.BetaDeploymentGetParams{})
	if err != nil || stillPaused.Status != anthropic.BetaManagedAgentsDeploymentStatusPaused {
		t.Fatalf("deployment after manual run: err=%v deployment=%+v", err, stillPaused)
	}

	unpaused, err := client.Beta.Deployments.Unpause(ctx, created.ID, anthropic.BetaDeploymentUnpauseParams{})
	if err != nil || unpaused.Status != anthropic.BetaManagedAgentsDeploymentStatusActive {
		t.Fatalf("unpause deployment: err=%v deployment=%+v", err, unpaused)
	}

	runPage, err := client.Beta.DeploymentRuns.List(ctx, anthropic.BetaDeploymentRunListParams{
		DeploymentID: anthropic.String(created.ID),
		TriggerType:  anthropic.BetaManagedAgentsTriggerTypeManual,
		Limit:        anthropic.Int(20),
	})
	if err != nil {
		t.Fatalf("list deployment runs: %v", err)
	}
	if !slices.ContainsFunc(runPage.Data, func(deploymentRun anthropic.BetaManagedAgentsDeploymentRun) bool {
		return deploymentRun.ID == run.ID
	}) {
		t.Fatalf("list deployment runs data = %+v", runPage.Data)
	}

	retrievedRun, err := client.Beta.DeploymentRuns.Get(ctx, run.ID, anthropic.BetaDeploymentRunGetParams{})
	if err != nil || retrievedRun.ID != run.ID || retrievedRun.DeploymentID != created.ID {
		t.Fatalf("get deployment run: err=%v run=%+v", err, retrievedRun)
	}

	archived, err := client.Beta.Deployments.Archive(ctx, created.ID, anthropic.BetaDeploymentArchiveParams{})
	if err != nil || archived.ArchivedAt.IsZero() {
		t.Fatalf("archive deployment: err=%v deployment=%+v", err, archived)
	}
}

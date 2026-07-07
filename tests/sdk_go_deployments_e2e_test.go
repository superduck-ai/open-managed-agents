//go:build e2e
// +build e2e

package tests

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestGoSDKDeploymentsManualRunE2E(t *testing.T) {
	baseURL := os.Getenv("TEST_API_BASE_URL")
	if baseURL == "" {
		app := newTestApp(t, nil)
		t.Cleanup(app.close)
		baseURL = app.baseURL
	}

	client := anthropic.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(defaultTestKey),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	nameSuffix := fmt.Sprintf("%d", time.Now().UnixNano())

	var agentID, environmentID, deploymentID, sessionID string
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if deploymentID != "" {
			if _, err := client.Beta.Deployments.Archive(cleanupCtx, deploymentID, anthropic.BetaDeploymentArchiveParams{}); err != nil {
				t.Logf("cleanup archive deployment %s: %v", deploymentID, err)
			}
		}
		if sessionID != "" {
			if _, err := client.Beta.Sessions.Delete(cleanupCtx, sessionID, anthropic.BetaSessionDeleteParams{}); err != nil {
				t.Logf("cleanup delete session %s: %v", sessionID, err)
			}
		}
		if agentID != "" {
			if _, err := client.Beta.Agents.Archive(cleanupCtx, agentID, anthropic.BetaAgentArchiveParams{}); err != nil {
				t.Logf("cleanup archive agent %s: %v", agentID, err)
			}
		}
		if environmentID != "" {
			if _, err := client.Beta.Environments.Delete(cleanupCtx, environmentID, anthropic.BetaEnvironmentDeleteParams{}); err != nil {
				t.Logf("cleanup delete environment %s: %v", environmentID, err)
			}
		}
	})

	agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
		Name: "go-sdk-deployment-agent-" + nameSuffix,
		Model: anthropic.BetaManagedAgentsModelConfigParams{
			ID: anthropic.BetaManagedAgentsModelClaudeOpus4_6,
		},
		System: anthropic.String("You are a concise order support assistant."),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	agentID = agent.ID

	environment, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
		Name: "go-sdk-deployment-env-" + nameSuffix,
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	environmentID = environment.ID

	const prompt = "Please check order 1234 and summarize the next action."
	deployment, err := client.Beta.Deployments.New(ctx, anthropic.BetaDeploymentNewParams{
		Agent: anthropic.BetaDeploymentNewParamsAgentUnion{
			OfString: anthropic.String(agent.ID),
		},
		EnvironmentID: environment.ID,
		InitialEvents: []anthropic.BetaManagedAgentsDeploymentInitialEventParamsUnion{{
			OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
				Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
				Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{
					OfText: &anthropic.BetaManagedAgentsTextBlockParam{
						Type: anthropic.BetaManagedAgentsTextBlockTypeText,
						Text: prompt,
					},
				}},
			},
		}},
		Name: "go-sdk-manual-run-deployment-" + nameSuffix,
	})
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	deploymentID = deployment.ID
	if !strings.HasPrefix(deployment.ID, "dep_") || deployment.EnvironmentID != environment.ID {
		t.Fatalf("unexpected deployment: %+v", deployment)
	}

	run, err := client.Beta.Deployments.Run(ctx, deployment.ID, anthropic.BetaDeploymentRunParams{})
	if err != nil {
		t.Fatalf("manual run deployment: %v", err)
	}
	if !strings.HasPrefix(run.ID, "drun_") {
		t.Fatalf("run id = %q, want drun_ prefix", run.ID)
	}
	if run.DeploymentID != deployment.ID {
		t.Fatalf("run deployment_id = %s, want %s", run.DeploymentID, deployment.ID)
	}
	if run.TriggerContext.Type != "manual" {
		t.Fatalf("run trigger type = %q, want manual", run.TriggerContext.Type)
	}
	if run.SessionID == "" {
		t.Fatal("run session_id is empty")
	}
	if run.Error.Type != "" {
		t.Fatalf("run error = %+v, want null", run.Error)
	}
	sessionID = run.SessionID

	gotRun, err := client.Beta.DeploymentRuns.Get(ctx, run.ID, anthropic.BetaDeploymentRunGetParams{})
	if err != nil {
		t.Fatalf("get deployment run: %v", err)
	}
	if gotRun.ID != run.ID || gotRun.SessionID != run.SessionID || gotRun.DeploymentID != deployment.ID {
		t.Fatalf("unexpected retrieved run: %+v", gotRun)
	}

	runPage, err := client.Beta.DeploymentRuns.List(ctx, anthropic.BetaDeploymentRunListParams{
		DeploymentID: anthropic.String(deployment.ID),
		TriggerType:  anthropic.BetaManagedAgentsTriggerTypeManual,
		HasError:     anthropic.Bool(false),
		Limit:        anthropic.Int(20),
	})
	if err != nil {
		t.Fatalf("list deployment runs: %v", err)
	}
	if !goSDKDeploymentRunPageContains(runPage.Data, run.ID) {
		t.Fatalf("deployment run %s not found in filtered list: %+v", run.ID, runPage.Data)
	}

	session, err := client.Beta.Sessions.Get(ctx, run.SessionID, anthropic.BetaSessionGetParams{})
	if err != nil {
		t.Fatalf("get run session: %v", err)
	}
	if session.DeploymentID != deployment.ID {
		t.Fatalf("session deployment_id = %q, want %q", session.DeploymentID, deployment.ID)
	}
	if session.EnvironmentID != environment.ID {
		t.Fatalf("session environment_id = %q, want %q", session.EnvironmentID, environment.ID)
	}

	sessionPage, err := client.Beta.Sessions.List(ctx, anthropic.BetaSessionListParams{
		DeploymentID: anthropic.String(deployment.ID),
		Limit:        anthropic.Int(20),
	})
	if err != nil {
		t.Fatalf("list sessions by deployment: %v", err)
	}
	if !goSDKSessionPageContains(sessionPage.Data, run.SessionID) {
		t.Fatalf("session %s not found in deployment-filtered list: %+v", run.SessionID, sessionPage.Data)
	}

	eventPage, err := client.Beta.Sessions.Events.List(ctx, run.SessionID, anthropic.BetaSessionEventListParams{
		Order: anthropic.BetaSessionEventListParamsOrderAsc,
		Limit: anthropic.Int(20),
	})
	if err != nil {
		t.Fatalf("list session events: %v", err)
	}
	if !goSDKEventPageContainsUserMessage(eventPage.Data, prompt) {
		t.Fatalf("initial user.message event not found in session events: %+v", eventPage.Data)
	}
}

func TestGoSDKDeploymentsManualRunRealSandboxE2E(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !quickstartShouldRunRealSandbox(cfg) {
		t.Skip("real deployment sandbox E2E requires E2B_API_KEY and E2B_DEBUG=false")
	}
	if externalBaseURL := strings.TrimSpace(os.Getenv("TEST_API_BASE_URL")); externalBaseURL != "" {
		t.Logf("Ignoring TEST_API_BASE_URL=%s; this test starts its own in-process API server so it can run the environment runner and inspect E2B sandbox state", externalBaseURL)
	}
	quickstartRequireRealSandboxConfig(t, cfg)
	if cfg.E2BRequestTimeout < 2*time.Minute {
		cfg.E2BRequestTimeout = 2 * time.Minute
	}
	if cfg.E2BSandboxTimeout < 2*time.Minute {
		cfg.E2BSandboxTimeout = 2 * time.Minute
	}

	app := newTestAppWithStore(t, &cfg, newFakeStore("deployments-real-sandbox-bucket"))
	defer app.close()

	client := anthropic.NewClient(
		option.WithBaseURL(app.baseURL),
		option.WithAPIKey(defaultTestKey),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	nameSuffix := fmt.Sprintf("%d", time.Now().UnixNano())

	agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
		Name: "deployment-sandbox-agent-" + nameSuffix,
		Model: anthropic.BetaManagedAgentsModelConfigParams{
			ID: anthropic.BetaManagedAgentsModelClaudeOpus4_8,
		},
		System: anthropic.String("You are a helpful coding assistant. Write clean, well-documented code."),
		Tools: []anthropic.BetaAgentNewParamsToolUnion{{
			OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{
				Type: anthropic.BetaManagedAgentsAgentToolset20260401ParamsTypeAgentToolset20260401,
			},
		}},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Logf("Agent ID: %s, version: %d", agent.ID, agent.Version)
	defer client.Beta.Agents.Archive(context.Background(), agent.ID, anthropic.BetaAgentArchiveParams{})

	environment, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
		Name: "deployment-sandbox-env-" + nameSuffix,
		Config: anthropic.BetaEnvironmentNewParamsConfigUnion{
			OfCloud: &anthropic.BetaCloudConfigParams{
				Networking: anthropic.BetaCloudConfigParamsNetworkingUnion{
					OfUnrestricted: &anthropic.BetaUnrestrictedNetworkParam{},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	t.Logf("Environment ID: %s", environment.ID)
	defer client.Beta.Environments.Delete(context.Background(), environment.ID, anthropic.BetaEnvironmentDeleteParams{})

	const prompt = "Create a Python script that generates the first 20 Fibonacci numbers and saves them to fibonacci.txt"
	deployment, err := client.Beta.Deployments.New(ctx, anthropic.BetaDeploymentNewParams{
		Agent: anthropic.BetaDeploymentNewParamsAgentUnion{
			OfString: anthropic.String(agent.ID),
		},
		EnvironmentID: environment.ID,
		InitialEvents: []anthropic.BetaManagedAgentsDeploymentInitialEventParamsUnion{{
			OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
				Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
				Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{
					OfText: &anthropic.BetaManagedAgentsTextBlockParam{
						Type: anthropic.BetaManagedAgentsTextBlockTypeText,
						Text: prompt,
					},
				}},
			},
		}},
		Name: "deployment-sandbox-" + nameSuffix,
	})
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	t.Logf("Deployment ID: %s", deployment.ID)
	defer client.Beta.Deployments.Archive(context.Background(), deployment.ID, anthropic.BetaDeploymentArchiveParams{})

	run, err := client.Beta.Deployments.Run(ctx, deployment.ID, anthropic.BetaDeploymentRunParams{})
	if err != nil {
		t.Fatalf("manual run deployment: %v", err)
	}
	if !strings.HasPrefix(run.ID, "drun_") || run.DeploymentID != deployment.ID || run.TriggerContext.Type != "manual" || run.SessionID == "" || run.Error.Type != "" {
		t.Fatalf("unexpected deployment run: %+v", run)
	}
	t.Logf("Deployment Run ID: %s, Session ID: %s", run.ID, run.SessionID)
	defer client.Beta.Sessions.Delete(context.Background(), run.SessionID, anthropic.BetaSessionDeleteParams{})

	session, err := client.Beta.Sessions.Get(ctx, run.SessionID, anthropic.BetaSessionGetParams{})
	if err != nil {
		t.Fatalf("get run session: %v", err)
	}
	if session.DeploymentID != deployment.ID || session.EnvironmentID != environment.ID {
		t.Fatalf("unexpected run-created session: %+v", session)
	}

	eventPage, err := client.Beta.Sessions.Events.List(ctx, run.SessionID, anthropic.BetaSessionEventListParams{
		Order: anthropic.BetaSessionEventListParamsOrderAsc,
		Limit: anthropic.Int(20),
	})
	if err != nil {
		t.Fatalf("list session events: %v", err)
	}
	if !goSDKEventPageContainsUserMessage(eventPage.Data, prompt) {
		t.Fatalf("deployment initial user.message event not found in session events: %+v", eventPage.Data)
	}

	quickstartRunRealSandbox(t, ctx, app, environment.ID, run.SessionID)
}

func goSDKDeploymentRunPageContains(runs []anthropic.BetaManagedAgentsDeploymentRun, id string) bool {
	for _, run := range runs {
		if run.ID == id {
			return true
		}
	}
	return false
}

func goSDKSessionPageContains(sessions []anthropic.BetaManagedAgentsSession, id string) bool {
	for _, session := range sessions {
		if session.ID == id {
			return true
		}
	}
	return false
}

func goSDKEventPageContainsUserMessage(events []anthropic.BetaManagedAgentsSessionEventUnion, text string) bool {
	for _, event := range events {
		if event.Type == "user.message" && strings.Contains(event.RawJSON(), text) {
			return true
		}
	}
	return false
}

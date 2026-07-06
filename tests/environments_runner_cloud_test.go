package tests

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestEnvironmentRunnerLaunchesManagedAgentCloudSession(t *testing.T) {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSessionAPIBaseURL = "http://code-session.example.test"
	cfg.CodeSessionSandboxAPIBaseURL = "http://code-session-sandbox.example.test"
	cfg.EnvironmentManagerPath = "/usr/local/bin/environment-manager"
	cfg.ClaudePath = "/opt/claude-code/bin/claude"
	cfg.ClaudeAgentVersion = "2.1.120"
	cfg.E2BTemplate = "fake-template"

	app := newTestAppWithStore(t, &cfg, newFakeStore("runner-cloud-bucket"))
	defer app.close()

	client := anthropic.NewClient(
		option.WithBaseURL(app.baseURL),
		option.WithAPIKey(defaultTestKey),
	)
	agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
		Name: "Runner Bridge Agent",
		Model: anthropic.BetaManagedAgentsModelConfigParams{
			ID: anthropic.BetaManagedAgentsModelClaudeOpus4_8,
		},
		System: anthropic.String("You are a concise coding assistant."),
		Tools: []anthropic.BetaAgentNewParamsToolUnion{{
			OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{
				Type: anthropic.BetaManagedAgentsAgentToolset20260401ParamsTypeAgentToolset20260401,
			},
		}},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	defer client.Beta.Agents.Archive(context.Background(), agent.ID, anthropic.BetaAgentArchiveParams{})

	environment, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
		Name: "runner-cloud-" + strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""),
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
	defer client.Beta.Environments.Delete(context.Background(), environment.ID, anthropic.BetaEnvironmentDeleteParams{})

	session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
		Agent:         anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agent.ID)},
		EnvironmentID: environment.ID,
		Title:         anthropic.String("Runner bridge session"),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer client.Beta.Sessions.Delete(context.Background(), session.ID, anthropic.BetaSessionDeleteParams{})

	const prompt = "Say hello from the runner bridge test"
	if _, err := client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
		Events: []anthropic.BetaManagedAgentsEventParamsUnion{{
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
	}); err != nil {
		t.Fatalf("send initial event: %v", err)
	}

	provider := &recordingRunnerProvider{sandboxID: "sandbox-runner-bridge"}
	runner := environments.NewRunnerWithConfig(app.db, provider, cfg)
	processed, err := runner.RunOnce(ctx, "runner-cloud-test")
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if !processed {
		t.Fatal("runner did not process queued session work")
	}

	apiKey, err := app.db.GetAPIKey(ctx, auth.HashAPIKey(defaultTestKey))
	if err != nil {
		t.Fatalf("load api key: %v", err)
	}
	codeSession, err := app.db.GetCodeSessionBySessionExternalID(ctx, apiKey.WorkspaceID, session.ID)
	if err != nil {
		t.Fatalf("load local code session: %v", err)
	}
	if !strings.HasPrefix(codeSession.ExternalID, "cse_") || codeSession.SessionExternalID != session.ID || codeSession.EnvironmentExternalID != environment.ID {
		t.Fatalf("unexpected local code session: %#v", codeSession)
	}
	if codeSession.PermissionMode != "bypassPermissions" {
		t.Fatalf("local code session permission mode = %q", codeSession.PermissionMode)
	}
	queued, err := app.db.ListQueuedCodeSessionInboundEvents(ctx, codeSession.ExternalID)
	if err != nil {
		t.Fatalf("list queued inbound events: %v", err)
	}
	if len(queued) != 2 || queued[0].EventType != "control_request" || queued[0].EventSubtype != "initialize" || queued[1].EventType != "user" {
		t.Fatalf("unexpected queued inbound events: %#v", queued)
	}
	var initial map[string]any
	if err := json.Unmarshal(queued[1].Payload, &initial); err != nil {
		t.Fatalf("decode initial worker event: %v", err)
	}
	message := initial["message"].(map[string]any)
	if initial["type"] != "user" || initial["session_id"] != codeSession.ExternalID || message["content"] != prompt {
		t.Fatalf("initial worker event was not converted: %#v", initial)
	}

	if len(provider.writes) != 1 || provider.writes[0].sandboxID != "sandbox-runner-bridge" {
		t.Fatalf("unexpected sandbox writes: %#v", provider.writes)
	}
	if !strings.HasSuffix(provider.writes[0].path, "/environment-manager.v0.json") {
		t.Fatalf("unexpected stdin path: %s", provider.writes[0].path)
	}
	var payload map[string]any
	if err := json.Unmarshal(provider.writes[0].data, &payload); err != nil {
		t.Fatalf("decode environment-manager payload: %v", err)
	}
	startup := payload["startup_context"].(map[string]any)
	if startup["api_base_url"] != "http://code-session-sandbox.example.test" || startup["session_id"] != codeSession.ExternalID || startup["use_code_sessions"] != true {
		t.Fatalf("unexpected startup context: %#v", startup)
	}
	auths := payload["auth"].([]any)
	sessionAuth := auths[0].(map[string]any)
	if sessionAuth["type"] != "session_ingress" || sessionAuth["token"] != codeSession.ExternalID {
		t.Fatalf("unexpected session auth: %#v", sessionAuth)
	}
	if len(provider.commands) != 1 || !strings.Contains(provider.commands[0], "--session '"+codeSession.ExternalID+"'") {
		t.Fatalf("unexpected sandbox commands: %#v", provider.commands)
	}

	stored, err := app.db.GetSession(ctx, apiKey.WorkspaceID, session.ID)
	if err != nil {
		t.Fatalf("load stored session: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(stored.Metadata, &metadata); err != nil {
		t.Fatalf("decode session metadata: %v", err)
	}
	if metadata["claude_code_session_id"] != codeSession.ExternalID || metadata["claude_code_sdk_url_path"] != "/v1/code/sessions/"+codeSession.ExternalID || metadata["runtime"] != "claude_code_local" {
		t.Fatalf("session metadata was not patched: %#v", metadata)
	}
}

type recordingRunnerProvider struct {
	sandboxID string
	writes    []recordedSandboxWrite
	commands  []string
}

type recordedSandboxWrite struct {
	sandboxID string
	path      string
	data      []byte
}

func (p *recordingRunnerProvider) Resolve(env db.Environment, work *db.EnvironmentWork) (e2bruntime.Resolution, error) {
	return e2bruntime.Resolution{
		Template:            "fake-template",
		Metadata:            map[string]string{"environment_id": env.ExternalID},
		Envs:                map[string]string{"ANTHROPIC_ENVIRONMENT_ID": env.ExternalID},
		Timeout:             time.Minute,
		AllowInternetAccess: true,
	}, nil
}

func (p *recordingRunnerProvider) Create(context.Context, db.Environment, *db.EnvironmentWork) (e2bruntime.Sandbox, error) {
	return e2bruntime.Sandbox{ID: p.sandboxID}, nil
}

func (p *recordingRunnerProvider) Kill(context.Context, string) error {
	return nil
}

func (p *recordingRunnerProvider) WriteFile(_ context.Context, sandboxID string, path string, data []byte) error {
	p.writes = append(p.writes, recordedSandboxWrite{sandboxID: sandboxID, path: path, data: append([]byte(nil), data...)})
	return nil
}

func (p *recordingRunnerProvider) RunCommand(_ context.Context, sandboxID string, command string) error {
	if sandboxID != p.sandboxID {
		p.commands = append(p.commands, "wrong sandbox: "+sandboxID)
		return nil
	}
	p.commands = append(p.commands, command)
	return nil
}

package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestEnvironmentRunnerLaunchesManagedAgentCloudSession(t *testing.T) {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.SandboxAPIBaseURL = "http://code-session-sandbox.example.test"
	cfg.EnvironmentRunner.ManagerPath = "/usr/local/bin/environment-manager"
	cfg.EnvironmentRunner.ClaudePath = "/opt/claude-code/bin/claude"
	cfg.EnvironmentRunner.ClaudeAgentVersion = "2.1.120"
	cfg.E2B.Template = "fake-template"
	cfg.AnthropicUpstream.APIKey = "sk-ant-upstream-must-not-enter-sandbox"

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
	runner := environments.NewRunnerWithConfigStoreAndCredentials(app.db, provider, cfg, nil, app.credentials)
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

	if len(provider.launches) != 1 || provider.launches[0].sandboxID != "sandbox-runner-bridge" {
		t.Fatalf("sandbox launch count/id = %d/%q, want 1/%q", len(provider.launches), firstLaunchSandboxID(provider.launches), "sandbox-runner-bridge")
	}
	var payload map[string]any
	if err := json.Unmarshal(provider.launches[0].stdin, &payload); err != nil {
		t.Fatalf("decode environment-manager payload: %v", err)
	}
	startup := payload["startup_context"].(map[string]any)
	if startup["api_base_url"] != "http://code-session-sandbox.example.test" || startup["session_id"] != codeSession.ExternalID || startup["use_code_sessions"] != true {
		t.Fatalf("unexpected startup context: %#v", startup)
	}
	auths := payload["auth"].([]any)
	sessionAuth := auths[0].(map[string]any)
	sessionIngressToken, _ := sessionAuth["token"].(string)
	if sessionAuth["type"] != "session_ingress" || !strings.HasPrefix(sessionIngressToken, "sk-ant-si-") {
		t.Fatalf("session auth type/token shape = %q/%t, want session_ingress/valid prefix", sessionAuth["type"], strings.HasPrefix(sessionIngressToken, "sk-ant-si-"))
	}
	modelAuth := auths[1].(map[string]any)
	modelAccessToken, _ := modelAuth["token"].(string)
	if modelAuth["type"] != "anthropic_oauth" || !strings.HasPrefix(modelAccessToken, "sk-ant-oat01-") {
		t.Fatalf("model auth type/token shape = %q/%t, want anthropic_oauth/valid prefix", modelAuth["type"], strings.HasPrefix(modelAccessToken, "sk-ant-oat01-"))
	}
	startupEnvironment := startup["environment_variables"].(map[string]any)
	if _, ok := startupEnvironment["CLAUDE_CODE_SESSION_ACCESS_TOKEN"]; ok {
		t.Fatalf("startup environment masks WebSocket auth FD: %#v", startupEnvironment)
	}
	if _, ok := payload["environment"].(map[string]any)["environment"]; ok {
		t.Fatalf("environment-manager payload should not contain Claude credential environment variables: %#v", payload["environment"])
	}
	if strings.Contains(string(provider.launches[0].stdin), cfg.AnthropicUpstream.APIKey) {
		t.Fatalf("environment-manager payload contains the configured upstream key (payload bytes=%d)", len(provider.launches[0].stdin))
	}
	if !strings.Contains(provider.launches[0].command, "--session '"+codeSession.ExternalID+"'") ||
		strings.Contains(provider.launches[0].command, "nohup") ||
		strings.Contains(provider.launches[0].command, "environment-manager.v0.json") {
		t.Fatalf("unexpected sandbox background command: %q", provider.launches[0].command)
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

func TestEnvironmentRunnerPackageProvisioning(t *testing.T) {
	t.Run("failure terminates sandbox before manager startup", func(t *testing.T) {
		provider, processed, err := runPackageEnvironment(t, errors.New("gem install failed"))
		if !processed || err == nil || !strings.Contains(err.Error(), "provision environment packages") {
			t.Fatalf("RunOnce() = (%t, %v), want processed provisioning failure", processed, err)
		}
		if len(provider.writes) != 2 || len(provider.commands) != 1 || len(provider.launches) != 0 {
			t.Fatalf("failure writes/commands/launches = %d/%d/%d, want manifest+provisioner, one command, no manager launch", len(provider.writes), len(provider.commands), len(provider.launches))
		}
		if strings.Contains(provider.commands[0], "@scope/package") || strings.Contains(provider.commands[0], "touch /tmp") {
			t.Fatalf("provision command contains package data: %q", provider.commands[0])
		}
		if !reflect.DeepEqual(provider.kills, []string{provider.sandboxID}) {
			t.Fatalf("killed sandboxes = %#v, want failed sandbox", provider.kills)
		}
		if provider.codeSessionCreated {
			t.Fatal("provisioning failure created an active code session")
		}
		if !provider.workHasMCPMetadata {
			t.Fatal("provisioning failure did not retain the pre-resolve MCP policy metadata")
		}
		if !provider.createSawMCPMetadata {
			t.Fatal("sandbox create did not receive the pre-resolve MCP policy metadata")
		}
	})

	t.Run("stop requested during provisioning terminates sandbox before manager startup", func(t *testing.T) {
		provider, processed, err := runPackageEnvironmentWithStop(t)
		if err != nil || !processed {
			t.Fatalf("RunOnce() = (%t, %v), want graceful stop", processed, err)
		}
		if len(provider.commands) != 1 || len(provider.launches) != 0 {
			t.Fatalf("stop commands/launches = %d/%d, want one provision command and no manager launch", len(provider.commands), len(provider.launches))
		}
		if !reflect.DeepEqual(provider.kills, []string{provider.sandboxID}) {
			t.Fatalf("killed sandboxes = %#v, want stopped sandbox", provider.kills)
		}
		if provider.codeSessionCreated {
			t.Fatal("graceful stop during provisioning created an active code session")
		}
		if !provider.workHasMCPMetadata {
			t.Fatal("graceful stop did not retain the pre-resolve MCP policy metadata")
		}
	})

	t.Run("sandbox is killed when persisting stopping state fails", func(t *testing.T) {
		provider, processed, err := runPackageEnvironmentWithStopStateFailure(t)
		if !processed || err == nil || !strings.Contains(err.Error(), "forced sandbox state update failure") {
			t.Fatalf("RunOnce() = (%t, %v), want stopping state persistence failure", processed, err)
		}
		if !reflect.DeepEqual(provider.kills, []string{provider.sandboxID}) {
			t.Fatalf("killed sandboxes = %#v, want sandbox killed despite state failure", provider.kills)
		}
		if provider.codeSessionCreated {
			t.Fatal("failed graceful stop created an active code session")
		}
		if !provider.workStopped {
			t.Fatal("failed stopping-state persistence left environment work running")
		}
	})

	t.Run("work is stopped when sandbox kill fails", func(t *testing.T) {
		provider, processed, err := runPackageEnvironmentWithStopKillFailure(t)
		if !processed || err == nil || !strings.Contains(err.Error(), "forced sandbox kill failure") {
			t.Fatalf("RunOnce() = (%t, %v), want sandbox kill failure", processed, err)
		}
		if !reflect.DeepEqual(provider.kills, []string{provider.sandboxID}) {
			t.Fatalf("killed sandboxes = %#v, want one attempted kill", provider.kills)
		}
		if !provider.workStopped {
			t.Fatal("sandbox kill failure left environment work running")
		}
	})

	t.Run("metadata failure rolls back code session creation", func(t *testing.T) {
		provider, processed, err := runPackageEnvironmentWithSessionMetadataFailure(t)
		if !processed || err == nil || !strings.Contains(err.Error(), "forced session metadata update failure") {
			t.Fatalf("RunOnce() = (%t, %v), want session metadata persistence failure", processed, err)
		}
		if provider.codeSessionCreated {
			t.Fatal("metadata failure left an active code session")
		}
		if provider.sessionHasRuntimeMetadata || provider.workHasRuntimeMetadata {
			t.Fatalf("metadata failure committed session/work runtime metadata = %t/%t", provider.sessionHasRuntimeMetadata, provider.workHasRuntimeMetadata)
		}
		if !provider.workHasMCPMetadata {
			t.Fatal("metadata transaction failure did not retain the pre-resolve MCP policy metadata")
		}
		if !reflect.DeepEqual(provider.kills, []string{provider.sandboxID}) {
			t.Fatalf("killed sandboxes = %#v, want failed sandbox", provider.kills)
		}
	})

	t.Run("success starts manager after fixed provisioner", func(t *testing.T) {
		provider, processed, err := runPackageEnvironment(t, nil)
		if err != nil || !processed {
			t.Fatalf("RunOnce() = (%t, %v), want success", processed, err)
		}
		if len(provider.writes) != 2 || len(provider.commands) != 1 || len(provider.launches) != 1 {
			t.Fatalf("success writes/commands/launches = %d/%d/%d, want manifest+provisioner, one provision command, one manager launch", len(provider.writes), len(provider.commands), len(provider.launches))
		}
		if !strings.HasSuffix(provider.writes[0].path, "/packages.v1.json") || !strings.HasSuffix(provider.writes[1].path, "/package-provisioner.v1.py") {
			t.Fatalf("sandbox write order = %#v", provider.writes)
		}
		if !strings.Contains(provider.commands[0], "package-provisioner.v1.py") || !strings.Contains(provider.launches[0].command, "task-run") {
			t.Fatalf("sandbox provision command/manager command = %q/%q", provider.commands[0], provider.launches[0].command)
		}
		if !reflect.DeepEqual(provider.operations, []string{"write:packages", "write:provisioner", "command:provision", "launch:manager"}) {
			t.Fatalf("sandbox operation order = %#v", provider.operations)
		}
		var manifest struct {
			Version  int `json:"version"`
			Packages struct {
				APT   []string `json:"apt"`
				Cargo []string `json:"cargo"`
				Gem   []string `json:"gem"`
				Go    []string `json:"go"`
				NPM   []string `json:"npm"`
				PIP   []string `json:"pip"`
			} `json:"packages"`
		}
		if err := json.Unmarshal(provider.writes[0].data, &manifest); err != nil {
			t.Fatalf("decode package manifest: %v", err)
		}
		if manifest.Version != 1 ||
			!reflect.DeepEqual(manifest.Packages.APT, []string{"ffmpeg"}) ||
			!reflect.DeepEqual(manifest.Packages.Cargo, []string{"ripgrep@14.1.1"}) ||
			!reflect.DeepEqual(manifest.Packages.Gem, []string{"rake:13.2.1"}) ||
			!reflect.DeepEqual(manifest.Packages.Go, []string{"golang.org/x/tools/cmd/goimports@v0.35.0"}) ||
			!reflect.DeepEqual(manifest.Packages.NPM, []string{"@scope/package@5.9.3"}) ||
			!reflect.DeepEqual(manifest.Packages.PIP, []string{`requests[socks] @ https://example.test/a.whl ; python_version >= "3.11"`, "name; touch /tmp/oma-package-shell"}) {
			t.Fatalf("package manifest changed specs: %#v", manifest)
		}
		if len(provider.kills) != 0 {
			t.Fatalf("successful sandbox was killed: %#v", provider.kills)
		}
		if !provider.codeSessionCreated {
			t.Fatal("successful provisioning did not create a code session")
		}
		if !provider.sessionHasRuntimeMetadata || !provider.workHasRuntimeMetadata {
			t.Fatalf("successful provisioning session/work runtime metadata = %t/%t, want true/true", provider.sessionHasRuntimeMetadata, provider.workHasRuntimeMetadata)
		}
		if !provider.workHasMCPMetadata {
			t.Fatal("successful provisioning did not retain MCP policy metadata")
		}
	})
}

func runPackageEnvironment(t *testing.T, commandErr error) (*recordingRunnerProvider, bool, error) {
	return runPackageEnvironmentWithHook(t, commandErr, nil)
}

func runPackageEnvironmentWithStop(t *testing.T) (*recordingRunnerProvider, bool, error) {
	return runPackageEnvironmentWithHook(t, nil, func(ctx context.Context, database *db.DB, environmentID string) {
		requestPackageEnvironmentStop(t, ctx, database, environmentID)
	})
}

func runPackageEnvironmentWithStopStateFailure(t *testing.T) (*recordingRunnerProvider, bool, error) {
	return runPackageEnvironmentWithHook(t, nil, func(ctx context.Context, database *db.DB, environmentID string) {
		requestPackageEnvironmentStop(t, ctx, database, environmentID)
		if _, err := database.Pool.Exec(ctx, `
			create or replace function oma_test_fail_sandbox_state_update() returns trigger
			language plpgsql as $$
			begin
				raise exception 'forced sandbox state update failure';
			end;
			$$;
			create trigger oma_test_fail_sandbox_state_update
			before update on environment_sandboxes
			for each row execute function oma_test_fail_sandbox_state_update()
		`); err != nil {
			t.Fatalf("install sandbox state failure trigger: %v", err)
		}
		t.Cleanup(func() {
			if _, err := database.Pool.Exec(context.Background(), `
				drop trigger if exists oma_test_fail_sandbox_state_update on environment_sandboxes;
				drop function if exists oma_test_fail_sandbox_state_update()
			`); err != nil {
				t.Fatalf("remove sandbox state failure trigger: %v", err)
			}
		})
	})
}

func runPackageEnvironmentWithStopKillFailure(t *testing.T) (*recordingRunnerProvider, bool, error) {
	return runPackageEnvironmentWithHookAndKill(t, nil, errors.New("forced sandbox kill failure"), func(ctx context.Context, database *db.DB, environmentID string) {
		requestPackageEnvironmentStop(t, ctx, database, environmentID)
	})
}

func runPackageEnvironmentWithSessionMetadataFailure(t *testing.T) (*recordingRunnerProvider, bool, error) {
	return runPackageEnvironmentWithHook(t, nil, func(ctx context.Context, database *db.DB, _ string) {
		if _, err := database.Pool.Exec(ctx, `
			create or replace function oma_test_fail_session_metadata_update() returns trigger
			language plpgsql as $$
			begin
				raise exception 'forced session metadata update failure';
			end;
			$$;
			create trigger oma_test_fail_session_metadata_update
			before update of metadata on sessions
			for each row execute function oma_test_fail_session_metadata_update()
		`); err != nil {
			t.Fatalf("install session metadata failure trigger: %v", err)
		}
		t.Cleanup(func() {
			if _, err := database.Pool.Exec(context.Background(), `
				drop trigger if exists oma_test_fail_session_metadata_update on sessions;
				drop function if exists oma_test_fail_session_metadata_update()
			`); err != nil {
				t.Fatalf("remove session metadata failure trigger: %v", err)
			}
		})
	})
}

func requestPackageEnvironmentStop(t *testing.T, ctx context.Context, database *db.DB, environmentID string) {
	t.Helper()
	ids := getDefaultDBIDs(t, database)
	works, _, err := database.ListEnvironmentWorkPage(ctx, db.ListEnvironmentWorkPageParams{
		WorkspaceID:           ids.WorkspaceID,
		EnvironmentExternalID: environmentID,
		Limit:                 10,
	})
	if err != nil || len(works) != 1 {
		t.Fatalf("list environment work count/error = %d/%v, want one work", len(works), err)
	}
	if _, err := database.StopEnvironmentWork(ctx, ids.WorkspaceID, environmentID, works[0].ExternalID, false); err != nil {
		t.Fatalf("request environment work stop: %v", err)
	}
}

func runPackageEnvironmentWithHook(
	t *testing.T,
	commandErr error,
	afterCommand func(context.Context, *db.DB, string),
) (*recordingRunnerProvider, bool, error) {
	return runPackageEnvironmentWithHookAndKill(t, commandErr, nil, afterCommand)
}

func runPackageEnvironmentWithHookAndKill(
	t *testing.T,
	commandErr error,
	killErr error,
	afterCommand func(context.Context, *db.DB, string),
) (*recordingRunnerProvider, bool, error) {
	t.Helper()
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.SandboxAPIBaseURL = "http://code-session-sandbox.example.test"
	cfg.EnvironmentRunner.ManagerPath = "/usr/local/bin/environment-manager"
	cfg.EnvironmentRunner.ClaudePath = "/opt/claude-code/bin/claude"
	cfg.EnvironmentRunner.ClaudeAgentVersion = "2.1.120"
	cfg.E2B.Template = "fake-template"
	app := newTestAppWithStore(t, &cfg, newFakeStore("runner-package-bucket"))
	t.Cleanup(app.close)
	agent := createAgent(t, app, `{
		"model":"claude-opus-4-8",
		"name":"Runner Package Agent",
		"mcp_servers":[{"type":"url","name":"notion","url":"https://mcp.notion.com/mcp"}]
	}`)
	t.Cleanup(func() { archiveAgent(t, app, agent.ID) })
	environment := createEnvironment(t, app, `{
		"name":"runner-packages-`+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")+`",
		"config":{"type":"cloud","networking":{"type":"unrestricted"},"packages":{
			"type":"packages","apt":["ffmpeg"],"cargo":["ripgrep@14.1.1"],"gem":["rake:13.2.1"],
			"go":["golang.org/x/tools/cmd/goimports@v0.35.0"],"npm":["@scope/package@5.9.3"],
			"pip":["requests[socks] @ https://example.test/a.whl ; python_version >= \"3.11\"","name; touch /tmp/oma-package-shell"]
		}}
	}`)
	t.Cleanup(func() { cleanupEnvironmentRows(t, app.db, environment.ID) })
	client := anthropic.NewClient(option.WithBaseURL(app.baseURL), option.WithAPIKey(defaultTestKey))
	session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
		Agent: anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agent.ID)}, EnvironmentID: environment.ID,
		Title: anthropic.String("Runner package session"),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = client.Beta.Sessions.Delete(context.Background(), session.ID, anthropic.BetaSessionDeleteParams{})
	})
	provider := &recordingRunnerProvider{sandboxID: "sandbox-runner-packages", commandErr: commandErr, killErr: killErr}
	if afterCommand != nil {
		provider.afterCommand = func() { afterCommand(ctx, app.db, environment.ID) }
	}
	runner := environments.NewRunnerWithConfigStoreAndCredentials(app.db, provider, cfg, app.store, app.credentials)
	processed, runErr := runner.RunOnce(ctx, "runner-package-test")
	if len(provider.creates) == 1 {
		provider.createSawMCPMetadata = hasJSONKey(provider.creates[0].metadata, "mcp_allowed_hosts")
	}
	ids := getDefaultDBIDs(t, app.db)
	_, codeSessionErr := app.db.GetCodeSessionBySessionExternalID(ctx, ids.WorkspaceID, session.ID)
	switch {
	case codeSessionErr == nil:
		provider.codeSessionCreated = true
	case errors.Is(codeSessionErr, db.ErrNotFound):
	default:
		t.Fatalf("look up package runner code session: %v", codeSessionErr)
	}
	works, _, workErr := app.db.ListEnvironmentWorkPage(ctx, db.ListEnvironmentWorkPageParams{
		WorkspaceID: ids.WorkspaceID, EnvironmentExternalID: environment.ID, Limit: 10,
	})
	if workErr != nil || len(works) != 1 {
		t.Fatalf("list package runner work count/error = %d/%v, want one work", len(works), workErr)
	}
	provider.workStopped = works[0].State == "stopped"
	provider.workHasRuntimeMetadata = hasJSONKey(works[0].Metadata, "claude_code_session_id")
	provider.workHasMCPMetadata = hasJSONKey(works[0].Metadata, "mcp_allowed_hosts")
	storedSession, sessionErr := app.db.GetSession(ctx, ids.WorkspaceID, session.ID)
	if sessionErr != nil {
		t.Fatalf("load package runner session: %v", sessionErr)
	}
	provider.sessionHasRuntimeMetadata = hasJSONKey(storedSession.Metadata, "claude_code_session_id")
	return provider, processed, runErr
}

func TestEnvironmentRunnerInstallsManagedAgentCustomSkill(t *testing.T) {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.SandboxAPIBaseURL = "http://code-session-sandbox.example.test"
	cfg.EnvironmentRunner.ManagerPath = "/usr/local/bin/environment-manager"
	cfg.EnvironmentRunner.ClaudePath = "/opt/claude-code/bin/claude"
	cfg.EnvironmentRunner.ClaudeAgentVersion = "2.1.120"
	cfg.E2B.Template = "fake-template"

	store := newFakeStore("runner-cloud-skills-bucket")
	app := newTestAppWithStore(t, &cfg, store)
	defer app.close()

	skill := createSkill(t, app, "runtime-skill")
	defer deleteSkill(t, app, skill.ID)
	agent := createAgent(t, app, `{
		"model":"claude-opus-4-8",
		"name":"Runner Skill Agent",
		"skills":[{"type":"custom","skill_id":"`+skill.ID+`","version":"latest"}]
	}`)
	defer archiveAgent(t, app, agent.ID)

	client := anthropic.NewClient(
		option.WithBaseURL(app.baseURL),
		option.WithAPIKey(defaultTestKey),
	)
	environment, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
		Name: "runner-cloud-skills-" + strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""),
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
		Title:         anthropic.String("Runner skills session"),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer client.Beta.Sessions.Delete(context.Background(), session.ID, anthropic.BetaSessionDeleteParams{})

	provider := &recordingRunnerProvider{sandboxID: "sandbox-runner-skills"}
	runner := environments.NewRunnerWithConfigStoreAndCredentials(app.db, provider, cfg, store, app.credentials)
	processed, err := runner.RunOnce(ctx, "runner-cloud-skills-test")
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if !processed {
		t.Fatal("runner did not process queued session work")
	}

	if len(provider.launches) != 1 {
		t.Fatalf("sandbox launch count = %d, want one environment-manager background process", len(provider.launches))
	}
	if len(provider.skillMounts) != 1 {
		t.Fatalf("skill mounts = %#v, want one prepared mount", provider.skillMounts)
	}
	mount := provider.skillMounts[0].mount
	if mount.MountPath != e2bruntime.SandboxSkillsMountPath || mount.VolumeName == "" || mount.ManifestSHA256 == "" {
		t.Fatalf("unexpected skill mount: %#v", mount)
	}
	if len(mount.Skills) != 1 || mount.Skills[0].Directory != "runtime-skill" {
		t.Fatalf("unexpected skill mount manifest: %#v", mount.Skills)
	}
	if len(provider.skillMounts[0].runtimeSkills) != 1 {
		t.Fatalf("runtime skills = %#v, want one", provider.skillMounts[0].runtimeSkills)
	}
	assertZipContains(t, provider.skillMounts[0].runtimeSkills[0].Archive, "runtime-skill/SKILL.md")
	if len(provider.creates) != 1 {
		t.Fatalf("sandbox creates = %#v, want one", provider.creates)
	}
	var workMetadata map[string]any
	if err := json.Unmarshal(provider.creates[0].metadata, &workMetadata); err != nil {
		t.Fatalf("decode work metadata: %v", err)
	}
	rawMount, ok := workMetadata[e2bruntime.SkillMountMetadataKey].(map[string]any)
	if !ok {
		t.Fatalf("work metadata missing skill mount: %#v", workMetadata)
	}
	if rawMount["mount_path"] != e2bruntime.SandboxSkillsMountPath || rawMount["volume_name"] != mount.VolumeName {
		t.Fatalf("unexpected work skill mount metadata: %#v", rawMount)
	}
	if strings.Contains(provider.launches[0].command, "installed managed agent skills") ||
		strings.Contains(provider.launches[0].command, "$HOME/.claude/skills") {
		t.Fatalf("sandbox command should not install managed agent skills directly: command=%q", provider.launches[0].command)
	}
}

func TestEnvironmentRunnerFailsWhenSkillResolverUnavailable(t *testing.T) {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.SandboxAPIBaseURL = "http://code-session-sandbox.example.test"
	cfg.EnvironmentRunner.ManagerPath = "/usr/local/bin/environment-manager"
	cfg.EnvironmentRunner.ClaudePath = "/opt/claude-code/bin/claude"
	cfg.EnvironmentRunner.ClaudeAgentVersion = "2.1.120"
	cfg.E2B.Template = "fake-template"

	store := newFakeStore("runner-cloud-missing-resolver-bucket")
	app := newTestAppWithStore(t, &cfg, store)
	defer app.close()

	skill := createSkill(t, app, "missing-resolver-skill")
	defer deleteSkill(t, app, skill.ID)
	agent := createAgent(t, app, `{
		"model":"claude-opus-4-8",
		"name":"Runner Missing Resolver Agent",
		"skills":[{"type":"custom","skill_id":"`+skill.ID+`","version":"latest"}]
	}`)
	defer archiveAgent(t, app, agent.ID)

	client := anthropic.NewClient(
		option.WithBaseURL(app.baseURL),
		option.WithAPIKey(defaultTestKey),
	)
	environment, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
		Name: "runner-cloud-no-resolver-" + strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""),
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
		Title:         anthropic.String("Runner missing resolver session"),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer client.Beta.Sessions.Delete(context.Background(), session.ID, anthropic.BetaSessionDeleteParams{})

	provider := &recordingRunnerProvider{sandboxID: "sandbox-should-not-start"}
	runner := environments.NewRunnerWithConfigStoreAndCredentials(app.db, provider, cfg, nil, app.credentials)
	processed, err := runner.RunOnce(ctx, "runner-cloud-no-resolver-test")
	if err == nil || !strings.Contains(err.Error(), "custom skill resolver is unavailable") {
		t.Fatalf("RunOnce error = %v, want custom resolver error", err)
	}
	if !processed {
		t.Fatal("runner did not process queued session work")
	}
	if len(provider.creates) != 0 || len(provider.commands) != 0 || len(provider.launches) != 0 {
		t.Fatalf("provider calls after missing resolver: creates/commands/launches=%d/%d/%d, want 0/0/0", len(provider.creates), len(provider.commands), len(provider.launches))
	}
}

func TestEnvironmentRunnerResolvesLimitedNetworkWithManagedAgentMCPHosts(t *testing.T) {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.SandboxAPIBaseURL = "http://code-session-sandbox.example.test"
	cfg.EnvironmentRunner.ManagerPath = "/usr/local/bin/environment-manager"
	cfg.EnvironmentRunner.ClaudePath = "/opt/claude-code/bin/claude"
	cfg.EnvironmentRunner.ClaudeAgentVersion = "2.1.120"
	cfg.E2B.Template = "fake-template"

	app := newTestAppWithStore(t, &cfg, newFakeStore("runner-cloud-network-order-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{
		"model":"claude-opus-4-8",
		"name":"Runner MCP Network Agent",
		"mcp_servers":[{"type":"url","name":"notion","url":"https://mcp.notion.com/mcp"}]
	}`)
	defer archiveAgent(t, app, agent.ID)
	environment := createEnvironment(t, app, `{
		"name":"runner-network-order-`+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")+`",
		"config":{
			"type":"cloud",
			"networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":true}
		}
	}`)
	defer cleanupEnvironmentRows(t, app.db, environment.ID)

	client := anthropic.NewClient(
		option.WithBaseURL(app.baseURL),
		option.WithAPIKey(defaultTestKey),
	)
	session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
		Agent:         anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agent.ID)},
		EnvironmentID: environment.ID,
		Title:         anthropic.String("Runner MCP network ordering session"),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer client.Beta.Sessions.Delete(context.Background(), session.ID, anthropic.BetaSessionDeleteParams{})

	provider := &recordingRunnerProvider{sandboxID: "sandbox-network-order"}
	runner := environments.NewRunnerWithConfigStoreAndCredentials(app.db, provider, cfg, nil, app.credentials)
	processed, err := runner.RunOnce(ctx, "runner-cloud-network-order-test")
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if !processed {
		t.Fatal("runner did not process queued session work")
	}
	if len(provider.resolves) != 1 {
		t.Fatalf("resolves = %#v, want one", provider.resolves)
	}
	if !hasJSONKey(provider.resolves[0].metadata, "mcp_allowed_hosts") {
		t.Fatalf("Resolve did not receive managed-agent MCP metadata: %s", provider.resolves[0].metadata)
	}
	if len(provider.creates) != 1 {
		t.Fatalf("creates = %#v, want one", provider.creates)
	}
	if !hasJSONKey(provider.creates[0].metadata, "mcp_allowed_hosts") {
		t.Fatalf("Create did not receive persisted MCP metadata: %s", provider.creates[0].metadata)
	}
	if provider.creates[0].resolution.Metadata["resolved_before_launch"] != "true" {
		t.Fatalf("Create did not use precomputed resolution: %#v", provider.creates[0].resolution)
	}
	if provider.creates[0].resolution.Network == nil {
		t.Fatalf("Create resolution has nil network, want limited network options")
	}
	allowOut, ok := provider.creates[0].resolution.Network.AllowOut.([]string)
	if !ok {
		t.Fatalf("Create resolution AllowOut = %#v, want []string", provider.creates[0].resolution.Network.AllowOut)
	}
	if !slices.Contains(allowOut, "mcp.notion.com") {
		t.Fatalf("Create resolution did not allow agent MCP host: %#v", allowOut)
	}
}

func TestEnvironmentRunnerClearsStaleMCPHosts(t *testing.T) {
	tests := []struct {
		name        string
		networking  string
		wantLimited bool
	}{
		{name: "current snapshot is empty", networking: `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":true}`, wantLimited: true},
		{name: "MCP access is disabled", networking: `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false}`, wantLimited: true},
		{name: "network is unrestricted", networking: `{"type":"unrestricted"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			cfg.CodeSession.SandboxAPIBaseURL = "http://code-session-sandbox.example.test"
			cfg.EnvironmentRunner.ManagerPath = "/usr/local/bin/environment-manager"
			cfg.EnvironmentRunner.ClaudePath = "/opt/claude-code/bin/claude"
			cfg.EnvironmentRunner.ClaudeAgentVersion = "2.1.120"
			cfg.E2B.Template = "fake-template"

			app := newTestAppWithStore(t, &cfg, newFakeStore("runner-cloud-stale-mcp-bucket"))
			defer app.close()

			agent := createAgent(t, app, `{"model":"claude-opus-4-8","name":"Runner Empty MCP Network Agent"}`)
			defer archiveAgent(t, app, agent.ID)
			environment := createEnvironment(t, app, `{
				"name":"runner-empty-mcp-`+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")+`",
				"config":{"type":"cloud","networking":`+test.networking+`}
			}`)
			defer cleanupEnvironmentRows(t, app.db, environment.ID)

			client := anthropic.NewClient(option.WithBaseURL(app.baseURL), option.WithAPIKey(defaultTestKey))
			session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
				Agent:         anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agent.ID)},
				EnvironmentID: environment.ID,
				Title:         anthropic.String("Runner empty MCP network session"),
			})
			if err != nil {
				t.Fatalf("create session: %v", err)
			}
			defer client.Beta.Sessions.Delete(context.Background(), session.ID, anthropic.BetaSessionDeleteParams{})

			ids := getDefaultDBIDs(t, app.db)
			work, err := app.db.GetLatestEnvironmentWorkByData(ctx, ids.WorkspaceID, environment.ID, "session", session.ID)
			if err != nil {
				t.Fatalf("load environment work: %v", err)
			}
			if _, err := app.db.UpdateEnvironmentWorkMetadata(ctx, ids.WorkspaceID, environment.ID, work.ExternalID,
				json.RawMessage(`{"mcp_allowed_hosts":["stale.example.com"]}`)); err != nil {
				t.Fatalf("seed stale MCP metadata: %v", err)
			}

			provider := &recordingRunnerProvider{sandboxID: "sandbox-empty-mcp"}
			runner := environments.NewRunnerWithConfigStoreAndCredentials(app.db, provider, cfg, nil, app.credentials)
			processed, err := runner.RunOnce(ctx, "runner-cloud-empty-mcp-test")
			if err != nil || !processed {
				t.Fatalf("RunOnce() = processed %v, error %v", processed, err)
			}
			if len(provider.resolves) != 1 {
				t.Fatalf("resolves = %#v, want one", provider.resolves)
			}
			var rawMetadata map[string]json.RawMessage
			if err := json.Unmarshal(provider.resolves[0].metadata, &rawMetadata); err != nil {
				t.Fatalf("decode Resolve metadata: %v", err)
			}
			if string(rawMetadata["mcp_allowed_hosts"]) != "[]" {
				t.Fatalf("empty MCP hosts metadata = %s, want []", rawMetadata["mcp_allowed_hosts"])
			}
			if len(provider.creates) != 1 {
				t.Fatalf("creates = %#v, want one", provider.creates)
			}
			network := provider.creates[0].resolution.Network
			if !test.wantLimited {
				if network != nil {
					t.Fatalf("unrestricted Create resolution network = %#v, want nil", network)
				}
				return
			}
			if network == nil {
				t.Fatal("limited Create resolution network is nil")
			}
			allowOut, ok := network.AllowOut.([]string)
			if !ok {
				t.Fatalf("limited Create resolution AllowOut = %#v, want []string", network.AllowOut)
			}
			if slices.Contains(allowOut, "stale.example.com") {
				t.Fatalf("Create resolution retained stale MCP host: %#v", allowOut)
			}
		})
	}
}

func TestEnvironmentRunnerDoesNotCreateCodeSessionWhenResolveFails(t *testing.T) {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.SandboxAPIBaseURL = "http://code-session-sandbox.example.test"
	cfg.EnvironmentRunner.ManagerPath = "/usr/local/bin/environment-manager"
	cfg.EnvironmentRunner.ClaudePath = "/opt/claude-code/bin/claude"
	cfg.EnvironmentRunner.ClaudeAgentVersion = "2.1.120"
	cfg.E2B.Template = "fake-template"

	app := newTestAppWithStore(t, &cfg, newFakeStore("runner-cloud-resolve-failure-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{
		"model":"claude-opus-4-8",
		"name":"Runner Resolve Failure Agent"
	}`)
	defer archiveAgent(t, app, agent.ID)
	environment := createEnvironment(t, app, `{"name":"runner-resolve-failure-`+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")+`"}`)
	defer cleanupEnvironmentRows(t, app.db, environment.ID)

	client := anthropic.NewClient(
		option.WithBaseURL(app.baseURL),
		option.WithAPIKey(defaultTestKey),
	)
	session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
		Agent:         anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agent.ID)},
		EnvironmentID: environment.ID,
		Title:         anthropic.String("Runner resolve failure session"),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer client.Beta.Sessions.Delete(context.Background(), session.ID, anthropic.BetaSessionDeleteParams{})

	provider := &recordingRunnerProvider{
		sandboxID:  "sandbox-should-not-start",
		resolveErr: fmt.Errorf("network config invalid"),
	}
	runner := environments.NewRunnerWithConfigStoreAndCredentials(app.db, provider, cfg, nil, app.credentials)
	processed, err := runner.RunOnce(ctx, "runner-cloud-resolve-failure-test")
	if err == nil || !strings.Contains(err.Error(), "network config invalid") {
		t.Fatalf("RunOnce error = %v, want resolve error", err)
	}
	if !processed {
		t.Fatal("runner did not process queued session work")
	}
	if len(provider.creates) != 0 || len(provider.commands) != 0 || len(provider.launches) != 0 {
		t.Fatalf("provider calls after resolve failure: creates/commands/launches=%d/%d/%d, want 0/0/0", len(provider.creates), len(provider.commands), len(provider.launches))
	}
	ids := getDefaultDBIDs(t, app.db)
	if _, err := app.db.GetCodeSessionBySessionExternalID(ctx, ids.WorkspaceID, session.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("code session lookup error = %v, want ErrNotFound", err)
	}
}

type recordingRunnerProvider struct {
	sandboxID                 string
	resolveErr                error
	commandErr                error
	killErr                   error
	afterCommand              func()
	resolves                  []recordedSandboxResolve
	writes                    []recordedSandboxWrite
	commands                  []string
	launches                  []recordedSandboxLaunch
	operations                []string
	creates                   []recordedSandboxCreate
	skillMounts               []recordedSkillMount
	kills                     []string
	codeSessionCreated        bool
	workStopped               bool
	sessionHasRuntimeMetadata bool
	workHasRuntimeMetadata    bool
	workHasMCPMetadata        bool
	createSawMCPMetadata      bool
}

type recordedSandboxResolve struct {
	metadata   json.RawMessage
	resolution e2bruntime.Resolution
}

type recordedSandboxLaunch struct {
	sandboxID string
	command   string
	stdin     []byte
}

func firstLaunchSandboxID(launches []recordedSandboxLaunch) string {
	if len(launches) == 0 {
		return ""
	}
	return launches[0].sandboxID
}

type recordedSandboxWrite struct {
	sandboxID string
	path      string
	data      []byte
}

type recordedSandboxCreate struct {
	metadata   json.RawMessage
	resolution e2bruntime.Resolution
}

type recordedSkillMount struct {
	mount         e2bruntime.SkillMount
	runtimeSkills []skillsapi.RuntimeSkill
}

func (p *recordingRunnerProvider) Resolve(env db.Environment, work *db.EnvironmentWork) (e2bruntime.Resolution, error) {
	record := recordedSandboxResolve{}
	if work != nil {
		record.metadata = append(json.RawMessage(nil), work.Metadata...)
	}
	if p.resolveErr != nil {
		p.resolves = append(p.resolves, record)
		return e2bruntime.Resolution{}, p.resolveErr
	}
	resolution, err := e2bruntime.NewProvider(config.E2BConfig{Template: "fake-template"}).Resolve(env, work)
	if err != nil {
		p.resolves = append(p.resolves, record)
		return e2bruntime.Resolution{}, err
	}
	if resolution.Metadata == nil {
		resolution.Metadata = map[string]string{}
	}
	resolution.Metadata["resolved_before_launch"] = "true"
	record.resolution = resolution
	p.resolves = append(p.resolves, record)
	return resolution, nil
}

func (p *recordingRunnerProvider) Create(_ context.Context, _ db.Environment, work *db.EnvironmentWork, resolution e2bruntime.Resolution) (e2bruntime.Sandbox, error) {
	if work != nil {
		p.creates = append(p.creates, recordedSandboxCreate{
			metadata:   append(json.RawMessage(nil), work.Metadata...),
			resolution: resolution,
		})
	}
	return e2bruntime.Sandbox{ID: p.sandboxID}, nil
}

func (p *recordingRunnerProvider) Kill(_ context.Context, sandboxID string) error {
	p.kills = append(p.kills, sandboxID)
	return p.killErr
}

func (p *recordingRunnerProvider) WriteFile(_ context.Context, sandboxID string, path string, data []byte) error {
	p.writes = append(p.writes, recordedSandboxWrite{sandboxID: sandboxID, path: path, data: append([]byte(nil), data...)})
	switch {
	case strings.HasSuffix(path, "/packages.v1.json"):
		p.operations = append(p.operations, "write:packages")
	case strings.HasSuffix(path, "/package-provisioner.v1.py"):
		p.operations = append(p.operations, "write:provisioner")
	default:
		p.operations = append(p.operations, "write:other")
	}
	return nil
}

func (p *recordingRunnerProvider) RunCommand(_ context.Context, sandboxID string, command string, _ time.Duration) error {
	if sandboxID != p.sandboxID {
		p.commands = append(p.commands, "wrong sandbox: "+sandboxID)
		return nil
	}
	p.commands = append(p.commands, command)
	if strings.Contains(command, "package-provisioner.v1.py") {
		p.operations = append(p.operations, "command:provision")
	} else {
		p.operations = append(p.operations, "command:other")
	}
	if p.commandErr != nil {
		return p.commandErr
	}
	if p.afterCommand != nil {
		p.afterCommand()
	}
	return nil
}

func (p *recordingRunnerProvider) PrepareSkillMount(ctx context.Context, runtimeSkills []skillsapi.RuntimeSkill) (*e2bruntime.SkillMount, error) {
	manifest, _, manifestSHA256, err := skillsapi.BuildMountManifest(runtimeSkills)
	if err != nil {
		return nil, err
	}
	mount := e2bruntime.SkillMount{
		MountPath:      e2bruntime.SandboxSkillsMountPath,
		VolumeName:     "test-managed-agent-skills-" + manifestSHA256[:12],
		ManifestSHA256: manifestSHA256,
		Skills:         manifest.Skills,
	}
	copied := make([]skillsapi.RuntimeSkill, 0, len(runtimeSkills))
	for _, skill := range runtimeSkills {
		archive, err := skill.LoadArchive(ctx)
		if err != nil {
			return nil, err
		}
		skill.Archive = archive
		copied = append(copied, skill)
	}
	p.skillMounts = append(p.skillMounts, recordedSkillMount{
		mount:         mount,
		runtimeSkills: copied,
	})
	return &mount, nil
}

func (p *recordingRunnerProvider) StartBackgroundCommand(_ context.Context, sandboxID string, command string, stdin []byte) error {
	if sandboxID != p.sandboxID {
		p.launches = append(p.launches, recordedSandboxLaunch{sandboxID: sandboxID, command: "wrong sandbox: " + command})
		return nil
	}
	p.launches = append(p.launches, recordedSandboxLaunch{
		sandboxID: sandboxID,
		command:   command,
		stdin:     append([]byte(nil), stdin...),
	})
	p.operations = append(p.operations, "launch:manager")
	return nil
}

func hasJSONKey(raw json.RawMessage, key string) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return false
	}
	_, ok := object[key]
	return ok
}

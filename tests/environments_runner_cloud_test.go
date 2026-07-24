package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	"github.com/superduck-ai/open-managed-agents/internal/storage"

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

	store := newFakeStore("runner-cloud-bucket")
	app := newTestAppWithStore(t, &cfg, store)
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

	file := uploadFile(t, app, "runner-data.csv", "text/csv", []byte("name,value\nalpha,1\n"))
	defer deleteFile(t, app, file.ID)
	resourceResponse := doSessionRequest(
		t,
		app,
		http.MethodPost,
		"/v1/sessions/"+session.ID+"/resources?beta=true",
		strings.NewReader(fmt.Sprintf(`{"type":"file","file_id":%q,"mount_path":"/workspace/data.csv"}`, file.ID)),
		defaultTestKey,
		true,
	)
	defer resourceResponse.Body.Close()
	if resourceResponse.StatusCode != http.StatusOK {
		t.Fatalf("add file resource status = %d: %s", resourceResponse.StatusCode, readAll(t, resourceResponse.Body))
	}
	var fileResource struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resourceResponse.Body).Decode(&fileResource); err != nil {
		t.Fatalf("decode file resource: %v", err)
	}
	defer func() {
		response := doSessionRequest(
			t,
			app,
			http.MethodDelete,
			"/v1/sessions/"+session.ID+"/resources/"+fileResource.ID+"?beta=true",
			nil,
			defaultTestKey,
			true,
		)
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf(
				"delete file resource status = %d: %s",
				response.StatusCode,
				readAll(t, response.Body),
			)
		}
	}()

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

	apiKey, err := app.db.GetAPIKey(ctx, auth.HashAPIKey(defaultTestKey))
	if err != nil {
		t.Fatalf("load api key: %v", err)
	}
	objectCountBeforeRunner := len(store.objects)
	provider := &recordingRunnerProvider{
		sandboxID: "sandbox-runner-bridge",
		beforeCreate: func() {
			if len(store.objects) != objectCountBeforeRunner {
				t.Fatalf(
					"runner startup copied Filestore objects: before=%d after=%d",
					objectCountBeforeRunner,
					len(store.objects),
				)
			}
			if _, lookupErr := app.db.GetCodeSessionBySessionExternalID(ctx, apiKey.WorkspaceID, session.ID); !errors.Is(lookupErr, db.ErrNotFound) {
				t.Fatalf("code session existed before sandbox creation: %v", lookupErr)
			}
		},
	}
	runner := newManagedAgentRunner(app, provider, cfg, nil)
	processed, err := runner.RunOnce(ctx, "runner-cloud-test")
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if !processed {
		t.Fatal("runner did not process queued session work")
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
		t.Fatalf("unexpected sandbox launches: %#v", provider.launches)
	}
	var payload map[string]any
	if err := json.Unmarshal(provider.launches[0].stdin, &payload); err != nil {
		t.Fatalf("decode environment-manager payload: %v", err)
	}
	startup := payload["startup_context"].(map[string]any)
	if startup["api_base_url"] != "http://code-session-sandbox.example.test" || startup["session_id"] != codeSession.ExternalID || startup["use_code_sessions"] != true {
		t.Fatalf("unexpected startup context: %#v", startup)
	}
	if sources, ok := startup["sources"].([]any); !ok || len(sources) != 0 {
		t.Fatalf("file resource must not be forwarded to environment-manager: %#v", startup["sources"])
	}
	auths := payload["auth"].([]any)
	sessionAuth := auths[0].(map[string]any)
	sessionIngressToken, _ := sessionAuth["token"].(string)
	if sessionAuth["type"] != "session_ingress" || !strings.HasPrefix(sessionIngressToken, "sk-ant-si-") {
		t.Fatalf("unexpected session auth: %#v", sessionAuth)
	}
	modelAuth := auths[1].(map[string]any)
	modelAccessToken, _ := modelAuth["token"].(string)
	if modelAuth["type"] != "anthropic_oauth" || !strings.HasPrefix(modelAccessToken, "sk-ant-oat01-") {
		t.Fatalf("unexpected model auth: %#v", modelAuth)
	}
	startupEnvironment := startup["environment_variables"].(map[string]any)
	if _, ok := startupEnvironment["CLAUDE_CODE_SESSION_ACCESS_TOKEN"]; ok {
		t.Fatalf("startup environment masks WebSocket auth FD: %#v", startupEnvironment)
	}
	if _, ok := payload["environment"].(map[string]any)["environment"]; ok {
		t.Fatalf("environment-manager payload should not contain Claude credential environment variables: %#v", payload["environment"])
	}
	if strings.Contains(string(provider.launches[0].stdin), cfg.AnthropicUpstream.APIKey) {
		t.Fatalf("environment-manager payload leaked upstream key: %s", provider.launches[0].stdin)
	}
	if !strings.Contains(provider.launches[0].command, "--session '"+codeSession.ExternalID+"'") ||
		strings.Contains(provider.launches[0].command, "nohup") ||
		strings.Contains(provider.launches[0].command, "environment-manager.v0.json") {
		t.Fatalf("unexpected sandbox background command: %#v", provider.launches[0])
	}
	if len(provider.rcloneLaunches) != 1 {
		t.Fatalf("rclone launches = %d, want 1", len(provider.rcloneLaunches))
	}
	if got, want := provider.operations, []string{"rclone-config-write", "rclone-config-chmod", "rclone-start", "rclone-ready", "rclone-config-cleanup", "environment-manager"}; !slices.Equal(got, want) {
		t.Fatalf("sandbox operation order = %#v, want %#v", got, want)
	}
	if len(provider.writes) != 1 || provider.writes[0].path != "/tmp/rclone-mount-config.json" {
		t.Fatalf("rclone config writes = %#v, want one config file", provider.writes)
	}
	if len(provider.rcloneLaunches[0].stdin) != 0 {
		t.Fatalf("rclone launch unexpectedly received stdin: %#v", provider.rcloneLaunches[0])
	}
	var rcloneConfig struct {
		Mounts []struct {
			AuthToken   string `json:"auth_token"`
			Source      string `json:"source"`
			Destination string `json:"destination"`
			Readonly    bool   `json:"readonly"`
		} `json:"mounts"`
	}
	if err := json.Unmarshal(provider.writes[0].data, &rcloneConfig); err != nil {
		t.Fatalf("decode rclone config: %v", err)
	}
	for _, mount := range rcloneConfig.Mounts {
		if mount.AuthToken == "" || strings.Contains(provider.rcloneLaunches[0].command, mount.AuthToken) {
			t.Fatal("rclone token is empty or leaked into command text")
		}
		claims, verifyErr := app.filestoreCredentials.Verify(mount.AuthToken)
		if verifyErr != nil {
			t.Fatalf("verify rclone token for %s: %v", mount.Source, verifyErr)
		}
		if mount.Source == "/outputs" {
			if mount.Readonly || len(claims.WritePrefixes) != 1 || claims.WritePrefixes[0] != "/outputs" {
				t.Fatalf("outputs token scope = readonly:%t prefixes:%#v", mount.Readonly, claims.WritePrefixes)
			}
		} else if !mount.Readonly || claims.Readonly == nil || !*claims.Readonly || len(claims.WritePrefixes) != 0 {
			t.Fatalf("readonly mount %s authority = mount readonly:%t claims:%#v", mount.Source, mount.Readonly, claims)
		}
	}
	if !slices.ContainsFunc(rcloneConfig.Mounts, func(mount struct {
		AuthToken   string `json:"auth_token"`
		Source      string `json:"source"`
		Destination string `json:"destination"`
		Readonly    bool   `json:"readonly"`
	}) bool {
		return mount.Source == "/uploads" && mount.Destination == "/mnt/session/uploads"
	}) {
		t.Fatalf("rclone config does not mount /uploads at the managed-agents root: %#v", rcloneConfig.Mounts)
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

func TestEnvironmentRunnerKillsSandboxWhenRcloneReadyFails(t *testing.T) {
	const providerSecretMarker = "provider-secret-marker"

	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.SandboxAPIBaseURL = "http://code-session-sandbox.example.test"
	cfg.E2B.Template = "fake-template"

	app := newTestAppWithStore(t, &cfg, newFakeStore("runner-rclone-failure-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-8","name":"Runner Rclone Failure Agent"}`)
	defer archiveAgent(t, app, agent.ID)
	environment := createEnvironment(t, app, `{"name":"runner-rclone-failure"}`)
	defer cleanupEnvironmentRows(t, app.db, environment.ID)

	client := anthropic.NewClient(option.WithBaseURL(app.baseURL), option.WithAPIKey(defaultTestKey))
	session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
		Agent:         anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agent.ID)},
		EnvironmentID: environment.ID,
		Title:         anthropic.String("Runner rclone failure session"),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer client.Beta.Sessions.Delete(context.Background(), session.ID, anthropic.BetaSessionDeleteParams{})

	ids := getDefaultDBIDs(t, app.db)
	work, err := app.db.GetLatestEnvironmentWorkByData(ctx, ids.WorkspaceID, environment.ID, "session", session.ID)
	if err != nil {
		t.Fatalf("load queued environment work: %v", err)
	}
	provider := &recordingRunnerProvider{
		sandboxID:         "sandbox-rclone-ready-failure",
		failOperation:     "rclone-ready",
		runCommandFailure: errors.New("simulated rclone ready failure: " + providerSecretMarker),
	}
	runner := newManagedAgentRunner(app, provider, cfg, nil)
	processed, err := runner.RunOnce(ctx, "runner-rclone-failure-test")
	if err == nil || err.Error() != "rclone-filestore readiness check failed" {
		t.Fatalf("RunOnce error = %v, want rclone ready failure", err)
	}
	if strings.Contains(err.Error(), providerSecretMarker) {
		t.Fatalf("RunOnce error leaked provider secret marker: %v", err)
	}
	if !processed {
		t.Fatal("runner did not process queued session work")
	}
	if got, want := provider.operations, []string{"rclone-config-write", "rclone-config-chmod", "rclone-start", "rclone-ready", "rclone-config-cleanup"}; !slices.Equal(got, want) {
		t.Fatalf("sandbox operation order = %#v, want %#v", got, want)
	}
	if len(provider.launches) != 0 {
		t.Fatalf("environment-manager launches = %#v, want none", provider.launches)
	}
	if got, want := provider.kills, []string{provider.sandboxID}; !slices.Equal(got, want) {
		t.Fatalf("killed sandboxes = %#v, want %#v", got, want)
	}

	stoppedWork, err := app.db.GetEnvironmentWork(ctx, ids.WorkspaceID, environment.ID, work.ExternalID)
	if err != nil {
		t.Fatalf("reload environment work: %v", err)
	}
	if stoppedWork.State != "stopped" || stoppedWork.StoppedAt == nil {
		t.Fatalf("environment work was not stopped: %#v", stoppedWork)
	}
	var sandboxState string
	var sandboxError *string
	if err := app.db.Pool.QueryRow(ctx, `
		select state, last_error
		from environment_sandboxes
		where work_id = $1
		order by id desc
		limit 1
	`, work.ID).Scan(&sandboxState, &sandboxError); err != nil {
		t.Fatalf("load failed environment sandbox: %v", err)
	}
	if sandboxState != "failed" || sandboxError == nil || *sandboxError != "rclone-filestore readiness check failed" {
		t.Fatalf("sandbox failure = state %q error %v", sandboxState, sandboxError)
	}
	if strings.Contains(*sandboxError, providerSecretMarker) {
		t.Fatalf("sandbox last_error leaked provider secret marker: %q", *sandboxError)
	}
	if _, lookupErr := app.db.GetCodeSessionBySessionExternalID(ctx, ids.WorkspaceID, session.ID); !errors.Is(lookupErr, db.ErrNotFound) {
		t.Fatalf("code session lookup after rclone failure = %v, want ErrNotFound", lookupErr)
	}
}

func TestEnvironmentRunnerRevokesCodeSessionWhenManagerStartFails(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.SandboxAPIBaseURL = "http://code-session-sandbox.example.test"
	cfg.EnvironmentRunner.ManagerPath = "/usr/local/bin/environment-manager"
	cfg.EnvironmentRunner.ClaudePath = "/opt/claude-code/bin/claude"
	cfg.E2B.Template = "fake-template"

	app := newTestAppWithStore(t, &cfg, newFakeStore("runner-manager-failure-bucket"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-8","name":"Runner Manager Failure Agent"}`)
	defer archiveAgent(t, app, agent.ID)
	environment := createEnvironment(t, app, `{"name":"runner-manager-failure"}`)
	defer cleanupEnvironmentRows(t, app.db, environment.ID)

	client := anthropic.NewClient(option.WithBaseURL(app.baseURL), option.WithAPIKey(defaultTestKey))
	session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
		Agent:         anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agent.ID)},
		EnvironmentID: environment.ID,
		Title:         anthropic.String("Runner manager failure session"),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer client.Beta.Sessions.Delete(context.Background(), session.ID, anthropic.BetaSessionDeleteParams{})

	ids := getDefaultDBIDs(t, app.db)
	provider := &recordingRunnerProvider{
		sandboxID:         "sandbox-manager-start-failure",
		failOperation:     "environment-manager",
		runCommandFailure: errors.New("simulated environment-manager launch failure"),
	}
	runner := newManagedAgentRunner(app, provider, cfg, nil)
	processed, err := runner.RunOnce(ctx, "runner-manager-failure-test")
	if err == nil || err.Error() != "environment manager process start failed" {
		t.Fatalf("RunOnce error = %v, want manager launch failure", err)
	}
	if !processed {
		t.Fatal("runner did not process queued session work")
	}
	if got, want := provider.kills, []string{provider.sandboxID}; !slices.Equal(got, want) {
		t.Fatalf("killed sandboxes = %#v, want %#v", got, want)
	}

	codeSession, err := app.db.GetCodeSessionBySessionExternalID(ctx, ids.WorkspaceID, session.ID)
	if err != nil {
		t.Fatalf("load compensated code session: %v", err)
	}
	if codeSession.Status != "terminated" || codeSession.ConnectionStatus != "disconnected" || codeSession.WorkerLeaseExpiresAt != nil {
		t.Fatalf("compensated code session = %#v", codeSession)
	}
	storedSession, err := app.db.GetSession(ctx, ids.WorkspaceID, session.ID)
	if err != nil {
		t.Fatalf("load Session after manager failure: %v", err)
	}
	if hasJSONKey(storedSession.Metadata, "claude_code_session_id") {
		t.Fatalf("failed runtime was published in Session metadata: %s", storedSession.Metadata)
	}
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
	runner := newManagedAgentRunner(app, provider, cfg, store)
	processed, err := runner.RunOnce(ctx, "runner-cloud-skills-test")
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if !processed {
		t.Fatal("runner did not process queued session work")
	}

	if len(provider.launches) != 1 {
		t.Fatalf("sandbox launches = %#v, want one environment-manager background process", provider.launches)
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
		t.Fatalf("sandbox command should not install managed agent skills directly: launches=%v", provider.launches)
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
	runner := newManagedAgentRunner(app, provider, cfg, nil)
	processed, err := runner.RunOnce(ctx, "runner-cloud-no-resolver-test")
	if err == nil || !strings.Contains(err.Error(), "custom skill resolver is unavailable") {
		t.Fatalf("RunOnce error = %v, want custom resolver error", err)
	}
	if !processed {
		t.Fatal("runner did not process queued session work")
	}
	if len(provider.creates) != 0 || len(provider.commands) != 0 || len(provider.launches) != 0 {
		t.Fatalf("provider should not be called after missing resolver: creates=%#v commands=%#v launches=%#v", provider.creates, provider.commands, provider.launches)
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
	runner := newManagedAgentRunner(app, provider, cfg, nil)
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
			runner := newManagedAgentRunner(app, provider, cfg, nil)
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
	runner := newManagedAgentRunner(app, provider, cfg, nil)
	processed, err := runner.RunOnce(ctx, "runner-cloud-resolve-failure-test")
	if err == nil || !strings.Contains(err.Error(), "network config invalid") {
		t.Fatalf("RunOnce error = %v, want resolve error", err)
	}
	if !processed {
		t.Fatal("runner did not process queued session work")
	}
	if len(provider.creates) != 0 || len(provider.commands) != 0 || len(provider.launches) != 0 {
		t.Fatalf("provider should not create sandbox after resolve failure: creates=%#v commands=%#v launches=%#v", provider.creates, provider.commands, provider.launches)
	}
	ids := getDefaultDBIDs(t, app.db)
	if _, err := app.db.GetCodeSessionBySessionExternalID(ctx, ids.WorkspaceID, session.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("code session lookup error = %v, want ErrNotFound", err)
	}
}

type recordingRunnerProvider struct {
	sandboxID         string
	resolveErr        error
	failOperation     string
	runCommandFailure error
	beforeCreate      func()
	resolves          []recordedSandboxResolve
	commands          []string
	launches          []recordedSandboxLaunch
	rcloneLaunches    []recordedSandboxLaunch
	writes            []recordedSandboxWrite
	operations        []string
	kills             []string
	creates           []recordedSandboxCreate
	skillMounts       []recordedSkillMount
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

func newManagedAgentRunner(app *testApp, provider e2bruntime.Provider, cfg config.Config, skillStore storage.ObjectStore) *environments.Runner {
	return environments.NewRunnerWithConfigStoreAndCredentials(
		app.db,
		provider,
		cfg,
		skillStore,
		app.credentials,
		app.filestoreCredentials,
	)
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
	if p.beforeCreate != nil {
		p.beforeCreate()
	}
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
	return nil
}

func (p *recordingRunnerProvider) WriteFile(_ context.Context, sandboxID, path string, data []byte) error {
	p.writes = append(p.writes, recordedSandboxWrite{
		sandboxID: sandboxID,
		path:      path,
		data:      append([]byte(nil), data...),
	})
	p.operations = append(p.operations, "rclone-config-write")
	if p.failOperation == "rclone-config-write" {
		return p.runCommandFailure
	}
	return nil
}

func (p *recordingRunnerProvider) FileExists(_ context.Context, sandboxID, path string) (bool, error) {
	if sandboxID != p.sandboxID {
		return false, fmt.Errorf("probe ready file in wrong sandbox: %s", sandboxID)
	}
	if path != "/tmp/rclone-mounts/ready" {
		return false, fmt.Errorf("probe unexpected ready file: %s", path)
	}
	p.operations = append(p.operations, "rclone-ready")
	if p.failOperation == "rclone-ready" {
		return false, p.runCommandFailure
	}
	return true, nil
}

func (p *recordingRunnerProvider) RunCommand(_ context.Context, sandboxID string, command string, _ time.Duration) error {
	if sandboxID != p.sandboxID {
		p.commands = append(p.commands, "wrong sandbox: "+sandboxID)
		return nil
	}
	p.commands = append(p.commands, command)
	operation := "command"
	switch {
	case strings.HasPrefix(command, "chmod 0600 "):
		operation = "rclone-config-chmod"
	case strings.HasPrefix(command, "rm -f ") && strings.Contains(command, "rclone-mount-config.json"):
		operation = "rclone-config-cleanup"
	}
	p.operations = append(p.operations, operation)
	if operation == p.failOperation {
		return p.runCommandFailure
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
	launch := recordedSandboxLaunch{
		sandboxID: sandboxID,
		command:   command,
		stdin:     append([]byte(nil), stdin...),
	}
	if strings.Contains(command, "/opt/rclone/rclone-filestore") {
		p.rcloneLaunches = append(p.rcloneLaunches, launch)
		p.operations = append(p.operations, "rclone-start")
		return nil
	}
	p.launches = append(p.launches, launch)
	p.operations = append(p.operations, "environment-manager")
	if p.failOperation == "environment-manager" {
		return p.runCommandFailure
	}
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

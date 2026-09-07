package tests

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

func TestEnvironmentRunnerMountsMemoryStoresAndWritesMarkdown(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.SandboxAPIBaseURL = "http://code-session-sandbox.example.test"
	cfg.E2B.Template = "fake-template"

	app := newTestAppWithStore(t, &cfg, newFakeStore("runner-memory-runtime-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-8","name":"Runner Memory Runtime Agent"}`)
	defer archiveAgent(t, app, agent.ID)
	environment := createEnvironment(t, app, `{"name":"runner-memory-runtime-`+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")+`"}`)
	defer cleanupEnvironmentRows(t, app.pool, environment.ID)

	rwStore := createMemoryStoreNamed(t, app, "user-preferences-"+uniqueMemoryStoreSuffix(t), "用户的饮食与语言偏好")
	defer deleteMemoryStore(t, app, rwStore.ID)
	roStore := createMemoryStoreNamed(t, app, "team-playbook-"+uniqueMemoryStoreSuffix(t), "团队规范，只读，不得修改")
	defer deleteMemoryStore(t, app, roStore.ID)

	instructions := strings.Repeat("字", sessionresource.MaxMemoryInstructionsRunes)
	session := createSession(t, app, sessionBodyWithResources(agent.ID, environment.ID, `[
		{"type":"memory_store","memory_store_id":`+quoteJSON(rwStore.ID)+`,"instructions":`+quoteJSON(instructions)+`},
		{"type":"memory_store","memory_store_id":`+quoteJSON(roStore.ID)+`,"access":"read_only"}
	]`))
	defer deleteSession(t, app, session.ID)
	resources := decodeMemoryResources(t, session.Resources)
	if len(resources) != 2 {
		t.Fatalf("session resources = %d, want 2", len(resources))
	}

	provider := &recordingRunnerProvider{sandboxID: "sandbox-runner-memory-runtime"}
	processed, err := newManagedAgentRunner(t, app, provider, cfg).RunOnce(ctx, "runner-memory-runtime-test")
	if err != nil || !processed {
		t.Fatalf("RunOnce() = (%t, %v), want success", processed, err)
	}

	if got, want := provider.operations, []string{
		"memory-root-mkdir",
		"rclone-config-write",
		"rclone-config-chmod",
		"rclone-start",
		"rclone-ready",
		"rclone-config-cleanup",
		"memory-markdown-write",
		"environment-manager",
	}; !slices.Equal(got, want) {
		t.Fatalf("sandbox operation order = %#v, want %#v", got, want)
	}

	rcloneWrite, markdownWrite := splitMemoryRuntimeWrites(t, provider.writes)
	var rcloneConfig struct {
		Mounts []struct {
			AuthToken   string  `json:"auth_token"`
			Source      string  `json:"source"`
			Destination string  `json:"destination"`
			Readonly    bool    `json:"readonly"`
			Cache       float64 `json:"cache_duration_s"`
		} `json:"mounts"`
	}
	if err := json.Unmarshal(rcloneWrite.data, &rcloneConfig); err != nil {
		t.Fatalf("decode rclone config: %v", err)
	}
	if len(rcloneConfig.Mounts) != 7 {
		t.Fatalf("rclone mounts = %d, want 7", len(rcloneConfig.Mounts))
	}
	var sawReadWrite, sawReadOnly bool
	for _, resource := range resources {
		readonly := resource.Access == sessionresource.MemoryAccessReadOnly
		assertMemoryRcloneMount(t, app, rcloneConfig.Mounts, resource, readonly)
		if readonly {
			sawReadOnly = true
		} else {
			sawReadWrite = true
		}
	}
	if !sawReadWrite || !sawReadOnly {
		t.Fatalf("session resources = %#v, want one rw and one ro store", resources)
	}
	for _, mount := range rcloneConfig.Mounts {
		if mount.Source == "/memory" || mount.Destination == sessionresource.MemoryMountRoot {
			t.Fatalf("parent /mnt/memory must not be a filestore mount: %#v", mount)
		}
	}

	markdown := string(markdownWrite.data)
	if !strings.Contains(markdown, "<!-- oma-stores -->") {
		t.Fatalf("MEMORY.md missing store marker:\n%s", markdown)
	}
	if !strings.Contains(markdown, instructions) {
		t.Fatal("MEMORY.md truncated 500-rune instructions")
	}
	if utf8.RuneCountInString(instructions) != sessionresource.MaxMemoryInstructionsRunes {
		t.Fatalf("instructions = %d runes", utf8.RuneCountInString(instructions))
	}
	for _, resource := range resources {
		access := "rw"
		if resource.Access == sessionresource.MemoryAccessReadOnly {
			access = "ro"
		}
		line := "- [" + resource.Name + "](" + resource.MountPath + ") " + access + " — " + resource.Description + "。" + resource.Instructions
		if !strings.Contains(markdown, line) {
			t.Fatalf("MEMORY.md missing store line %q in:\n%s", line, markdown)
		}
	}

	if len(provider.launches) != 1 {
		t.Fatalf("environment-manager launches = %#v, want 1", provider.launches)
	}
	var payload map[string]any
	if err := json.Unmarshal(provider.launches[0].stdin, &payload); err != nil {
		t.Fatalf("decode environment-manager payload: %v", err)
	}
	startup := payload["startup_context"].(map[string]any)
	if sources, ok := startup["sources"].([]any); !ok || len(sources) != 0 {
		t.Fatalf("memory stores leaked into sources: %#v", startup["sources"])
	}
	startupEnv := startup["environment_variables"].(map[string]any)
	if startupEnv["CLAUDE_CODE_REMOTE"] != "true" ||
		startupEnv["CLAUDE_CODE_REMOTE_MEMORY_DIR"] != sessionresource.MemoryMountRoot ||
		startupEnv["CLAUDE_COWORK_MEMORY_PATH_OVERRIDE"] != sessionresource.MemoryMountRoot {
		t.Fatalf("startup memory env = %#v", startupEnv)
	}

	ids := getDefaultDBIDs(t, app.pool)
	codeSession, err := app.db.GetCodeSessionBySessionExternalID(ctx, ids.WorkspaceUUID, session.ID)
	if err != nil {
		t.Fatalf("load code session: %v", err)
	}
	queued, err := app.db.ListQueuedCodeSessionInboundEvents(ctx, codeSession.ExternalID)
	if err != nil || len(queued) == 0 {
		t.Fatalf("queued inbound events = %#v err=%v", queued, err)
	}
	var initialize map[string]any
	if err := json.Unmarshal(queued[0].Payload, &initialize); err != nil {
		t.Fatalf("decode initialize: %v", err)
	}
	appendPrompt, _ := initialize["request"].(map[string]any)["appendSystemPrompt"].(string)
	if appendPrompt == "" || strings.Contains(appendPrompt, sessionresource.MemoryMountRoot) || strings.Contains(appendPrompt, instructions) {
		t.Fatalf("appendSystemPrompt leaked memory policy: %q", appendPrompt)
	}
	for _, resource := range resources {
		if strings.Contains(appendPrompt, resource.Name) {
			t.Fatalf("appendSystemPrompt leaked store name %q: %q", resource.Name, appendPrompt)
		}
	}

	for _, command := range provider.commands {
		if strings.Contains(command.request.Command, "cp ") || strings.Contains(command.request.Command, "rclone copy") {
			t.Fatalf("startup copied store files: %s", command.request.Command)
		}
	}
}

func TestEnvironmentRunnerFailsClosedWhenMemoryMarkdownWriteFails(t *testing.T) {
	const providerSecretMarker = "provider-secret-marker"
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.SandboxAPIBaseURL = "http://code-session-sandbox.example.test"
	cfg.E2B.Template = "fake-template"

	app := newTestAppWithStore(t, &cfg, newFakeStore("runner-memory-markdown-failure-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-8","name":"Runner Memory Markdown Failure Agent"}`)
	defer archiveAgent(t, app, agent.ID)
	environment := createEnvironment(t, app, `{"name":"runner-memory-markdown-failure"}`)
	defer cleanupEnvironmentRows(t, app.pool, environment.ID)
	store := createMemoryStoreNamed(t, app, "user-preferences-"+uniqueMemoryStoreSuffix(t), "用户的饮食与语言偏好")
	defer deleteMemoryStore(t, app, store.ID)
	session := createSession(t, app, sessionBodyWithMemoryResource(agent.ID, environment.ID, store.ID, ""))
	defer deleteSession(t, app, session.ID)

	ids := getDefaultDBIDs(t, app.pool)
	sessionRecord, found, err := app.db.GetSession(ctx, ids.WorkspaceUUID, session.ID)
	if err != nil || !found {
		t.Fatalf("load queued session: found=%v error=%v", found, err)
	}
	work, err := app.db.GetLatestEnvironmentWorkForSession(ctx, ids.WorkspaceUUID, environment.ID, sessionRecord.UUID)
	if err != nil {
		t.Fatalf("load queued environment work: %v", err)
	}

	provider := &recordingRunnerProvider{
		sandboxID:         "sandbox-memory-markdown-failure",
		failOperation:     "memory-markdown-write",
		runCommandFailure: errors.New("simulated memory markdown failure: " + providerSecretMarker),
	}
	processed, runErr := newManagedAgentRunner(t, app, provider, cfg).RunOnce(ctx, "runner-memory-markdown-failure-test")
	if runErr == nil || runErr.Error() != "memory markdown write failed" {
		t.Fatalf("RunOnce error = %v, want memory markdown write failed", runErr)
	}
	if strings.Contains(runErr.Error(), providerSecretMarker) {
		t.Fatalf("RunOnce error leaked provider secret marker: %v", runErr)
	}
	if !processed {
		t.Fatal("runner did not process queued session work")
	}
	if len(provider.launches) != 0 {
		t.Fatalf("environment-manager launches = %#v, want none", provider.launches)
	}
	if got, want := provider.kills, []string{provider.sandboxID}; !slices.Equal(got, want) {
		t.Fatalf("killed sandboxes = %#v, want %#v", got, want)
	}
	if _, err := app.db.GetCodeSessionBySessionExternalID(ctx, ids.WorkspaceUUID, session.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("code session lookup error = %v, want ErrNotFound", err)
	}
	assertFailedSandboxAndStoppedWork(t, app, ids.WorkspaceUUID, environment.ID, work.ExternalID, work.UUID, "memory markdown write failed")
}

func TestEnvironmentRunnerFailsClosedWhenMemoryMountReadyFails(t *testing.T) {
	const providerSecretMarker = "provider-secret-marker"
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.SandboxAPIBaseURL = "http://code-session-sandbox.example.test"
	cfg.E2B.Template = "fake-template"

	app := newTestAppWithStore(t, &cfg, newFakeStore("runner-memory-ready-failure-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-8","name":"Runner Memory Ready Failure Agent"}`)
	defer archiveAgent(t, app, agent.ID)
	environment := createEnvironment(t, app, `{"name":"runner-memory-ready-failure"}`)
	defer cleanupEnvironmentRows(t, app.pool, environment.ID)
	store := createMemoryStoreNamed(t, app, "user-preferences-"+uniqueMemoryStoreSuffix(t), "用户的饮食与语言偏好")
	defer deleteMemoryStore(t, app, store.ID)
	session := createSession(t, app, sessionBodyWithMemoryResource(agent.ID, environment.ID, store.ID, ""))
	defer deleteSession(t, app, session.ID)

	ids := getDefaultDBIDs(t, app.pool)
	sessionRecord, found, err := app.db.GetSession(ctx, ids.WorkspaceUUID, session.ID)
	if err != nil || !found {
		t.Fatalf("load queued session: found=%v error=%v", found, err)
	}
	work, err := app.db.GetLatestEnvironmentWorkForSession(ctx, ids.WorkspaceUUID, environment.ID, sessionRecord.UUID)
	if err != nil {
		t.Fatalf("load queued environment work: %v", err)
	}

	provider := &recordingRunnerProvider{
		sandboxID:         "sandbox-memory-ready-failure",
		failOperation:     "rclone-ready",
		runCommandFailure: errors.New("simulated rclone ready failure: " + providerSecretMarker),
	}
	processed, runErr := newManagedAgentRunner(t, app, provider, cfg).RunOnce(ctx, "runner-memory-ready-failure-test")
	if runErr == nil || runErr.Error() != "rclone-filestore readiness check failed" {
		t.Fatalf("RunOnce error = %v, want rclone ready failure", runErr)
	}
	if !processed {
		t.Fatal("runner did not process queued session work")
	}
	if slices.Contains(provider.operations, "memory-markdown-write") || slices.Contains(provider.operations, "environment-manager") {
		t.Fatalf("continued after mount failure: %#v", provider.operations)
	}
	if len(provider.launches) != 0 {
		t.Fatalf("environment-manager launches = %#v, want none", provider.launches)
	}
	if got, want := provider.kills, []string{provider.sandboxID}; !slices.Equal(got, want) {
		t.Fatalf("killed sandboxes = %#v, want %#v", got, want)
	}
	assertFailedSandboxAndStoppedWork(t, app, ids.WorkspaceUUID, environment.ID, work.ExternalID, work.UUID, "rclone-filestore readiness check failed")
}

func uniqueMemoryStoreSuffix(t *testing.T) string {
	t.Helper()
	return strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")
}

func createMemoryStoreNamed(t *testing.T, app *testApp, name, description string) memoryStoreAPIResponse {
	t.Helper()
	store := createMemoryStore(t, app, name)
	return updateMemoryStore(t, app, store.ID, `{"description":`+quoteJSON(description)+`}`)
}

func splitMemoryRuntimeWrites(t *testing.T, writes []recordedSandboxWrite) (recordedSandboxWrite, recordedSandboxWrite) {
	t.Helper()
	var rcloneWrite, markdownWrite recordedSandboxWrite
	for _, write := range writes {
		switch write.path {
		case "/tmp/rclone-mount-config.json":
			rcloneWrite = write
		case "/mnt/memory/MEMORY.md":
			markdownWrite = write
		default:
			t.Fatalf("unexpected sandbox write %q", write.path)
		}
	}
	if rcloneWrite.path == "" || markdownWrite.path == "" {
		t.Fatalf("writes = %#v, want rclone config and MEMORY.md", writes)
	}
	return rcloneWrite, markdownWrite
}

func assertMemoryRcloneMount(t *testing.T, app *testApp, mounts []struct {
	AuthToken   string  `json:"auth_token"`
	Source      string  `json:"source"`
	Destination string  `json:"destination"`
	Readonly    bool    `json:"readonly"`
	Cache       float64 `json:"cache_duration_s"`
}, resource memoryResourceAPIResponse, readonly bool) {
	t.Helper()
	slug := sessionresource.MemorySlugFromMountPath(resource.MountPath)
	source := "/memory/" + slug
	var found bool
	for _, mount := range mounts {
		if mount.Source != source {
			continue
		}
		found = true
		if mount.Destination != resource.MountPath || mount.Readonly != readonly || mount.Cache != 1 {
			t.Fatalf("memory mount %+v, want dest=%s readonly=%t cache=1", mount, resource.MountPath, readonly)
		}
		claims, err := app.filestoreCredentials.Verify(mount.AuthToken)
		if err != nil {
			t.Fatalf("verify memory token: %v", err)
		}
		if readonly {
			if claims.Readonly == nil || !*claims.Readonly {
				t.Fatalf("ro token claims = %#v", claims)
			}
		} else if claims.Readonly != nil {
			t.Fatalf("rw token claims = %#v", claims)
		}
	}
	if !found {
		t.Fatalf("missing memory mount %s in %#v", source, mounts)
	}
}

func assertFailedSandboxAndStoppedWork(
	t *testing.T,
	app *testApp,
	workspaceUUID, environmentID, workExternalID, workUUID, wantError string,
) {
	t.Helper()
	ctx := context.Background()
	stoppedWork, err := app.db.GetEnvironmentWork(ctx, workspaceUUID, environmentID, workExternalID)
	if err != nil {
		t.Fatalf("reload environment work: %v", err)
	}
	if stoppedWork.State != "stopped" || stoppedWork.StoppedAt == nil {
		t.Fatalf("environment work was not stopped: %#v", stoppedWork)
	}
	var sandboxState string
	var sandboxError *string
	if err := app.pool.QueryRow(ctx, `
		select state, last_error
		from environment_sandboxes
		where work_uuid = $1
		order by uuid desc
		limit 1
	`, workUUID).Scan(&sandboxState, &sandboxError); err != nil {
		t.Fatalf("load failed environment sandbox: %v", err)
	}
	if sandboxState != "failed" || sandboxError == nil || *sandboxError != wantError {
		t.Fatalf("sandbox state=%s last_error=%v, want failed %q", sandboxState, sandboxError, wantError)
	}
}

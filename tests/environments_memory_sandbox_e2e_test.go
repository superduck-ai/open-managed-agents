//go:build e2e

package tests

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"

	e2b "github.com/superduck-ai/e2b-go-sdk"
)

const (
	memorySandboxSeedPath    = "/notes/seed.md"
	memorySandboxSeedContent = "remember the soup"
	memorySandboxFromAPath   = "/notes/from-a.md"
	memorySandboxFromABody   = "from-session-a"
	memorySandboxRootName    = "user_m5.md"
	memorySandboxRootBody    = "session-local-only"
	memorySandboxIndexMark   = "session-a-index"
	memorySandboxBlockedPath = "/notes/blocked.md"
	memorySandboxRWInstr     = "有新偏好就更新"
)

func TestMemorySandboxCrossSessionLifetime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real E2B memory sandbox lifetime test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	requireFullE2BBridgeConfig(t, cfg)
	t.Logf("Testing memory sandbox lifetime on image %s", cfg.E2B.Template)
	if cfg.E2B.RequestTimeout < 2*time.Minute {
		cfg.E2B.RequestTimeout = 2 * time.Minute
	}
	if cfg.E2B.SandboxTimeout < 2*time.Minute {
		cfg.E2B.SandboxTimeout = 2 * time.Minute
	}

	app := newTestApp(t, &cfg)
	defer app.close()
	if quickstartLooksLikeLoopbackURL(cfg.E2B.APIURL) {
		app.cfg.CodeSession.SandboxAPIBaseURL = ""
	}
	quickstartEnsureSandboxIngress(t, app)
	cfg = app.cfg

	agent := createAgent(t, app, `{"model":"claude-opus-4-8","name":"Memory Sandbox Lifetime Agent"}`)
	defer archiveAgent(t, app, agent.ID)
	environment := createEnvironment(t, app, `{
		"name":"memory-sandbox-lifetime-`+uniqueMemoryStoreSuffix(t)+`",
		"config":{"type":"cloud","networking":{"type":"unrestricted"}}
	}`)
	defer cleanupEnvironmentRows(t, app.pool, environment.ID)

	rwStore := createMemoryStoreNamed(t, app, "m5-rw-"+uniqueMemoryStoreSuffix(t), "个人对西餐的喜好")
	defer deleteMemoryStore(t, app, rwStore.ID)
	roStore := createMemoryStoreNamed(t, app, "m5-ro-"+uniqueMemoryStoreSuffix(t), "团队规范，只读，不得修改")
	defer deleteMemoryStore(t, app, roStore.ID)
	seed := createMemory(t, app, rwStore.ID, memorySandboxSeedPath, memorySandboxSeedContent)

	resourcesJSON := `[
		{"type":"memory_store","memory_store_id":` + quoteJSON(rwStore.ID) + `,"instructions":` + quoteJSON(memorySandboxRWInstr) + `},
		{"type":"memory_store","memory_store_id":` + quoteJSON(roStore.ID) + `,"access":"read_only"}
	]`

	sessionA := startMemorySandboxSession(t, ctx, app, cfg, agent.ID, environment.ID, resourcesJSON)
	defer sessionA.stop(t, app, environment.ID)

	rwA := memoryResourceByAccess(t, sessionA.resources, sessionresource.MemoryAccessReadWrite)
	roA := memoryResourceByAccess(t, sessionA.resources, sessionresource.MemoryAccessReadOnly)
	if rwA.MemoryStoreID != rwStore.ID || roA.MemoryStoreID != roStore.ID {
		t.Fatalf("session A mounts = %+v", sessionA.resources)
	}

	t.Run("M5-01 seed is readable at the rw mount", func(t *testing.T) {
		got := waitE2BFileContent(t, ctx, sessionA.sandbox, rwA.MountPath+memorySandboxSeedPath)
		if got != memorySandboxSeedContent {
			t.Fatalf("seed content = %q, want %q", got, memorySandboxSeedContent)
		}
	})
	if t.Failed() {
		return
	}

	t.Run("M5-10 MEMORY.md is a sandbox file and appendSystemPrompt stays clean", func(t *testing.T) {
		markdown := waitE2BFileContent(t, ctx, sessionA.sandbox, sessionresource.MemoryMountRoot+"/MEMORY.md")
		if !strings.Contains(markdown, "<!-- oma-stores -->") {
			t.Fatalf("MEMORY.md missing store section:\n%s", markdown)
		}
		if !strings.Contains(markdown, rwA.MountPath) || !strings.Contains(markdown, roA.MountPath) {
			t.Fatalf("MEMORY.md missing mount paths:\n%s", markdown)
		}
		if !strings.Contains(markdown, memorySandboxRWInstr) {
			t.Fatalf("MEMORY.md missing instructions:\n%s", markdown)
		}
		if !strings.Contains(markdown, " rw ") || !strings.Contains(markdown, " ro ") {
			t.Fatalf("MEMORY.md missing access markers:\n%s", markdown)
		}
		prompt := initializeAppendSystemPrompt(t, app, sessionA.session.ID)
		if prompt == "" {
			t.Fatal("initialize appendSystemPrompt is empty")
		}
		if strings.Contains(prompt, sessionresource.MemoryMountRoot) ||
			strings.Contains(prompt, memorySandboxRWInstr) ||
			strings.Contains(prompt, rwA.Name) ||
			strings.Contains(prompt, roA.Name) {
			t.Fatalf("appendSystemPrompt leaked memory policy: %q", prompt)
		}
	})
	if t.Failed() {
		return
	}

	t.Run("M5-09 console Add memory is visible and platform MEMORY.md is not a store file", func(t *testing.T) {
		assertConsoleListsMemory(t, app, rwStore.ID, seed.ID, memorySandboxSeedPath)
		if _, found := findMemoryByPath(t, app, rwStore.ID, "/MEMORY.md"); found {
			t.Fatal("platform MEMORY.md leaked into the rw store")
		}
		if _, found := findMemoryByPath(t, app, roStore.ID, "/MEMORY.md"); found {
			t.Fatal("platform MEMORY.md leaked into the ro store")
		}
	})
	if t.Failed() {
		return
	}

	roVersionsBefore := len(listMemoryVersions(t, app, roStore.ID, "limit=100").Data)

	t.Run("M5-02 sandbox write into rw store becomes a session_actor version", func(t *testing.T) {
		writeSandboxFile(t, ctx, sessionA.sandbox, rwA.MountPath+memorySandboxFromAPath, memorySandboxFromABody+"\n")
		memory := waitForMemoryPath(t, app, rwStore.ID, memorySandboxFromAPath)
		full := retrieveMemoryFull(t, app, rwStore.ID, memory.ID)
		assertMemoryContent(t, full, memorySandboxFromABody+"\n")
		if !versionsHaveSessionActor(t, app, rwStore.ID, sessionA.session.ID) {
			t.Fatal("missing session_actor version for session A")
		}
		assertConsoleListsMemory(t, app, rwStore.ID, memory.ID, memorySandboxFromAPath)
	})
	if t.Failed() {
		return
	}

	t.Run("M5-06 read_only mount rejects writes and creates no version", func(t *testing.T) {
		command := `
set +e
if printf 'must-fail\n' > ` + shellPath(roA.MountPath+memorySandboxBlockedPath) + `; then
	printf 'unexpected-success\n'
	exit 1
fi
printf 'write-denied\n'
`
		stdout, stderr, err := runE2BCommand(ctx, sessionA.sandbox, command, 30*time.Second)
		if err != nil {
			t.Fatalf("ro write probe: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
		}
		if !strings.Contains(stdout, "write-denied") {
			t.Fatalf("ro write probe stdout=%s stderr=%s", stdout, stderr)
		}
		if _, found := findMemoryByPath(t, app, roStore.ID, memorySandboxBlockedPath); found {
			t.Fatal("read_only sandbox write created a memory")
		}
		if versionsHaveSessionActor(t, app, roStore.ID, sessionA.session.ID) {
			t.Fatal("read_only sandbox write created a session_actor version")
		}
		if got := len(listMemoryVersions(t, app, roStore.ID, "limit=100").Data); got != roVersionsBefore {
			t.Fatalf("ro versions = %d, want %d", got, roVersionsBefore)
		}
	})
	if t.Failed() {
		return
	}

	rootSandboxPath := sessionresource.MemoryMountRoot + "/" + memorySandboxRootName
	t.Run("M5-04 M5-05 root and MEMORY.md index writes stay session-local", func(t *testing.T) {
		writeSandboxFile(t, ctx, sessionA.sandbox, rootSandboxPath, memorySandboxRootBody+"\n")
		command := "printf '\\n- " + memorySandboxIndexMark + "\\n' >> " + shellPath(sessionresource.MemoryMountRoot+"/MEMORY.md")
		if stdout, stderr, err := runE2BCommand(ctx, sessionA.sandbox, command, 30*time.Second); err != nil {
			t.Fatalf("append MEMORY.md index: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
		}
		if got := waitE2BFileContent(t, ctx, sessionA.sandbox, rootSandboxPath); got != memorySandboxRootBody {
			t.Fatalf("root file content = %q", got)
		}
		markdown := waitE2BFileContent(t, ctx, sessionA.sandbox, sessionresource.MemoryMountRoot+"/MEMORY.md")
		if !strings.Contains(markdown, memorySandboxIndexMark) {
			t.Fatalf("MEMORY.md missing session index:\n%s", markdown)
		}
		if _, found := findMemoryByPath(t, app, rwStore.ID, "/"+memorySandboxRootName); found {
			t.Fatal("root file leaked into the rw store")
		}
		if _, found := findMemoryByPath(t, app, roStore.ID, "/"+memorySandboxRootName); found {
			t.Fatal("root file leaked into the ro store")
		}
		if _, found := findMemoryByPath(t, app, rwStore.ID, "/MEMORY.md"); found {
			t.Fatal("edited MEMORY.md leaked into the rw store")
		}
	})
	if t.Failed() {
		return
	}

	frozenMountPath := rwA.MountPath
	frozenName := rwA.Name
	t.Run("M5-08 renaming the store does not change a running session snapshot", func(t *testing.T) {
		updateMemoryStore(t, app, rwStore.ID, `{"name":`+quoteJSON("renamed-m5-"+uniqueMemoryStoreSuffix(t))+`}`)
		fresh := retrieveSession(t, app, sessionA.session.ID, defaultTestKey)
		current := memoryResourceByAccess(t, sessionMemoryResources(t, fresh), sessionresource.MemoryAccessReadWrite)
		if current.MountPath != frozenMountPath || current.Name != frozenName {
			t.Fatalf("running snapshot changed: %+v", current)
		}
		markdown := waitE2BFileContent(t, ctx, sessionA.sandbox, sessionresource.MemoryMountRoot+"/MEMORY.md")
		storeSection, _, _ := strings.Cut(markdown, memorySandboxIndexMark)
		if !strings.Contains(storeSection, frozenName) || !strings.Contains(storeSection, frozenMountPath) {
			t.Fatalf("running MEMORY.md store section changed:\n%s", markdown)
		}
		if strings.Contains(storeSection, "renamed-m5-") {
			t.Fatalf("running MEMORY.md picked up the live store name:\n%s", markdown)
		}
	})
	if t.Failed() {
		return
	}

	sessionA.stop(t, app, environment.ID)

	sessionB := startMemorySandboxSession(t, ctx, app, cfg, agent.ID, environment.ID, resourcesJSON)
	defer sessionB.stop(t, app, environment.ID)
	rwB := memoryResourceByAccess(t, sessionB.resources, sessionresource.MemoryAccessReadWrite)

	t.Run("M5-03 session B reads A's store write and rebuilds MEMORY.md", func(t *testing.T) {
		got := waitE2BFileContent(t, ctx, sessionB.sandbox, rwB.MountPath+memorySandboxFromAPath)
		if got != memorySandboxFromABody {
			t.Fatalf("session B store file = %q, want %q", got, memorySandboxFromABody)
		}
		seedGot := waitE2BFileContent(t, ctx, sessionB.sandbox, rwB.MountPath+memorySandboxSeedPath)
		if seedGot != memorySandboxSeedContent {
			t.Fatalf("session B seed = %q", seedGot)
		}
		markdown := waitE2BFileContent(t, ctx, sessionB.sandbox, sessionresource.MemoryMountRoot+"/MEMORY.md")
		if !strings.Contains(markdown, "<!-- oma-stores -->") {
			t.Fatalf("session B MEMORY.md missing store section:\n%s", markdown)
		}
		if !strings.Contains(markdown, rwB.MountPath) {
			t.Fatalf("session B MEMORY.md missing B mount path:\n%s", markdown)
		}
		if strings.Contains(markdown, memorySandboxIndexMark) {
			t.Fatalf("session B inherited A's MEMORY.md index:\n%s", markdown)
		}
		if rwB.Name != frozenName && !strings.Contains(markdown, rwB.Name) {
			t.Fatalf("session B MEMORY.md missing B snapshot name %q:\n%s", rwB.Name, markdown)
		}
	})
	if t.Failed() {
		return
	}

	t.Run("M5-04 session B does not inherit A's root file", func(t *testing.T) {
		command := "if [ -e " + shellPath(rootSandboxPath) + " ]; then printf 'inherited\n'; exit 1; fi; printf 'absent\n'"
		stdout, stderr, err := runE2BCommand(ctx, sessionB.sandbox, command, 30*time.Second)
		if err != nil {
			t.Fatalf("session B root probe: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
		}
		if !strings.Contains(stdout, "absent") {
			t.Fatalf("session B inherited the root file: stdout=%s stderr=%s", stdout, stderr)
		}
	})
	if t.Failed() {
		return
	}

	sessionB.stop(t, app, environment.ID)

	sessionC := startMemorySandboxSession(t, ctx, app, cfg, agent.ID, environment.ID, `[]`)
	defer sessionC.stop(t, app, environment.ID)

	t.Run("M5-07 session without stores has no memory root", func(t *testing.T) {
		if len(sessionC.resources) != 0 {
			t.Fatalf("session C resources = %+v, want none", sessionC.resources)
		}
		command := `
set -eu
if [ -e ` + shellPath(sessionresource.MemoryMountRoot) + ` ]; then
	printf 'memory-root-present\n'
	exit 1
fi
printf 'scratch\n' > /home/user/m5-scratch.md
printf 'no-memory-root\n'
`
		stdout, stderr, err := runE2BCommand(ctx, sessionC.sandbox, command, 30*time.Second)
		if err != nil {
			t.Fatalf("session C memory-root probe: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
		}
		if !strings.Contains(stdout, "no-memory-root") {
			t.Fatalf("session C probe stdout=%s stderr=%s", stdout, stderr)
		}
		prompt := initializeAppendSystemPrompt(t, app, sessionC.session.ID)
		if strings.Contains(prompt, sessionresource.MemoryMountRoot) {
			t.Fatalf("session C appendSystemPrompt leaked memory root: %q", prompt)
		}
	})
}

type memorySandboxSession struct {
	session   sessionAPIResponse
	workID    string
	sandbox   *e2b.Sandbox
	resources []memoryResourceAPIResponse
	stopped   bool
}

func startMemorySandboxSession(
	t *testing.T,
	ctx context.Context,
	app *testApp,
	cfg config.Config,
	agentID, environmentID, resourcesJSON string,
) *memorySandboxSession {
	t.Helper()
	session := createSession(t, app, sessionBodyWithResources(agentID, environmentID, resourcesJSON))
	live := &memorySandboxSession{
		session:   session,
		resources: sessionMemoryResources(t, session),
		workID:    quickstartFindSessionEnvironmentWorkID(t, app, environmentID, session.ID),
	}

	runner := newManagedAgentRunner(t, app, e2bruntime.NewProvider(cfg.E2B), cfg)
	processed, err := runner.RunOnce(ctx, "memory-sandbox-e2e")
	if err != nil {
		t.Fatalf("run environment runner once: %v", err)
	}
	if !processed {
		t.Fatal("environment runner did not process queued session work")
	}

	codeSessionID, metadata := quickstartWaitForCodeSessionMetadata(t, ctx, app, session.ID)
	if strings.TrimSpace(codeSessionID) == "" || metadata["runtime"] != "claude_code_local" {
		t.Fatalf("session metadata was not patched with local code session ids: %#v", metadata)
	}
	providerSandboxID, workState := quickstartWaitForProviderSandboxMetadata(t, ctx, app, environmentID, live.workID)
	if workState != "active" && workState != "running" {
		t.Fatalf("environment work state = %s, want active", workState)
	}
	if strings.TrimSpace(providerSandboxID) == "" {
		t.Fatal("provider sandbox id was not recorded")
	}

	sandbox, err := e2b.Connect(ctx, providerSandboxID, &e2b.SandboxConnectOpts{
		ConnectionOpts: e2bruntime.ConnectionOptsFromConfig(cfg.E2B),
	})
	if err != nil {
		t.Fatalf("connect to sandbox %s: %v", providerSandboxID, err)
	}
	live.sandbox = sandbox
	return live
}

func (s *memorySandboxSession) stop(t *testing.T, app *testApp, environmentID string) {
	t.Helper()
	if s == nil || s.stopped {
		return
	}
	s.stopped = true
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if s.workID != "" {
		quickstartStopEnvironmentWork(t, stopCtx, app, environmentID, s.workID)
	}
	if s.session.ID != "" {
		deleteSession(t, app, s.session.ID)
	}
}

func sessionMemoryResources(t *testing.T, session sessionAPIResponse) []memoryResourceAPIResponse {
	t.Helper()
	out := make([]memoryResourceAPIResponse, 0, len(session.Resources))
	for _, raw := range session.Resources {
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatalf("decode resource envelope: %v", err)
		}
		if envelope.Type != sessionresource.MemoryStoreType {
			continue
		}
		out = append(out, decodeMemoryResource(t, raw))
	}
	return out
}

func memoryResourceByAccess(t *testing.T, resources []memoryResourceAPIResponse, access string) memoryResourceAPIResponse {
	t.Helper()
	for _, resource := range resources {
		if resource.Access == access {
			return resource
		}
	}
	t.Fatalf("missing memory resource with access %s: %+v", access, resources)
	return memoryResourceAPIResponse{}
}

func writeSandboxFile(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox, path, content string) {
	t.Helper()
	command := "mkdir -p \"$(dirname " + shellPath(path) + ")\" && printf '%s' " + shellPath(content) + " > " + shellPath(path)
	stdout, stderr, err := runE2BCommand(ctx, sandbox, command, 30*time.Second)
	if err != nil {
		t.Fatalf("write %s: %v\nstdout=%s\nstderr=%s", path, err, stdout, stderr)
	}
}

func waitE2BFileContent(t *testing.T, ctx context.Context, sandbox *e2b.Sandbox, path string) string {
	t.Helper()
	command := "cat " + shellPath(path)
	deadline := time.Now().Add(30 * time.Second)
	var lastOut, lastErr string
	var last error
	for {
		lastOut, lastErr, last = runE2BCommand(ctx, sandbox, command, 30*time.Second)
		if last == nil {
			return strings.TrimSpace(lastOut)
		}
		if time.Now().After(deadline) {
			t.Fatalf("read %s: %v\nstdout=%s\nstderr=%s", path, last, lastOut, lastErr)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting to read %s: %v", path, ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func waitForMemoryPath(t *testing.T, app *testApp, storeID, path string) memoryAPIResponse {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		memory, found := findMemoryByPath(t, app, storeID, path)
		if found {
			return memory
		}
		if time.Now().After(deadline) {
			t.Fatalf("memory %s did not appear in store %s", path, storeID)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func assertConsoleListsMemory(t *testing.T, app *testApp, storeID, memoryID, path string) {
	t.Helper()
	page := listMemories(t, app, storeID, "path_prefix="+url.QueryEscape(parentMemoryPrefix(path))+"&limit=50")
	if !containsMemoryItem(t, page.Data, memoryID) {
		t.Fatalf("console list missing %s at %s: %+v", memoryID, path, page.Data)
	}
}

func parentMemoryPrefix(path string) string {
	trimmed := strings.TrimSuffix(path, "/")
	idx := strings.LastIndex(trimmed, "/")
	if idx <= 0 {
		return "/"
	}
	return trimmed[:idx+1]
}

func initializeAppendSystemPrompt(t *testing.T, app *testApp, sessionID string) string {
	t.Helper()
	ids := getDefaultDBIDs(t, app.pool)
	codeSession, err := app.db.GetCodeSessionBySessionExternalID(context.Background(), ids.WorkspaceUUID, sessionID)
	if err != nil {
		t.Fatalf("load code session for %s: %v", sessionID, err)
	}
	queued, err := app.db.ListQueuedCodeSessionInboundEvents(context.Background(), codeSession.ExternalID)
	if err != nil {
		t.Fatalf("list queued inbound events: %v", err)
	}
	for _, event := range queued {
		if prompt := appendSystemPromptFromPayload(event.Payload); prompt != "" {
			return prompt
		}
	}
	_, _, payload := latestCodeSessionInboundEventForSource(t, app, codeSession.ExternalID, "internal")
	return appendSystemPromptFromPayload(payload)
}

func appendSystemPromptFromPayload(payload json.RawMessage) string {
	var envelope struct {
		Request struct {
			AppendSystemPrompt string `json:"appendSystemPrompt"`
		} `json:"request"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return ""
	}
	return envelope.Request.AppendSystemPrompt
}

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
)

func TestSessionEventsRetryIdleSandboxResumeAfterProviderFailure(t *testing.T) {
	ctx := context.Background()
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-idle-sandbox-resume-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-idle-sandbox-resume-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-idle-sandbox-resume-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	defer deleteSession(t, app, session.ID)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	sandbox, err := app.db.GetResumableEnvironmentSandboxForCodeSession(ctx, codeSessionID)
	if err != nil || sandbox.ProviderSandboxID == nil {
		t.Fatalf("load resumable sandbox = (%+v, %v), want provider sandbox", sandbox, err)
	}
	app.sandboxTimeouts.setError(errors.New("provider temporarily unavailable"))
	sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"first resume attempt"}]}]}`, defaultTestKey)

	app.sandboxTimeouts.setError(nil)
	sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"second resume attempt"}]}]}`, defaultTestKey)

	calls := app.sandboxTimeouts.snapshotCalls()
	if len(calls) != 2 {
		t.Fatalf("sandbox resume calls = %d, want failed attempt followed by retry", len(calls))
	}
	for _, call := range calls {
		if call.sandboxID != *sandbox.ProviderSandboxID || call.timeout != app.cfg.E2B.SandboxTimeout {
			t.Fatalf("sandbox resume call = %+v, want id %q and timeout %s", call, *sandbox.ProviderSandboxID, app.cfg.E2B.SandboxTimeout)
		}
	}
}

func TestSessionEventsReplaceProviderSandboxAfterItWasDeleted(t *testing.T) {
	ctx := context.Background()
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-deleted-sandbox-recovery-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-deleted-sandbox-recovery-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-deleted-sandbox-recovery-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	defer deleteSession(t, app, session.ID)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	codeSession, err := getCodeSession(app, ctx, codeSessionID)
	if err != nil {
		t.Fatalf("load original code session: %v", err)
	}
	oldIngressToken := codeSessionIngressToken(t, app, codeSessionID)
	if _, err := app.pool.Exec(ctx, `
		update code_sessions
		set current_worker_epoch = 1
		where external_id = $1
	`, codeSessionID); err != nil {
		t.Fatalf("seed original worker epoch: %v", err)
	}

	oldSandbox, err := app.db.GetResumableEnvironmentSandboxForCodeSession(ctx, codeSessionID)
	if err != nil || oldSandbox.ProviderSandboxID == nil {
		t.Fatalf("load original sandbox = (%+v, %v), want provider sandbox", oldSandbox, err)
	}
	oldProviderSandboxID := *oldSandbox.ProviderSandboxID
	app.sandboxTimeouts.setError(sandboxruntime.ErrSandboxNotFound)
	sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"message after sandbox deletion"}]}]}`, defaultTestKey)

	assertQueuedSandboxRecovery(t, app, session.ID, codeSession.UUID, oldProviderSandboxID)

	newProviderSandboxID := oldProviderSandboxID + "-replacement"
	provider := &recordingRunnerProvider{sandboxID: newProviderSandboxID}
	recoveryConfig := app.cfg
	if recoveryConfig.CodeSession.SandboxAPIBaseURL == "" {
		recoveryConfig.CodeSession.SandboxAPIBaseURL = "http://sandbox-api.example.test"
	}
	runner := newManagedAgentRunner(t, app, provider, recoveryConfig)
	runUntilSandboxCreated(t, ctx, runner, provider)

	newSandbox, err := app.db.GetResumableEnvironmentSandboxForCodeSession(ctx, codeSessionID)
	if err != nil || newSandbox.ProviderSandboxID == nil || *newSandbox.ProviderSandboxID != newProviderSandboxID {
		t.Fatalf("replacement sandbox = (%+v, %v), want provider id %q", newSandbox, err, newProviderSandboxID)
	}
	assertSingleActiveRecoveredCodeSession(t, app, session.ID)
	if len(provider.launches) != 1 {
		t.Fatalf("replacement manager launches = %d, want 1", len(provider.launches))
	}
	newIngressToken, workerEpoch := replacementManagerCredentials(t, provider.launches[0].stdin)
	if workerEpoch != "2" {
		t.Fatalf("replacement worker epoch = %q, want 2", workerEpoch)
	}
	assertWorkerRegistration(t, app, codeSessionID, oldIngressToken, http.StatusUnauthorized, "")
	assertWorkerRegistration(t, app, codeSessionID, newIngressToken, http.StatusOK, "2")
	assertWorkerRegistration(t, app, codeSessionID, newIngressToken, http.StatusOK, "2")
	assertRecoveredWorkPublished(t, app, session.ID)
	assertRecoveryMessageQueued(t, app, codeSessionID)
}

func TestFailedReplacementPreservesDurableCodeSession(t *testing.T) {
	ctx := context.Background()
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-failed-sandbox-recovery-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-failed-sandbox-recovery-agent"}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-failed-sandbox-recovery-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	defer deleteSession(t, app, session.ID)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	codeSession, err := getCodeSession(app, ctx, codeSessionID)
	if err != nil {
		t.Fatalf("load original code session: %v", err)
	}
	oldSandbox, err := app.db.GetResumableEnvironmentSandboxForCodeSession(ctx, codeSessionID)
	if err != nil || oldSandbox.ProviderSandboxID == nil {
		t.Fatalf("load original sandbox = (%+v, %v), want provider sandbox", oldSandbox, err)
	}
	app.sandboxTimeouts.setError(sandboxruntime.ErrSandboxNotFound)
	sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"recover after deletion"}]}]}`, defaultTestKey)

	replacementID := *oldSandbox.ProviderSandboxID + "-failed-replacement"
	provider := &recordingRunnerProvider{
		sandboxID:         replacementID,
		failOperation:     "environment-manager",
		runCommandFailure: errors.New("manager launch failed"),
	}
	cfg := app.cfg
	if cfg.CodeSession.SandboxAPIBaseURL == "" {
		cfg.CodeSession.SandboxAPIBaseURL = "http://sandbox-api.example.test"
	}
	processed, runErr := newManagedAgentRunner(t, app, provider, cfg).RunOnce(ctx, "failed-recovery-test")
	if !processed || runErr == nil {
		t.Fatalf("failed recovery RunOnce = (processed=%t, err=%v), want processed error", processed, runErr)
	}

	recovered, err := getCodeSession(app, ctx, codeSessionID)
	if err != nil {
		t.Fatalf("reload code session after failed recovery: %v", err)
	}
	if recovered.Status != "active" || recovered.UUID != codeSession.UUID {
		t.Fatalf("code session after failed recovery = (uuid=%q, status=%q), want original active %q", recovered.UUID, recovered.Status, codeSession.UUID)
	}
	assertQueuedSandboxRecovery(t, app, session.ID, codeSession.UUID, replacementID)
	if len(provider.kills) != 1 || provider.kills[0] != replacementID {
		t.Fatalf("failed replacement kills = %#v, want [%q]", provider.kills, replacementID)
	}
}

func assertQueuedSandboxRecovery(t *testing.T, app *testApp, sessionID, codeSessionUUID, providerSandboxID string) {
	t.Helper()
	var workState string
	if err := app.pool.QueryRow(context.Background(), `
		select state
		from environment_work
		where data->>'id' = $1 and deleted_at is null
		order by created_at desc
		limit 1
	`, sessionID).Scan(&workState); err != nil {
		t.Fatalf("load queued recovery work: %v", err)
	}
	if workState != "queued" {
		t.Fatalf("recovery work state = %q, want queued", workState)
	}
	var activeCodeSessionUUID string
	if err := app.pool.QueryRow(context.Background(), `
		select uuid
		from code_sessions
		where session_external_id = $1 and status = 'active' and deleted_at is null
	`, sessionID).Scan(&activeCodeSessionUUID); err != nil {
		t.Fatalf("load inferred recovery code session: %v", err)
	}
	if activeCodeSessionUUID != codeSessionUUID {
		t.Fatalf("inferred recovery code session = %q, want %q", activeCodeSessionUUID, codeSessionUUID)
	}
	var sandboxState string
	if err := app.pool.QueryRow(context.Background(), `
		select state
		from environment_sandboxes
		where provider_sandbox_id = $1
	`, providerSandboxID).Scan(&sandboxState); err != nil {
		t.Fatalf("load failed sandbox: %v", err)
	}
	if sandboxState != "failed" {
		t.Fatalf("sandbox %q state = %q, want failed", providerSandboxID, sandboxState)
	}
}

func runUntilSandboxCreated(t *testing.T, ctx context.Context, runner interface {
	RunOnce(context.Context, string) (bool, error)
}, provider *recordingRunnerProvider) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(provider.creates) == 0 {
		processed, err := runner.RunOnce(ctx, "sessions-deleted-sandbox-recovery-test")
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("run replacement sandbox: %v", err)
		}
		if !processed && time.Now().After(deadline) {
			t.Fatal("replacement sandbox was not created before deadline")
		}
		if len(provider.creates) == 0 {
			time.Sleep(25 * time.Millisecond)
		}
	}
}

func assertSingleActiveRecoveredCodeSession(t *testing.T, app *testApp, sessionID string) {
	t.Helper()
	var count int
	if err := app.pool.QueryRow(context.Background(), `
		select count(*)
		from code_sessions
		where session_external_id = $1 and status = 'active' and deleted_at is null
	`, sessionID).Scan(&count); err != nil {
		t.Fatalf("count code sessions after recovery: %v", err)
	}
	if count != 1 {
		t.Fatalf("active code session count after recovery = %d, want 1", count)
	}
}

func replacementManagerCredentials(t *testing.T, payload []byte) (string, string) {
	t.Helper()
	var managerPayload struct {
		StartupContext struct {
			EnvironmentVariables map[string]any `json:"environment_variables"`
		} `json:"startup_context"`
		Auth []struct {
			Type  string `json:"type"`
			Token string `json:"token"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(payload, &managerPayload); err != nil {
		t.Fatalf("decode replacement manager payload: %v", err)
	}
	var ingressToken string
	for _, auth := range managerPayload.Auth {
		if auth.Type == "session_ingress" {
			ingressToken = auth.Token
			break
		}
	}
	if ingressToken == "" {
		t.Fatal("replacement manager payload has no session_ingress token")
	}
	workerEpoch, _ := managerPayload.StartupContext.EnvironmentVariables["CLAUDE_CODE_WORKER_EPOCH"].(string)
	return ingressToken, workerEpoch
}

func assertWorkerRegistration(t *testing.T, app *testApp, codeSessionID, token string, wantStatus int, wantEpoch string) {
	t.Helper()
	req, err := http.NewRequest(
		http.MethodPost,
		app.baseURL+"/v1/code/sessions/"+codeSessionID+"/worker/register",
		strings.NewReader(`{"session_id":`+quoteJSON(codeSessionID)+`}`),
	)
	if err != nil {
		t.Fatalf("create worker register request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("register recovered worker: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read worker register response: %v", err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("register worker status = %d, want %d: %s", resp.StatusCode, wantStatus, body)
	}
	if wantEpoch == "" {
		return
	}
	var result struct {
		WorkerEpoch string `json:"worker_epoch"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode worker register response: %v", err)
	}
	if result.WorkerEpoch != wantEpoch {
		t.Fatalf("registered worker epoch = %q, want %q", result.WorkerEpoch, wantEpoch)
	}
}

func assertRecoveredWorkPublished(t *testing.T, app *testApp, sessionID string) {
	t.Helper()
	var state string
	if err := app.pool.QueryRow(context.Background(), `
		select state
		from environment_work
		where data->>'id' = $1 and deleted_at is null
		order by created_at desc
		limit 1
	`, sessionID).Scan(&state); err != nil {
		t.Fatalf("load recovered work: %v", err)
	}
	if state != "active" {
		t.Fatalf("recovered work state = %q, want active", state)
	}
}

func assertRecoveryMessageQueued(t *testing.T, app *testApp, codeSessionID string) {
	t.Helper()
	inbound, err := app.db.ListQueuedCodeSessionInboundEvents(context.Background(), codeSessionID)
	if err != nil {
		t.Fatalf("list recovery inbound events: %v", err)
	}
	for _, event := range inbound {
		if bytes.Contains(event.Payload, []byte("message after sandbox deletion")) {
			return
		}
	}
	t.Fatalf("replacement code session queue = %#v, want post-deletion message", inbound)
}

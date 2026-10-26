//go:build integration || e2e || e2b_integration

package liveworker

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestChatBrainProxyTakeover(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	cfg, err := config.Load()
	requireOK(t, err)
	provider, stop := startLazyBrainRunner(t, e, cfg)
	defer stop()
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version": e.agent.CurrentVersion,
		"model":   map[string]string{"id": "claude-sonnet-4-6"},
		"tools":   []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": true, "permission_policy": map[string]string{"type": "always_allow"}}}},
	}, http.StatusOK)
	f := createPublicChat(t, e)
	first := startBrainNode(t, e, cfg, "proxy-first")
	defer first.stop(t)
	firstResult := configureBrainProxyProbe(t, e)
	submitChat(t, f, "Run Bash to verify HTTPS through the inherited proxy, then reply done.")
	before := waitBrainProxyProbe(t, f)
	f.code = before
	sandbox, err := e.database.GetResumableEnvironmentSandboxForCodeSession(t.Context(), before.ExternalID)
	requireOK(t, err)
	if sandbox.ProviderSandboxID == nil {
		t.Fatal("proxy probe has no provider sandbox")
	}
	firstProxy := <-firstResult
	t.Logf("before owner_pid=%d epoch=%d sandbox_uuid=%s provider_id=%s %s", first.command.Process.Pid, before.CurrentWorkerEpoch, sandbox.UUID, *sandbox.ProviderSandboxID, firstProxy)
	chatProof(t, started, "brain_proxy_before_takeover")
	oldToken, err := e.credentials.Issue(brainCredentialIdentity(t, f, before.CurrentWorkerEpoch))
	requireOK(t, err)
	e.request(t, "GET", "/v2/sessions/"+before.ExternalID, oldToken, nil, http.StatusOK)
	first.stop(t)
	secondResult := configureBrainProxyProbe(t, e)
	submitChat(t, f, "After owner replacement, run the HTTPS Bash probe again and reply done.")
	waitRealWorker(t, "old Brain lease expired", func() bool {
		state, _, err := e.database.GetCodeSession(t.Context(), before.ExternalID)
		requireOK(t, err)
		return state.WorkerLeaseExpiresAt == nil || !state.WorkerLeaseExpiresAt.After(time.Now())
	}, 75*time.Second)
	replacement := startBrainNode(t, e, cfg, "proxy-replacement")
	defer replacement.stop(t)
	after := waitBrainProxyProbe(t, f)
	if after.CurrentWorkerEpoch <= before.CurrentWorkerEpoch {
		t.Fatal("Brain owner replacement did not advance worker epoch")
	}
	e.request(t, "GET", "/v2/sessions/"+before.ExternalID, oldToken, nil, http.StatusOK)
	t.Logf("old token identity accepted after owner replacement: previous_epoch=%d current_epoch=%d http_status=200", before.CurrentWorkerEpoch, after.CurrentWorkerEpoch)
	chatProof(t, started, "brain_proxy_old_token_identity")
	e.request(t, "POST", "/v2/ccr-sessions/"+before.ExternalID+"/sandbox/mcp", oldToken,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, http.StatusConflict)
	chatProof(t, started, "brain_proxy_old_worker_rejected")
	reused, err := e.database.GetResumableEnvironmentSandboxForCodeSession(t.Context(), before.ExternalID)
	requireOK(t, err)
	secondProxy := <-secondResult
	if reused.UUID != sandbox.UUID || reused.ProviderSandboxID == nil || *reused.ProviderSandboxID != *sandbox.ProviderSandboxID || provider.created.Load() != 1 || secondProxy != firstProxy {
		t.Fatalf("owner replacement did not reuse sandbox and relay: allocations=%d before=%s after=%s", provider.created.Load(), firstProxy, secondProxy)
	}
	t.Logf("after owner_pid=%d epoch=%d sandbox_uuid=%s provider_id=%s %s allocations=%d", replacement.command.Process.Pid, after.CurrentWorkerEpoch, reused.UUID, *reused.ProviderSandboxID, secondProxy, provider.created.Load())
	registerPublicSandboxCleanup(t, e, f)
	chatProof(t, started, "brain_proxy_same_sandbox_https")
}

func configureBrainProxyProbe(t *testing.T, e *liveEnv) <-chan string {
	t.Helper()
	result := make(chan string, 1)
	command := `printf 'proxy=%s\n' "$https_proxy"; curl --fail --silent --show-error --connect-timeout 10 --max-time 25 -o /dev/null -w 'http_code=%{http_code} connect_code=%{http_connect} remote_ip=%{remote_ip} ssl_verify=%{ssl_verify_result}\n' https://1.1.1.1/cdn-cgi/trace`
	arguments, err := json.Marshal(map[string]any{"command": command, "timeout": 35})
	requireOK(t, err)
	model := brainToolCallModel(t, "Bash", string(arguments), func(content json.RawMessage) {
		text := string(content)
		for _, expected := range []string{"http_code=200 connect_code=200 remote_ip=127.0.0.1 ssl_verify=0", "proxy=http://127.0.0.1:"} {
			if !strings.Contains(text, expected) {
				t.Errorf("HTTPS Bash result missing %q: %s", expected, content)
			}
		}
		if strings.Contains(text, "Invalid code session token") {
			t.Error("sandbox relay credential was rejected after takeover")
		}
		proxy := regexp.MustCompile(`proxy=http://127\.0\.0\.1:[0-9]+`).FindString(text)
		status := regexp.MustCompile(`http_code=[0-9]+ connect_code=[0-9]+ remote_ip=[0-9.]+ ssl_verify=[0-9]+`).FindString(text)
		t.Logf("Bash HTTPS %s %s", proxy, status)
		result <- proxy
	})
	configureChatModel(t, e, model.URL)
	return result
}

func waitBrainProxyProbe(t *testing.T, f *liveSession) db.CodeSession {
	t.Helper()
	waitRealWorker(t, "Brain HTTPS tool roundtrip completed", func() bool {
		session, _, err := f.env.database.GetSession(t.Context(), f.session.WorkspaceUUID, f.session.ExternalID)
		requireOK(t, err)
		return toolModelCalls.Load() == 2 && session.Status == "idle"
	}, 100*time.Second)
	code, err := f.env.database.GetCodeSessionBySessionExternalID(t.Context(), f.session.WorkspaceUUID, f.session.ExternalID)
	requireOK(t, err)
	return code
}

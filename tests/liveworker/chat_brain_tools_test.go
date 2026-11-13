//go:build integration || e2e || e2b_integration

package liveworker

import (
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/devinit"
)

func TestChatBrainRealTools(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	providers, err := devinit.LoadModelProviders(os.Getenv("VERIFY_BE_MODEL_PROVIDERS"))
	requireOK(t, err)
	model := os.Getenv("VERIFY_BE_MODEL")
	e := newLiveEnv(t)
	for _, provider := range providers.Providers {
		if !slices.Contains(provider.ModelIDs, model) {
			continue
		}
		catalog := isolatedWorkerProviders(t, e)
		if len(catalog) != 1 || catalog[0].Name != isolatedProviderName {
			t.Fatal("real tools require an isolated model provider")
		}
		e.request(t, "PUT", isolatedProviderPath(e)+"/"+catalog[0].ID, "", map[string]any{
			"name": isolatedProviderName, "slug": "default", "api_backend": provider.APIBackend, "base_url": provider.BaseURL, "api_key": provider.APIKey,
			"models": []any{map[string]any{"id": model, "context_window": 128000, "max_output_tokens": 4096, "input_modalities": []string{"text"}}},
		}, 200, e.cookies...)
	}
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version": e.agent.CurrentVersion, "model": map[string]string{"id": model},
		"system": "Follow the requested tool sequence exactly. Execute tools instead of describing hypothetical results. Do not delegate or use a substitute tool.",
		"tools":  []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": true, "permission_policy": map[string]string{"type": "always_allow"}}}},
	}, 200)
	cfg, err := config.Load()
	requireOK(t, err)
	provider, stop := startLazyBrainRunner(t, e, cfg)
	defer stop()
	node := startBrainNode(t, e, cfg, "real-tools")
	defer node.stop(t)
	f := createPublicChat(t, e)
	submitChat(t, f, "Execute these six tools in this order, using each named tool at least once. 1. Write /tmp/oma-claim-real.txt with exactly alpha-marker followed by a newline. 2. Edit that file, replacing alpha-marker with beta-marker. 3. Read the file. 4. Grep for beta-marker in /tmp/oma-claim-real.txt. 5. Glob for oma-claim-real.txt under /tmp. 6. Bash: cat /tmp/oma-claim-real.txt. After all six tools succeed, reply exactly CLAIMS_OK. Do not replace any named tool with another tool.")
	waitRealWorker(t, "real model completed all six sandbox tools", func() bool {
		history := chatHistory(t, f)
		for _, event := range history {
			if event.Type == "session.error" {
				t.Fatal("real model Session reported an error")
			}
			if event.Type == "session.status_idle" && event.StopReason.Type == "end_turn" {
				return true
			}
		}
		return false
	}, 5*time.Minute)
	names := map[string]bool{}
	answers := 0
	lastAnswer := ""
	for _, event := range chatHistory(t, f) {
		if event.Type == "agent.message" {
			answers++
			lastAnswer = ""
			for _, block := range event.Content {
				if block.Type == "text" {
					lastAnswer += block.Text
				}
			}
		}
		if event.Type == "agent.tool_use" {
			names[strings.ToLower(event.Name)] = true
		}
		if event.Type == "agent.tool_result" && event.IsError {
			t.Fatal("real model received a failed tool result")
		}
	}
	for _, name := range []string{"write", "edit", "read", "grep", "glob", "bash"} {
		if !names[name] {
			t.Fatalf("real model did not execute %s", name)
		}
	}
	if strings.TrimSpace(lastAnswer) != "CLAIMS_OK" {
		t.Fatal("real model did not confirm completion")
	}
	chatProof(t, started, "real_model_six_tools")
	f.code, err = e.database.GetCodeSessionBySessionExternalID(t.Context(), e.key.WorkspaceUUID.String(), f.session.ExternalID)
	requireOK(t, err)
	sandbox := waitMemorySandbox(t, e, f)
	registerPublicSandboxCleanup(t, e, f)
	output, err := exec.CommandContext(t.Context(), "docker", "exec", sandbox, "cat", "/tmp/oma-claim-real.txt").Output()
	requireOK(t, err)
	if string(output) != "beta-marker\n" || provider.created.Load() != 1 {
		t.Fatal("real sandbox bytes or allocation count did not match")
	}
	waitChatIdle(t, f, answers)
	chatProof(t, started, "real_sandbox_bytes")
	var calls []struct {
		State string `json:"state"`
		Tool  string `json:"tool_name"`
	}
	rows, err := e.database.SQLDB().QueryContext(t.Context(), `SELECT json_agg(json_build_object('state',state,'tool_name',tool_name)) FROM sandbox_tool_calls WHERE code_session_uuid=$1`, f.code.UUID)
	requireOK(t, err)
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("missing durable tool calls")
	}
	var data []byte
	requireOK(t, rows.Scan(&data))
	requireOK(t, json.Unmarshal(data, &calls))
	if len(calls) < 6 {
		t.Fatal("six tools did not create durable calls")
	}
	for _, call := range calls {
		if call.State != "succeeded" {
			t.Fatalf("tool %s persisted state %s", call.Tool, call.State)
		}
	}
	chatProof(t, started, "real_claims_persisted")
	verifyLiveClaimProtocol(t, e, f, started, sandbox)
}

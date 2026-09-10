package liveworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

// The actual API, DB, Redis, JetStream and Claude Code exchange all ingress and
// delivery traffic. Only model output is deterministic; no paid model is used.
func TestRealWorkerToolPermissions(t *testing.T) {
	if os.Getenv("LIVE_WORKER_REAL_CLAUDE") != "1" {
		t.Skip("opt in with LIVE_WORKER_REAL_CLAUDE=1")
	}
	e := newLiveEnv(t)
	for _, scenario := range []struct {
		name, policy, decision             string
		enabled, manual, writes, interrupt bool
	}{
		{"public_interrupt_with_queued_input", "always_ask", "", true, true, false, true},
		{"manual_deny", "always_ask", "deny", true, true, false, false},
		{"automatic_deny", "always_allow", "", false, false, false, false},
		{"manual_allow_with_queued_input", "always_ask", "allow", true, true, true, false},
		{"automatic_allow", "always_allow", "", true, false, true, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			snapshot, err := json.Marshal(map[string]any{
				"model": map[string]string{"id": "claude-sonnet-4-6"},
				"tools": []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{
					"enabled": scenario.enabled, "permission_policy": map[string]string{"type": scenario.policy},
				}}},
			})
			requireOK(t, err)
			f := e.newSessionWithSnapshot(t, snapshot)
			var modelCalls atomic.Int32
			model := realWorkerModelFixture(t, &modelCalls)
			container := startRealPermissionWorker(t, f, model)
			waitRealWorker(t, "initialize ACK", func() bool {
				consumer, err := e.stream.Consumer(t.Context(), "oma_worker_"+f.code.ExternalID)
				if errors.Is(err, jetstream.ErrConsumerNotFound) {
					return false
				}
				requireOK(t, err)
				info, err := consumer.Info(t.Context())
				requireOK(t, err)
				return info.NumAckPending == 0 && info.NumPending == 0
			})
			e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey, map[string]any{
				"events": []any{map[string]any{"type": "user.message", "content": []any{map[string]string{"type": "text", "text": "Run the requested test tool, then reply done."}}}},
			}, 200)
			if scenario.manual {
				publicToolID := ""
				waitRealWorker(t, "permission request", func() bool {
					code, found, err := e.database.GetCodeSession(t.Context(), f.code.ExternalID)
					requireOK(t, err)
					if !found || code.WorkerStatus != "requires_action" {
						return false
					}
					var metadata map[string]json.RawMessage
					requireOK(t, json.Unmarshal(code.WorkerExternalMetadata, &metadata))
					for key, raw := range metadata {
						if !strings.HasPrefix(key, "managed_agent_tool_permission_request:") {
							continue
						}
						var request struct {
							PublicID string `json:"public_event_id"`
							ToolID   string `json:"provider_tool_use_id"`
						}
						requireOK(t, json.Unmarshal(raw, &request))
						if request.ToolID == "toolu_control_e2e" {
							publicToolID = request.PublicID
						}
					}
					return publicToolID != ""
				})
				e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey, map[string]any{
					"events": []any{map[string]any{"type": "user.message", "content": []any{map[string]string{"type": "text", "text": "Reply queued done after the previous task."}}}},
				}, 200)
				waitRealWorker(t, "blocked task lane", func() bool {
					info := f.consumer(t)
					if info.Config.MaxAckPending != 1 {
						t.Fatal("delivery window changed")
					}
					return info.NumAckPending == 1 && info.NumPending == 1
				})
				input := map[string]string{"type": "user.tool_confirmation", "tool_use_id": publicToolID, "result": scenario.decision}
				if scenario.interrupt {
					input = map[string]string{"type": "user.interrupt"}
				}
				e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey, map[string]any{"events": []any{input}}, 200)
			}
			waitRealWorker(t, "all input and response ACKs", func() bool {
				if modelCalls.Load() < 2 {
					return false
				}
				for _, suffix := range []string{"", "_reply"} {
					consumer, err := e.stream.Consumer(t.Context(), "oma_worker_"+f.code.ExternalID+suffix)
					requireOK(t, err)
					info, err := consumer.Info(t.Context())
					requireOK(t, err)
					if info.Config.MaxAckPending != 1 {
						t.Fatal("delivery window changed")
					}
					if info.NumAckPending != 0 || info.NumPending != 0 {
						return false
					}
				}
				return true
			})
			proof, err := exec.Command("docker", "exec", container, "sh", "-c", "if [ -f /tmp/oma-control-e2e.txt ]; then cat /tmp/oma-control-e2e.txt; else printf '<absent>'; fi").Output()
			requireOK(t, err)
			if scenario.writes {
				if err != nil || string(proof) != "verified control delivery" {
					t.Fatalf("tool write proof=%q error=%v", proof, err)
				}
			} else if string(proof) != "<absent>" {
				t.Fatal("denied tool wrote the file")
			}
			t.Logf("real API and Worker: manual=%v allowed=%v, both windows stayed at 1 and drained", scenario.manual, scenario.writes)
		})
	}
}

func waitRealWorker(t *testing.T, label string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", label)
}

func startRealPermissionWorker(t *testing.T, f *liveSession, modelURL string) string {
	t.Helper()
	name := "oma-control-e2e-" + f.code.ExternalID
	log, err := os.Create(filepath.Join(t.TempDir(), "worker.log"))
	requireOK(t, err)
	t.Cleanup(func() { _ = log.Close() })
	image := os.Getenv("OMA_WORKER_CONTROL_IMAGE")
	if image == "" {
		image = "ghcr.io/superduck-ai/managed-agent-sandbox:latest"
	}
	args := []string{"run", "--rm", "--pull=never", "--name", name, "--entrypoint", "/opt/claude-code/bin/claude"}
	for key, value := range map[string]string{
		"ANTHROPIC_BASE_URL": modelURL, "ANTHROPIC_API_KEY": "isolated-fake-model-key",
		"CLAUDE_CODE_SESSION_ACCESS_TOKEN": f.token, "CLAUDE_CODE_WORKER_EPOCH": "1",
		"CLAUDE_CODE_USE_CCR_V2": "1", "CLAUDE_CODE_POST_FOR_SESSION_INGRESS_V2": "1",
		"CLAUDE_CODE_REMOTE": "true", "CLAUDE_CODE_REMOTE_SESSION_ID": f.code.ExternalID,
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1", "CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL": "1",
		"CLAUDE_CODE_ENABLE_BACKGROUND_PLUGIN_REFRESH": "0",
	} {
		args = append(args, "-e", key+"="+value)
	}
	url := strings.Replace(f.env.url, "127.0.0.1", "host.docker.internal", 1)
	args = append(args, image, "--output-format=stream-json", "--verbose", "--input-format=stream-json", "--include-partial-messages", "--replay-user-messages", "--model", "claude-sonnet-4-6", "--sdk-url", url+f.path(""))
	cmd := exec.Command("docker", args...)
	cmd.Stdout, cmd.Stderr = log, log
	requireOK(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, "docker", "stop", "--time", "1", name).Run()
		select {
		case <-done:
		case <-ctx.Done():
			_ = cmd.Process.Kill()
		}
		if t.Failed() {
			data, _ := os.ReadFile(log.Name())
			// Diagnostic output is from this test's fake model. Never print the
			// short-lived ingress token even if a CLI diagnostic includes it.
			t.Log(strings.ReplaceAll(string(data[max(0, len(data)-6000):]), f.token, "[redacted]"))
		}
	})
	return name
}

func realWorkerModelFixture(t *testing.T, calls *atomic.Int32) string {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			w.WriteHeader(200)
			return
		}
		call := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(event string, data map[string]any) {
			data["type"] = event
			body, _ := json.Marshal(data)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body)
		}
		write("message_start", map[string]any{"message": map[string]any{"id": fmt.Sprintf("msg_control_%d", call), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 10, "output_tokens": 1}}})
		reason := "end_turn"
		if call == 1 {
			reason = "tool_use"
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": "toolu_control_e2e", "name": "Write", "input": map[string]any{}}})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"file_path":"/tmp/oma-control-e2e.txt","content":"verified control delivery"}`}})
		} else {
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": "done"}})
		}
		write("content_block_stop", map[string]any{"index": 0})
		write("message_delta", map[string]any{"delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 12}})
		write("message_stop", map[string]any{})
	}))
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	requireOK(t, err)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return fmt.Sprintf("http://host.docker.internal:%d", listener.Addr().(*net.TCPAddr).Port)
}

// A fresh isolated API needs model metadata to create the test agent. Existing
// providers are never modified; only the row created here is cleaned up.
func ensureRealWorkerProvider(t *testing.T, e *liveEnv, cfg config.Config) {
	t.Helper()
	org, workspace := e.key.OrganizationUUID.String(), e.key.WorkspaceUUID.String()
	providers, err := e.database.ListLLMProviders(t.Context(), org, workspace)
	requireOK(t, err)
	if len(providers) != 0 {
		return
	}
	mk := cfg.Vault.MasterKey
	kek, err := secrets.ResolveKEK(mk.Kek, mk.KekFile)
	requireOK(t, err)
	defer clear(kek)
	service, err := secrets.NewLocalServiceWithKeys(t.Context(), secrets.LocalKeyMaterial{Version: mk.EffectiveVersion(), KEK: kek}, nil)
	requireOK(t, err)
	now := time.Now().UTC()
	provider := db.LLMProvider{UUID: uuid.NewString(), ExternalID: "llmprov_probe_" + uuid.NewString(), OrganizationUUID: org, WorkspaceUUID: workspace, Name: "isolated worker model metadata", BaseURL: "http://127.0.0.1:9", APIKeyLast4: "fake", ModelIDs: []string{"claude-opus-4-6", "claude-sonnet-4-6"}, CreatedAt: now, UpdatedAt: now}
	envelope, err := service.Seal(t.Context(), llmproviders.SecretBinding(provider), []byte("isolated-fake"))
	requireOK(t, err)
	provider.SecretEnvelope = &envelope
	_, err = e.database.CreateLLMProvider(t.Context(), provider)
	requireOK(t, err)
	t.Cleanup(func() {
		requireOK(t, e.database.DeleteLLMProvider(context.Background(), org, workspace, provider.ExternalID))
	})
}

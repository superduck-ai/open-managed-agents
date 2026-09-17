package liveworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

// Public inputs, permissions and ACKs use the real API; raw controls use its
// production ingress service. Only model output is a fixture, and the proxy
// can close SSE to exercise reconnect against the same PG/Redis/JetStream.
func TestRealWorkerControlDelivery(t *testing.T) {
	if os.Getenv("LIVE_WORKER_REAL_CLAUDE") != "1" {
		t.Skip("opt in with LIVE_WORKER_REAL_CLAUDE=1")
	}
	e := newLiveEnv(t)
	for _, scenario := range []struct {
		name, policy, decision, control string
		disabled, textOnly, reconnect   bool
	}{
		{name: "manual_deny", policy: "always_ask", decision: "deny"},
		{name: "automatic_deny", policy: "always_allow", disabled: true},
		{name: "interrupt_waiting_task", policy: "always_ask", control: "interrupt"},
		{name: "unknown_response_then_permission", policy: "always_ask", decision: "allow", control: "unknown_response"},
		{name: "manual_allow_with_queued_input_and_reconnect", policy: "always_ask", decision: "allow", reconnect: true},
		{name: "automatic_allow", policy: "always_allow"},
		{name: "text_only", policy: "always_allow", textOnly: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			snapshot, err := json.Marshal(map[string]any{
				"model": map[string]string{"id": "claude-sonnet-4-6"},
				"tools": []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{
					"enabled": !scenario.disabled, "permission_policy": map[string]string{"type": scenario.policy},
				}}},
			})
			requireOK(t, err)
			f := e.newSessionWithSnapshot(t, snapshot)
			var calls, queuedCall atomic.Int32
			resume := make(chan struct{})
			release := sync.OnceFunc(func() { close(resume) })
			t.Cleanup(release)
			if !scenario.reconnect {
				release()
			}
			model := realWorkerModelFixture(t, &calls, &queuedCall, scenario.textOnly, resume)
			worker := startRealControlWorker(t, f, model)
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
			sendRealWorkerInput(t, f, "Run the requested test tool, then reply done.")
			var queuedSequence uint64
			if scenario.policy == "always_ask" {
				toolID := waitRealWorkerPermission(t, f)
				sendRealWorkerInput(t, f, "Reply queued done after the previous task.")
				waitRealWorker(t, "blocked task lane", func() bool {
					info := f.consumer(t)
					return info.NumAckPending == 1 && info.NumPending == 1
				})
				queued, err := e.stream.GetLastMsgForSubject(t.Context(), f.consumer(t).Config.FilterSubject)
				requireOK(t, err)
				queuedSequence = queued.Sequence
				if scenario.reconnect {
					worker.reconnect(t)
				}
				if scenario.control == "unknown_response" {
					queueRealWorkerControl(t, f, map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": "no_such_request", "response": map[string]any{}}})
					waitRealWorker(t, "unknown response ACK", func() bool {
						info := realWorkerReplyConsumer(t, f)
						return info.Delivered.Stream > 0 && info.NumAckPending == 0 && info.NumPending == 0
					})
				}
				if scenario.control == "interrupt" {
					// Public user.interrupt conversion is a separate bugfix; this tests
					// delivery of the canonical Worker protocol message.
					queueRealWorkerControl(t, f, map[string]any{"type": "control_request", "request_id": "interrupt_probe", "request": map[string]string{"subtype": "interrupt"}})
				} else {
					e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey, map[string]any{
						"events": []any{map[string]string{"type": "user.tool_confirmation", "tool_use_id": toolID, "result": scenario.decision}},
					}, 200)
				}
			}
			if scenario.reconnect {
				waitRealWorker(t, "approved task continuing", func() bool { return calls.Load() >= 2 && realWorkerReplyConsumer(t, f).NumAckPending == 0 })
				replySequence := realWorkerReplyConsumer(t, f).Delivered.Stream
				if replySequence <= queuedSequence || f.consumer(t).NumPending != 1 {
					t.Fatal("approval must overtake the still-queued input")
				}
				worker.reconnect(t)
				release()
				t.Logf("reconnected after reply sequence %d; queued sequence %d must still execute", replySequence, queuedSequence)
			}
			minimumCalls := int32(2)
			if scenario.textOnly {
				minimumCalls = 1
			}
			if queuedSequence != 0 && scenario.control != "interrupt" {
				minimumCalls = 3
			}
			waitRealWorker(t, "all input and response ACKs", func() bool {
				if calls.Load() < minimumCalls || (queuedSequence != 0 && queuedCall.Load() == 0) {
					return false
				}
				for _, info := range []*jetstream.ConsumerInfo{f.consumer(t), realWorkerReplyConsumer(t, f)} {
					if info.Config.MaxAckPending != 1 {
						t.Fatal("delivery window changed")
					}
					if info.NumAckPending != 0 || info.NumPending != 0 {
						return false
					}
				}
				return true
			})
			if scenario.reconnect && queuedCall.Load() < 3 {
				t.Fatal("queued input did not start a separate model turn")
			}
			proof, err := exec.Command("docker", "exec", worker.container, "sh", "-c", "if [ -f /tmp/oma-control-e2e.txt ]; then cat /tmp/oma-control-e2e.txt; else printf '<absent>'; fi").Output()
			requireOK(t, err)
			writes := !scenario.disabled && !scenario.textOnly && scenario.control != "interrupt" && scenario.decision != "deny"
			want := "<absent>"
			if writes {
				want = "verified control delivery"
			}
			if string(proof) != want {
				t.Fatalf("tool write proof=%q want=%q", proof, want)
			}
			t.Log("real API and Worker completed; both windows stayed at 1 and drained")
		})
	}
}

func sendRealWorkerInput(t *testing.T, f *liveSession, text string) {
	t.Helper()
	f.env.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", f.env.apiKey, map[string]any{
		"events": []any{map[string]any{"type": "user.message", "content": []any{map[string]string{"type": "text", "text": text}}}},
	}, 200)
}

func waitRealWorkerPermission(t *testing.T, f *liveSession) string {
	t.Helper()
	publicToolID := ""
	waitRealWorker(t, "permission request", func() bool {
		code, found, err := f.env.database.GetCodeSession(t.Context(), f.code.ExternalID)
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
	return publicToolID
}

func queueRealWorkerControl(t *testing.T, f *liveSession, fields map[string]any) {
	t.Helper()
	fields["uuid"] = uuid.NewString()
	payload, err := json.Marshal(fields)
	requireOK(t, err)
	requireOK(t, f.env.service.QueueRawPublicSessionEvents(t.Context(), f.code, []json.RawMessage{payload}))
}

func realWorkerReplyConsumer(t *testing.T, f *liveSession) *jetstream.ConsumerInfo {
	t.Helper()
	consumer, err := f.env.stream.Consumer(t.Context(), "oma_worker_"+f.code.ExternalID+"_reply")
	requireOK(t, err)
	info, err := consumer.Info(t.Context())
	requireOK(t, err)
	return info
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

type realControlWorker struct {
	container   string
	mu          sync.Mutex
	connections int
	disconnect  context.CancelFunc
}

func (w *realControlWorker) reconnect(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	disconnect, previous := w.disconnect, w.connections
	w.mu.Unlock()
	if disconnect == nil {
		t.Fatal("Worker has no SSE connection")
	}
	disconnect()
	waitRealWorker(t, "Worker SSE reconnect", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.connections > previous
	})
}

func startRealControlWorker(t *testing.T, f *liveSession, modelURL string) *realControlWorker {
	t.Helper()
	name := "oma-control-e2e-" + f.code.ExternalID
	worker := &realControlWorker{container: name}
	target, err := url.Parse(f.env.url)
	requireOK(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxyURL := serveRealWorkerFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/worker/events/stream") {
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			worker.mu.Lock()
			worker.connections++
			worker.disconnect = cancel
			worker.mu.Unlock()
			r = r.WithContext(ctx)
		}
		proxy.ServeHTTP(w, r)
	}))
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
	args = append(args, image, "--output-format=stream-json", "--verbose", "--input-format=stream-json", "--include-partial-messages", "--replay-user-messages", "--model", "claude-sonnet-4-6", "--sdk-url", proxyURL+f.path(""))
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
	return worker
}

func realWorkerModelFixture(t *testing.T, calls, queuedCall *atomic.Int32, textOnly bool, resume <-chan struct{}) string {
	t.Helper()
	return serveRealWorkerFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			w.WriteHeader(200)
			return
		}
		call := calls.Add(1)
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if strings.Contains(string(body), "Reply queued done after the previous task.") {
			queuedCall.CompareAndSwap(0, call)
		}
		if call == 2 {
			select {
			case <-resume:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(event string, data map[string]any) {
			data["type"] = event
			body, _ := json.Marshal(data)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body)
		}
		write("message_start", map[string]any{"message": map[string]any{"id": fmt.Sprintf("msg_control_%d", call), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 10, "output_tokens": 1}}})
		reason := "end_turn"
		if call == 1 && !textOnly {
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
}

func serveRealWorkerFixture(t *testing.T, handler http.Handler) string {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
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

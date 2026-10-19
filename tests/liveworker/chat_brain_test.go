package liveworker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestChatBrain(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	if os.Getenv("OMA_BRAIN_BINARY") == "" {
		t.Fatal("OMA_BRAIN_BINARY required")
	}
	e := newLiveEnv(t)
	cfg, err := config.Load()
	requireOK(t, err)
	_, stop := startLazyBrainRunner(t, e, cfg)
	defer stop()
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version": e.agent.CurrentVersion,
		"model":   map[string]string{"id": "claude-sonnet-4-6"},
		"tools":   []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": true, "permission_policy": map[string]string{"type": "always_allow"}}}},
	}, 200)
	f := createPublicChat(t, e)
	previewSeen := make(chan struct{}, 4)
	var calls atomic.Int32
	model := brainHistoryModel(t, previewSeen, &calls)
	configureChatModel(t, e, model.URL)
	first := startBrainNode(t, e, cfg, "first")
	second := startBrainNode(t, e, cfg, "second")
	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout(t, 180*time.Second))
	defer cancel()
	events, closeStream := connectChatStream(t, ctx, f)
	defer closeStream()
	input := submitChat(t, f, "请回复固定的聊天验证文本。")
	waitRealWorker(t, "brain claimed session", func() bool {
		state, err := e.database.GetCodeSessionBySessionExternalID(t.Context(), f.session.WorkspaceUUID, f.session.ExternalID)
		if err != nil {
			return false
		}
		f.code = state
		return state.WorkerRegisteredAt != nil
	})
	scope := f.code
	verifyBrainTaskFencing(t, f)
	chatProof(t, started, "brain_fencing")

	var received []chatEvent
	final := receiveChatAnswer(t, ctx, events, func() { previewSeen <- struct{}{} }, func(event chatEvent) { received = append(received, event) })
	verifyChatHistory(t, f, input, final)
	waitChatIdle(t, f, 1)
	verifyBrainMessageOrder(t, ctx, f, events, received)
	if calls.Load() != 1 {
		t.Fatalf("two nodes duplicated model calls: %d", calls.Load())
	}
	chatProof(t, started, "brain_roundtrip")
	toolModel := brainToolModel(t)
	configureChatModel(t, e, toolModel.URL)
	submitChat(t, f, "Use Write to write /tmp/oma-brain-proof.txt, then answer done.")
	waitRealWorker(t, "native MCP loop completed", func() bool {
		state, _, err := e.database.GetSession(t.Context(), f.code.WorkspaceUUID, f.session.ExternalID)
		requireOK(t, err)
		return toolModelCalls.Load() == 2 && state.Status == "idle"
	})
	sandbox, err := e.database.GetResumableEnvironmentSandboxForCodeSession(t.Context(), f.code.ExternalID)
	requireOK(t, err)
	output, err := exec.CommandContext(t.Context(), "docker", "exec", *sandbox.ProviderSandboxID, "cat", "/tmp/oma-brain-proof.txt").Output()
	requireOK(t, err)
	if string(output) != "native grok loop" {
		t.Fatalf("unexpected sandbox bytes %q", output)
	}
	for {
		select {
		case event := <-events:
			if event.Type == "agent.message" && len(event.Content) > 0 && event.Content[0].Text == "done" {
				goto toolStreamDrained
			}
		case <-ctx.Done():
			t.Fatal("tool final did not reach public SSE")
		}
	}
toolStreamDrained:
	chatProof(t, started, "brain_mcp_loop")
	before, _, err := e.database.GetCodeSession(t.Context(), scope.ExternalID)
	requireOK(t, err)
	snapshots, _, err := e.service.EventPayloadStore().ListCodeSessionInternalEventsPage(t.Context(), db.ListCodeSessionInternalEventsPageParams{
		WorkspaceUUID: scope.WorkspaceUUID, CodeSessionExternalID: scope.ExternalID, Limit: 500,
	})
	requireOK(t, err)
	if !slices.ContainsFunc(snapshots, func(event db.CodeSessionInternalEvent) bool {
		return bytes.Contains(event.Payload, []byte(`"dsh_log"`))
	}) {
		t.Fatal("native history was not committed to CCR private events")
	}
	previousToken, err := e.credentials.Issue(brainCredentialIdentity(t, f, before.CurrentWorkerEpoch))
	requireOK(t, err)
	first.stop(t)
	second.stop(t)
	configureChatModel(t, e, model.URL)
	submitChat(t, f, "恢复后再次回复固定文本。")
	waitRealWorker(t, "expired brain lease becomes claimable", func() bool {
		state, _, err := e.database.GetCodeSession(t.Context(), scope.ExternalID)
		requireOK(t, err)
		return state.WorkerLeaseExpiresAt == nil || !state.WorkerLeaseExpiresAt.After(time.Now())
	}, 75*time.Second)
	startBrainNode(t, e, cfg, "replacement")
	waitRealWorker(t, "replacement owns a new epoch", func() bool {
		state, _, err := e.database.GetCodeSession(t.Context(), scope.ExternalID)
		requireOK(t, err)
		return state.CurrentWorkerEpoch > before.CurrentWorkerEpoch
	})
	endpointPath := "/v2/ccr-sessions/" + scope.ExternalID + "/sandbox/mcp"
	e.request(t, "POST", endpointPath, previousToken, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, http.StatusUnauthorized)
	receiveChatAnswer(t, ctx, events, func() { previewSeen <- struct{}{} })
	waitChatIdle(t, f, 3)
	if calls.Load() != 2 {
		t.Fatalf("restored loop model calls=%d", calls.Load())
	}
	chatProof(t, started, "brain_takeover")
	current, _, err := e.database.GetCodeSession(t.Context(), scope.ExternalID)
	requireOK(t, err)
	currentToken, err := e.credentials.Issue(brainCredentialIdentity(t, f, current.CurrentWorkerEpoch))
	requireOK(t, err)
	requireOK(t, e.service.TerminateManagedAgentCodeSession(t.Context(), f.session, f.code.ExternalID))
	for _, method := range []string{"initialize", "tools/call"} {
		e.request(t, "POST", endpointPath, currentToken, map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}, http.StatusUnauthorized)
	}
	chatProof(t, started, "brain_retirement")
	verifyLazyBrain(t, started)
}

func verifyBrainMessageOrder(t *testing.T, ctx context.Context, f *liveSession, events <-chan chatEvent, received []chatEvent) {
	t.Helper()
	for len(received) == 0 || received[len(received)-1].Type != "session.status_idle" {
		select {
		case event, open := <-events:
			if !open {
				t.Fatal("public SSE ended before idle")
			}
			received = append(received, event)
		case <-ctx.Done():
			t.Fatal("timed out waiting for ordered Brain events")
		}
	}
	var history struct {
		Data []chatEvent `json:"data"`
	}
	requireOK(t, json.Unmarshal(f.env.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/events?order=asc&limit=100", f.env.apiKey, nil, 200), &history))
	want := []string{"span.model_request_start", "agent.thinking", "agent.message", "span.model_request_end"}
	var liveIDs []string
	for _, source := range [][]chatEvent{received, history.Data} {
		var types, ids []string
		for _, event := range source {
			if !slices.Contains(want, event.Type) {
				continue
			}
			if event.Type == "agent.thinking" && (len(event.Content) != 1 || event.Content[0].Thinking != "检查回复") {
				t.Fatalf("thinking content mismatch: %+v", event)
			}
			types, ids = append(types, event.Type), append(ids, event.ID)
		}
		if !slices.Equal(types, want) {
			t.Fatalf("Brain event order = %v, want %v", types, want)
		}
		if liveIDs != nil && !slices.Equal(ids, liveIDs) {
			t.Fatalf("history IDs = %v, SSE IDs = %v", ids, liveIDs)
		}
		liveIDs = ids
	}
}

type brainNode struct {
	command *exec.Cmd
	done    chan error
	stopped bool
}

func startBrainNode(t *testing.T, e *liveEnv, cfg config.Config, name string) *brainNode {
	t.Helper()
	log, err := os.Create(filepath.Join(filepath.Dir(os.Getenv("CONFIG_FILE")), "brain-"+name+".log"))
	requireOK(t, err)
	t.Cleanup(func() { _ = log.Close() })
	command := exec.Command(os.Getenv("OMA_BRAIN_BINARY"))
	command.Env = append(os.Environ(), "OMA_API_URL="+e.url, "NATS_URL="+cfg.NATS.URL)
	command.Stdout, command.Stderr = log, log
	requireOK(t, command.Start())
	node := &brainNode{command: command, done: make(chan error, 1)}
	go func() { node.done <- command.Wait() }()
	t.Cleanup(func() { node.stop(t) })
	return node
}

func (node *brainNode) stop(t *testing.T) {
	t.Helper()
	if node.stopped {
		return
	}
	node.stopped = true
	requireOK(t, node.command.Process.Kill())
	select {
	case <-node.done:
	case <-time.After(10 * time.Second):
		t.Error("brain did not stop")
	}
}

func brainCredentialIdentity(t *testing.T, f *liveSession, epoch int64) codesessions.SessionCredentialIdentity {
	t.Helper()
	c, err := f.env.database.GetCodeSessionCredentialContextForIssue(t.Context(), f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.ExternalID)
	requireOK(t, err)
	return codesessions.SessionCredentialIdentity{SessionID: c.CodeSessionExternalID, PublicSessionID: c.PublicSessionExternalID,
		AgentID: c.AgentExternalID, AgentVersion: c.AgentVersion, OrganizationUUID: c.OrganizationUUID,
		WorkspaceUUID: c.WorkspaceUUID, AccountEmail: c.AccountEmail, WorkerEpoch: epoch}
}

func verifyBrainTaskFencing(t *testing.T, f *liveSession) {
	t.Helper()
	e := f.env
	state, _, err := e.database.GetCodeSession(t.Context(), f.code.ExternalID)
	requireOK(t, err)
	identity := brainCredentialIdentity(t, f, state.CurrentWorkerEpoch)
	claimPath := "/v1/code/sessions/" + f.code.ExternalID + "/worker/register"
	worker := map[string]string{}
	foreign := identity
	foreign.OrganizationUUID = uuid.NewString()
	foreignToken, err := e.credentials.Issue(foreign)
	requireOK(t, err)
	e.request(t, "POST", claimPath, foreignToken, worker, http.StatusUnauthorized)
	identity.WorkerEpoch++
	staleToken, err := e.credentials.Issue(identity)
	requireOK(t, err)
	e.request(t, "POST", claimPath, staleToken, worker, http.StatusConflict)
	e.request(t, "POST", "/v2/ccr-sessions/"+f.code.ExternalID+"/sandbox/mcp", staleToken,
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, http.StatusUnauthorized)
}

var toolModelCalls atomic.Int32

func brainToolModel(t *testing.T) *httptest.Server {
	t.Helper()
	toolModelCalls.Store(0)
	messagePrefix := "msg_brain_" + uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Anthropic-Version") != "2023-06-01" {
			t.Error("Brain model request lost Anthropic-Version")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var request struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		step := toolModelCalls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(kind string, fields map[string]any) {
			fields["type"] = kind
			data, _ := json.Marshal(fields)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data)
			w.(http.Flusher).Flush()
		}
		write("message_start", map[string]any{"message": map[string]any{"id": fmt.Sprintf("%s_%d", messagePrefix, step), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "usage": map[string]int{"input_tokens": 10, "output_tokens": 0}}})
		stop := "end_turn"
		if step == 1 {
			names := make([]string, 0, len(request.Tools))
			for _, tool := range request.Tools {
				names = append(names, tool.Name)
			}
			slices.Sort(names)
			if !slices.Equal(names, []string{"Bash", "Edit", "Glob", "Grep", "Read", "Write", "archive_agent", "list_agents", "send_to_agent"}) {
				t.Errorf("unexpected Brain model tools: %v", names)
			}

			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": "call_brain_write", "name": "Write", "input": map[string]any{}}})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"file_path":"/tmp/oma-brain-proof.txt","content":"native grok loop"}`}})
			stop = "tool_use"
		} else {
			if step != 2 {
				t.Errorf("unexpected native model step %d", step)
			}
			if !bytes.Contains(request.Messages[len(request.Messages)-1].Content, []byte("tool_result")) {
				t.Error("native next model request lost tool result")
			}
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": "done"}})
		}
		write("content_block_stop", map[string]any{"index": 0})
		write("message_delta", map[string]any{"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 12}})
		write("message_stop", map[string]any{})
	}))
	t.Cleanup(server.Close)
	return server
}

func brainHistoryModel(t *testing.T, previewSeen <-chan struct{}, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	fixture := chatModelFixture(t, previewSeen, calls, true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Anthropic-Version") != "2023-06-01" {
			t.Error("Brain model request lost Anthropic-Version")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		requireOK(t, err)
		if calls.Load() == 1 {
			for _, expected := range []string{"请回复固定的聊天验证文本", "tool_result", "oma-brain-proof.txt"} {
				if !bytes.Contains(body, []byte(expected)) {
					t.Errorf("restored model context missing %q", expected)
				}
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		fixture.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}
